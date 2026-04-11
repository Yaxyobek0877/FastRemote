package capture

import (
	"bytes"
	"image"
	"image/jpeg"
	"log"
	"sync"
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
	droppedFrames  int
	totalFrames    int

	// Delta detection — fast byte comparison instead of MD5
	lastFrame     []byte
	skipIdentical bool

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
		frameChan:      make(chan []byte, 2), // Reduced from 5 to 2 for lower latency
		baseQuality:    quality,
		currentQuality: quality,
		minQuality:     30,
		maxQuality:     100,
		skipIdentical:  true,
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

// Start begins screen capture
func (c *Capturer) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.streaming {
		return
	}

	c.streaming = true
	c.stopChan = make(chan struct{})
	c.droppedFrames = 0
	c.totalFrames = 0
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
	log.Printf("[Capture] Stopped (dropped %d/%d frames)", c.droppedFrames, c.totalFrames)
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
	c.mu.Lock()
	interval := time.Duration(1000/c.fps) * time.Millisecond
	c.mu.Unlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	adaptiveTicker := time.NewTicker(2 * time.Second)
	defer adaptiveTicker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-adaptiveTicker.C:
			c.adjustQuality()
			// Update ticker interval if fps changed
			c.mu.Lock()
			newInterval := time.Duration(1000/c.fps) * time.Millisecond
			c.mu.Unlock()
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
			}
		case <-ticker.C:
			frame, err := c.captureFrame()
			if err != nil {
				log.Printf("[Capture] Error: %v", err)
				continue
			}
			if frame == nil {
				continue // Delta detection: frame unchanged
			}

			c.mu.Lock()
			c.totalFrames++
			c.mu.Unlock()

			// Non-blocking send (drop frames if buffer full)
			select {
			case c.frameChan <- frame:
			default:
				c.mu.Lock()
				c.droppedFrames++
				c.mu.Unlock()
			}
		}
	}
}

func (c *Capturer) adjustQuality() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.totalFrames == 0 {
		return
	}

	dropRate := float64(c.droppedFrames) / float64(c.totalFrames)

	if dropRate > 0.15 {
		// Too many drops — reduce quality
		c.currentQuality -= 10
		if c.currentQuality < c.minQuality {
			c.currentQuality = c.minQuality
		}
		log.Printf("[Capture] Adaptive: quality ↓ %d (drop rate %.1f%%)", c.currentQuality, dropRate*100)
	} else if dropRate < 0.03 && c.currentQuality < c.baseQuality {
		// Low drops — increase quality back toward base
		c.currentQuality += 5
		if c.currentQuality > c.baseQuality {
			c.currentQuality = c.baseQuality
		}
		log.Printf("[Capture] Adaptive: quality ↑ %d (drop rate %.1f%%)", c.currentQuality, dropRate*100)
	}

	// Reset counters
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
	c.mu.Lock()
	c.ActualWidth = bounds.Dx()
	c.ActualHeight = bounds.Dy()
	c.mu.Unlock()

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

	// Fast delta detection: compare frame size first, then sample bytes
	if c.skipIdentical && c.fastFrameCompare(frameData) {
		return nil, nil // No significant change
	}

	return frameData, nil
}

// fastFrameCompare uses size + sampled byte comparison instead of MD5
// Returns true if frame is (likely) identical to the last one
func (c *Capturer) fastFrameCompare(frame []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lastFrame == nil {
		c.lastFrame = make([]byte, len(frame))
		copy(c.lastFrame, frame)
		return false
	}

	// Different size = definitely different
	if len(frame) != len(c.lastFrame) {
		c.lastFrame = make([]byte, len(frame))
		copy(c.lastFrame, frame)
		return false
	}

	// Sample comparison: check every Nth byte for speed
	// For a 100KB frame, checking every 64th byte = ~1500 comparisons
	step := len(frame) / 1500
	if step < 1 {
		step = 1
	}

	identical := true
	for i := 0; i < len(frame); i += step {
		if frame[i] != c.lastFrame[i] {
			identical = false
			break
		}
	}

	if !identical {
		copy(c.lastFrame[:0], frame) // reuse slice if possible
		c.lastFrame = append(c.lastFrame[:0], frame...)
	}

	return identical
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
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, srcBounds, draw.Over, nil)
	return dst
}
