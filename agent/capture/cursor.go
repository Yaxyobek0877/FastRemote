package capture

import (
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CursorInfo holds the current cursor state
type CursorInfo struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	CursorType string  `json:"cursorType"` // "default", "pointer", "text", "wait", "crosshair", "move", etc.
}

// CursorTracker monitors system cursor position and type
type CursorTracker struct {
	mu        sync.Mutex
	running   bool
	stopChan  chan struct{}
	cursorCh  chan CursorInfo
	screenW   int
	screenH   int
	lastX     float64
	lastY     float64
	lastType  string
}

// NewCursorTracker creates a new cursor tracker
func NewCursorTracker(screenW, screenH int) *CursorTracker {
	if screenW <= 0 {
		screenW = 1920
	}
	if screenH <= 0 {
		screenH = 1080
	}
	return &CursorTracker{
		cursorCh: make(chan CursorInfo, 10),
		screenW:  screenW,
		screenH:  screenH,
		lastType: "default",
	}
}

// Updates returns the channel that receives cursor updates
func (ct *CursorTracker) Updates() <-chan CursorInfo {
	return ct.cursorCh
}

// Start begins cursor tracking
func (ct *CursorTracker) Start() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.running {
		return
	}

	ct.running = true
	ct.stopChan = make(chan struct{})
	go ct.trackLoop()
	log.Println("[Cursor] Started cursor tracking")
}

// Stop halts cursor tracking
func (ct *CursorTracker) Stop() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if !ct.running {
		return
	}

	ct.running = false
	close(ct.stopChan)
	log.Println("[Cursor] Stopped cursor tracking")
}

// UpdateScreenSize sets the screen dimensions for coordinate normalization
func (ct *CursorTracker) UpdateScreenSize(w, h int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if w > 0 {
		ct.screenW = w
	}
	if h > 0 {
		ct.screenH = h
	}
}

var xdotoolLocationRegex = regexp.MustCompile(`x:(\d+)\s+y:(\d+)`)

func (ct *CursorTracker) trackLoop() {
	// Send cursor position every ~33ms (~30 updates/sec)
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ct.stopChan:
			return
		case <-ticker.C:
			info, changed := ct.getCursorInfo()
			if changed {
				select {
				case ct.cursorCh <- info:
				default:
					// Drop if buffer full
				}
			}
		}
	}
}

func (ct *CursorTracker) getCursorInfo() (CursorInfo, bool) {
	// Get cursor position using xdotool
	output, err := exec.Command("xdotool", "getmouselocation", "--shell").CombinedOutput()
	if err != nil {
		return CursorInfo{}, false
	}

	info := CursorInfo{CursorType: "default"}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "X=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "X=")); err == nil {
				info.X = float64(v) / float64(ct.screenW)
			}
		} else if strings.HasPrefix(line, "Y=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "Y=")); err == nil {
				info.Y = float64(v) / float64(ct.screenH)
			}
		}
	}

	// Clamp values
	if info.X < 0 {
		info.X = 0
	}
	if info.X > 1 {
		info.X = 1
	}
	if info.Y < 0 {
		info.Y = 0
	}
	if info.Y > 1 {
		info.Y = 1
	}

	// Try to get cursor type from xfixes or window info
	info.CursorType = ct.detectCursorType()

	// Check if anything changed (threshold to avoid excessive updates)
	ct.mu.Lock()
	dx := info.X - ct.lastX
	dy := info.Y - ct.lastY
	typeChanged := info.CursorType != ct.lastType
	// Only send if moved more than ~2px at 1920 resolution, or type changed
	threshold := 1.0 / float64(ct.screenW) * 2.0
	moved := (dx*dx + dy*dy) > threshold*threshold
	changed := moved || typeChanged

	if changed {
		ct.lastX = info.X
		ct.lastY = info.Y
		ct.lastType = info.CursorType
	}
	ct.mu.Unlock()

	return info, changed
}

func (ct *CursorTracker) detectCursorType() string {
	// Get window under cursor and try to detect cursor type
	// Use the cursor name from X11 if available
	output, err := exec.Command("xdotool", "getactivewindow", "getwindowname").CombinedOutput()
	if err != nil {
		return "default"
	}

	windowName := strings.ToLower(strings.TrimSpace(string(output)))

	// Heuristics based on active window and cursor properties
	// For a full implementation, we'd query X11 XFixesCursorImage
	// but this gives reasonable defaults
	if strings.Contains(windowName, "terminal") ||
		strings.Contains(windowName, "console") ||
		strings.Contains(windowName, "bash") {
		return "text"
	}

	return "default"
}
