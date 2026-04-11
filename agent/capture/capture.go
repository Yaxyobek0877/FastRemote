package capture

import (
	"bytes"
	"image"
	"image/jpeg"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kbinani/screenshot"
	"golang.org/x/image/draw"
)

// Capturer handles screen capture with adaptive quality
type Capturer struct {
	mu        sync.Mutex
	streaming bool
	fps       int
	quality   int
	maxWidth  int
	maxHeight int
	stopChan  chan struct{}
	frameChan chan []byte

	// Adaptive quality
	baseQuality    int
	currentQuality int
	minQuality     int
	maxQuality     int
	droppedFrames  int32 // atomic
	totalFrames    int32 // atomic

	// Delta detection — separate mutex to avoid contention with settings
	frameMu       sync.Mutex
	lastFrame     []byte
	skipIdentical bool

	// Force next frame flag — bypasses delta detection when mouse is active
	forceNext     int32 // atomic: 1 = force, 0 = normal
	forceUntil    int64 // atomic: unix nano timestamp until which to force frames

	// Buffer pool for JPEG encoding
	bufPool sync.Pool

	// Actual screen size (for cursor normalization)
	ActualWidth  int
	ActualHeight int
}

// New creates a new Capturer with sensible defaults
func New(fps, quality, maxWidth, maxHeight int) *Capturer {
	if fps <= 0 {
		fps = 20
	}
	if quality <= 0 {
		quality = 85
	}
	if maxWidth <= 0 {
		maxWidth = 3840
	}
	if maxHeight <= 0 {
		maxHeight = 2160
	}

	// Detect actual screen size
	actualW, actualH := 1920, 1080
	n := screenshot.NumActiveDisplays()
	if n > 0 {
		bounds := screenshot.GetDisplayBounds(0)
		actualW = bounds.Dx()
		actualH = bounds.Dy()
	}

	return &Capturer{
		fps:            fps,
		quality:        quality,
		maxWidth:       maxWidth,
		maxHeight:      maxHeight,
		frameChan:      make(chan []byte, 2), // Buffered(2) — smooths frame delivery, prevents drops
		baseQuality:    quality,
		currentQuality: quality,
		minQuality:     30,
		maxQuality:     100,
		skipIdentical:  false, // Disabled — sampling misses small text edits. FPS ticker is sufficient throttle.
		ActualWidth:    actualW,
		ActualHeight:   actualH,
		bufPool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Frames returns the channel that receives JPEG frames
func (c *Capturer) Frames() <-chan []byte {
	return c.frameChan
}

// ForceNextFrame forces the next N milliseconds of frames to bypass delta detection.
// Call this when the user is actively interacting (mouse move, click, etc.)
func (c *Capturer) ForceNextFrame() {
	// Force frames for the next 500ms after any mouse activity
	atomic.StoreInt64(&c.forceUntil, time.Now().Add(500*time.Millisecond).UnixNano())
	atomic.StoreInt32(&c.forceNext, 1)
}

// shouldForceFrame checks if delta detection should be bypassed
func (c *Capturer) shouldForceFrame() bool {
	if atomic.LoadInt32(&c.forceNext) == 0 {
		return false
	}
	if time.Now().UnixNano() > atomic.LoadInt64(&c.forceUntil) {
		// Force period expired
		atomic.StoreInt32(&c.forceNext, 0)
		return false
	}
	return true
}

// Start begins screen capture
func (c *Capturer) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.streaming {
		return
	}

	c.streaming = true
	c.stopChan = make(chan struct{})
	atomic.StoreInt32(&c.droppedFrames, 0)
	atomic.StoreInt32(&c.totalFrames, 0)
	go c.captureLoop()
	log.Printf("[Capture] Started: %d FPS, quality %d, max %dx%d", c.fps, c.quality, c.maxWidth, c.maxHeight)
}

// Stop halts screen capture
func (c *Capturer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.streaming {
		return
	}

	c.streaming = false
	close(c.stopChan)
	log.Printf("[Capture] Stopped (dropped %d/%d frames)", atomic.LoadInt32(&c.droppedFrames), atomic.LoadInt32(&c.totalFrames))
}

// IsStreaming returns whether capture is active
func (c *Capturer) IsStreaming() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streaming
}

// SetSettings dynamically updates capture settings
func (c *Capturer) SetSettings(fps, quality, maxWidth, maxHeight int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if fps > 0 && fps <= 60 {
		c.fps = fps
	}
	if quality > 0 && quality <= 100 {
		c.quality = quality
		c.baseQuality = quality
		c.currentQuality = quality
	}
	if maxWidth > 0 {
		c.maxWidth = maxWidth
	}
	if maxHeight > 0 {
		c.maxHeight = maxHeight
	}

	log.Printf("[Capture] Settings updated: %d FPS, quality %d, max %dx%d", c.fps, c.quality, c.maxWidth, c.maxHeight)
}

// GetSettings returns current capture settings
func (c *Capturer) GetSettings() (fps, quality, maxWidth, maxHeight int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fps, c.currentQuality, c.maxWidth, c.maxHeight
}

func (c *Capturer) captureLoop() {
	for {
		select {
		case <-c.stopChan:
			return
		default:
		}

		frameStart := time.Now()

		frame, err := c.captureFrame()
		if err != nil {
			log.Printf("[Capture] Error: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if frame != nil {
			atomic.AddInt32(&c.totalFrames, 1)

			// "Latest frame swap" — drain any stale frame then push new one
			select {
			case <-c.frameChan:
			default:
			}
			select {
			case c.frameChan <- frame:
			default:
				atomic.AddInt32(&c.droppedFrames, 1)
			}
		}

		// Precise sleep to hit target FPS
		c.mu.Lock()
		interval := time.Duration(1000/c.fps) * time.Millisecond
		c.mu.Unlock()

		elapsed := time.Since(frameStart)
		if elapsed < interval {
			time.Sleep(interval - elapsed)
		}
	}
}

func (c *Capturer) adjustQuality() {
	// Disabled adaptive quality to respect manual settings
	c.mu.Lock()
	defer c.mu.Unlock()
	c.droppedFrames = 0
	c.totalFrames = 0
}

func (c *Capturer) captureFrame() ([]byte, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, nil
	}

	// Capture primary display
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, err
	}

	// Update actual screen size
	c.ActualWidth = bounds.Dx()
	c.ActualHeight = bounds.Dy()

	// No delta detection — every frame is captured and sent.
	// FPS ticker is sufficient throttle. Delta detection was causing
	// small text changes to be missed (sampling couldn't catch them).

	// Resize if needed
	resized := c.resizeImage(img)

	// Encode to JPEG using pooled buffer
	c.mu.Lock()
	quality := c.currentQuality
	c.mu.Unlock()

	buf := c.bufPool.Get().(*bytes.Buffer)
	buf.Reset()

	err = jpeg.Encode(buf, resized, &jpeg.Options{Quality: quality})
	if err != nil {
		c.bufPool.Put(buf)
		return nil, err
	}

	frameData := make([]byte, buf.Len())
	copy(frameData, buf.Bytes())
	c.bufPool.Put(buf)

	return frameData, nil
}

// fastFrameCompare uses sampled byte comparison.
// Returns true if frame is (likely) identical to the last one.
// Uses frameMu (NOT c.mu) to avoid blocking settings/quality changes.
func (c *Capturer) fastFrameCompare(frame []byte) bool {
	c.frameMu.Lock()
	defer c.frameMu.Unlock()

	if c.lastFrame == nil || len(frame) != len(c.lastFrame) {
		c.lastFrame = make([]byte, len(frame))
		copy(c.lastFrame, frame)
		return false
	}

	// Sample 500 points — fast enough to never cause frame stalls
	step := len(frame) / 500
	if step < 4 {
		step = 4
	}

	for i := 0; i < len(frame); i += step {
		if frame[i] != c.lastFrame[i] {
			// Different — update stored frame and return false
			copy(c.lastFrame, frame)
			return false
		}
	}

	return true
}

func (c *Capturer) resizeImage(src image.Image) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	c.mu.Lock()
	maxW := c.maxWidth
	maxH := c.maxHeight
	c.mu.Unlock()

	if srcW <= maxW && srcH <= maxH {
		return src
	}

	// Calculate new dimensions maintaining aspect ratio
	ratio := float64(srcW) / float64(srcH)
	newW := maxW
	newH := int(float64(newW) / ratio)

	if newH > maxH {
		newH = maxH
		newW = int(float64(newH) * ratio)
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	// Use NearestNeighbor instead of ApproxBiLinear for real-time speed, massive CPU latency reduction
	draw.NearestNeighbor.Scale(dst, dst.Bounds(), src, srcBounds, draw.Over, nil)
	return dst
}
