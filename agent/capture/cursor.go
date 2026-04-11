package capture

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CursorInfo holds the current cursor state
type CursorInfo struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	CursorType string  `json:"cursorType"` // "default", "pointer", "text", "wait", "crosshair", "move", etc.
}

// CursorTracker monitors system cursor type using XFixes
type CursorTracker struct {
	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
	cursorCh chan CursorInfo
	lastType string

	// Python subprocess
	cmd *exec.Cmd
}

// NewCursorTracker creates a new cursor tracker
func NewCursorTracker(screenW, screenH int) *CursorTracker {
	return &CursorTracker{
		cursorCh: make(chan CursorInfo, 20),
		lastType: "default",
	}
}

// Updates returns the channel that receives cursor updates
func (ct *CursorTracker) Updates() <-chan CursorInfo {
	return ct.cursorCh
}

// Start begins cursor tracking via Python XFixes subprocess
func (ct *CursorTracker) Start() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.running {
		return
	}

	ct.running = true
	ct.stopChan = make(chan struct{})
	go ct.trackLoop()
	log.Println("[Cursor] Started cursor tracking (XFixes)")
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

	// Kill the python subprocess
	if ct.cmd != nil && ct.cmd.Process != nil {
		ct.cmd.Process.Kill()
		ct.cmd = nil
	}

	log.Println("[Cursor] Stopped cursor tracking")
}

// UpdateScreenSize is kept for API compatibility
func (ct *CursorTracker) UpdateScreenSize(w, h int) {}

func (ct *CursorTracker) trackLoop() {
	// Find the Python script
	scriptPath := ct.findScript()
	if scriptPath == "" {
		log.Println("[Cursor] cursor_detect.py not found, cursor type detection disabled")
		return
	}

	log.Printf("[Cursor] Using cursor detection script: %s", scriptPath)

	for {
		select {
		case <-ct.stopChan:
			return
		default:
		}

		// Start Python subprocess
		cmd := exec.Command("python3", scriptPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("[Cursor] Failed to create pipe: %v", err)
			return
		}

		if err := cmd.Start(); err != nil {
			log.Printf("[Cursor] Failed to start cursor_detect.py: %v", err)
			return
		}

		ct.mu.Lock()
		ct.cmd = cmd
		ct.mu.Unlock()

		// Read cursor type changes line by line
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-ct.stopChan:
				cmd.Process.Kill()
				return
			default:
			}

			newType := strings.TrimSpace(scanner.Text())
			if newType == "" {
				continue
			}

			// Validate known types
			switch newType {
			case "default", "text", "pointer", "crosshair", "move", "wait":
				// valid
			default:
				newType = "default"
			}

			ct.mu.Lock()
			changed := newType != ct.lastType
			if changed {
				ct.lastType = newType
			}
			ct.mu.Unlock()

			if changed {
				info := CursorInfo{CursorType: newType}
				// Latest-value swap
				select {
				case <-ct.cursorCh:
				default:
				}
				select {
				case ct.cursorCh <- info:
				default:
				}
			}
		}

		// Process exited — check if we should restart
		cmd.Wait()

		ct.mu.Lock()
		ct.cmd = nil
		shouldContinue := ct.running
		ct.mu.Unlock()

		if !shouldContinue {
			return
		}

		log.Println("[Cursor] cursor_detect.py exited, restarting in 2s...")
		select {
		case <-ct.stopChan:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (ct *CursorTracker) findScript() string {
	// 1. Check next to the executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "cursor_detect.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		candidate = filepath.Join(dir, "capture", "cursor_detect.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// 2. Check working directory
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, "cursor_detect.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		candidate = filepath.Join(wd, "capture", "cursor_detect.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}
