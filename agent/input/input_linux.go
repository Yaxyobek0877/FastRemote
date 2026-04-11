//go:build linux
// +build linux

package input

import (
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var (
	xdotoolStdin io.WriteCloser
	xdotoolMu    sync.Mutex
	xdotoolOnce  sync.Once
)

func initXdotool() {
	// Start xdotool once to accept commands via stdin for smooth input
	cmd := exec.Command("xdotool", "-")
	stdin, err := cmd.StdinPipe()
	if err == nil {
		xdotoolStdin = stdin
		err = cmd.Start()
		if err != nil {
			log.Printf("[Input] Failed to start persistent xdotool: %v", err)
			xdotoolStdin = nil
		} else {
			log.Println("[Input] xdotool persistent stdin session started")
		}
	} else {
		log.Printf("[Input] Failed to create StdinPipe for xdotool: %v", err)
	}
}

// Handler handles input injection on Linux using xdotool
type Handler struct {
	screenW int
	screenH int
}

// New creates a new input handler
func New() *Handler {
	h := &Handler{}
	h.updateScreenSize()
	xdotoolOnce.Do(initXdotool)
	return h
}

func (h *Handler) updateScreenSize() {
	// Use xdotool's own coordinate space for accurate positioning
	output, err := exec.Command("xdotool", "getdisplaygeometry").CombinedOutput()
	if err == nil {
		parts := strings.Fields(strings.TrimSpace(string(output)))
		if len(parts) == 2 {
			if w, e := strconv.Atoi(parts[0]); e == nil && w > 0 {
				h.screenW = w
			}
			if ht, e := strconv.Atoi(parts[1]); e == nil && ht > 0 {
				h.screenH = ht
			}
		}
	}
	if h.screenW == 0 {
		h.screenW = 1920
	}
	if h.screenH == 0 {
		h.screenH = 1080
	}
	log.Printf("[Input] Screen size for xdotool: %dx%d", h.screenW, h.screenH)
}

// MouseMove moves the cursor to relative position (0.0-1.0)
func (h *Handler) MouseMove(x, y float64) error {
	absX := int(x * float64(h.screenW))
	absY := int(y * float64(h.screenH))
	// Clamp
	if absX < 0 {
		absX = 0
	}
	if absX >= h.screenW {
		absX = h.screenW - 1
	}
	if absY < 0 {
		absY = 0
	}
	if absY >= h.screenH {
		absY = h.screenH - 1
	}
	return runXdotool("mousemove", "--screen", "0", strconv.Itoa(absX), strconv.Itoa(absY))
}

// MouseMoveRelative moves cursor by delta (pixel offsets)
func (h *Handler) MouseMoveRelative(dx, dy int) error {
	if dx == 0 && dy == 0 {
		return nil
	}
	return runXdotool("mousemove_relative", "--", strconv.Itoa(dx), strconv.Itoa(dy))
}

// MouseClick performs a mouse click
func (h *Handler) MouseClick(x, y float64, button string) error {
	if err := h.MouseMove(x, y); err != nil {
		return err
	}

	btn := "1" // left
	switch button {
	case "right":
		btn = "3"
	case "middle":
		btn = "2"
	}

	return runXdotool("click", btn)
}

// MouseDoubleClick performs a double click
func (h *Handler) MouseDoubleClick(x, y float64, button string) error {
	if err := h.MouseMove(x, y); err != nil {
		return err
	}

	btn := "1"
	switch button {
	case "right":
		btn = "3"
	case "middle":
		btn = "2"
	}

	return runXdotool("click", "--repeat", "2", "--delay", "50", btn)
}

// MouseDown presses a mouse button
func (h *Handler) MouseDown(x, y float64, button string) error {
	if err := h.MouseMove(x, y); err != nil {
		return err
	}

	btn := "1"
	switch button {
	case "right":
		btn = "3"
	case "middle":
		btn = "2"
	}

	return runXdotool("mousedown", btn)
}

// MouseUp releases a mouse button
func (h *Handler) MouseUp(x, y float64, button string) error {
	if err := h.MouseMove(x, y); err != nil {
		return err
	}

	btn := "1"
	switch button {
	case "right":
		btn = "3"
	case "middle":
		btn = "2"
	}

	return runXdotool("mouseup", btn)
}

// MouseScroll scrolls the mouse wheel
func (h *Handler) MouseScroll(x, y float64, deltaY float64) error {
	if err := h.MouseMove(x, y); err != nil {
		return err
	}

	// Normalize: 1 click per 120 units of deltaY, clamped to 1-3
	clicks := int(math.Abs(deltaY) / 120)
	if clicks < 1 {
		clicks = 1
	}
	if clicks > 3 {
		clicks = 3
	}

	direction := "5" // scroll down
	if deltaY < 0 {
		direction = "4" // scroll up
	}

	return runXdotool("click", "--repeat", strconv.Itoa(clicks), direction)
}

// TypeChar types a single character accurately using xdotool type
// This handles all printable characters including shifted ones (?, !, @, etc.)
func (h *Handler) TypeChar(char string) error {
	// Space needs special handling — 'type' via stdin loses the space
	if char == " " {
		return runXdotool("key", "space")
	}
	return runXdotool("type", "--clearmodifiers", "--delay", "0", "--", char)
}

// KeyPress simulates a key press with modifiers
func (h *Handler) KeyPress(key, code string, ctrl, alt, shift, meta bool) error {
	xKey := mapKeyToXdotool(key, code)
	if xKey == "" {
		return nil
	}

	// Build modifier prefix
	var modifiers []string
	if ctrl {
		modifiers = append(modifiers, "ctrl")
	}
	if alt {
		modifiers = append(modifiers, "alt")
	}
	if shift {
		modifiers = append(modifiers, "shift")
	}
	if meta {
		modifiers = append(modifiers, "super")
	}

	if len(modifiers) > 0 {
		combo := strings.Join(modifiers, "+") + "+" + xKey
		return runXdotool("key", combo)
	}

	return runXdotool("key", xKey)
}

// KeyDown presses a key down
func (h *Handler) KeyDown(key, code string) error {
	xKey := mapKeyToXdotool(key, code)
	if xKey == "" {
		return nil
	}
	return runXdotool("keydown", xKey)
}

// KeyUp releases a key
func (h *Handler) KeyUp(key, code string) error {
	xKey := mapKeyToXdotool(key, code)
	if xKey == "" {
		return nil
	}
	return runXdotool("keyup", xKey)
}

// TypeText types a string
func (h *Handler) TypeText(text string) error {
	return runXdotool("type", "--clearmodifiers", "--delay", "0", "--", text)
}

func runXdotool(args ...string) error {
	xdotoolMu.Lock()
	defer xdotoolMu.Unlock()

	// Fast path: use persistent xdotool process if available
	if xdotoolStdin != nil {
		cmdStr := strings.Join(args, " ") + "\n"
		_, err := xdotoolStdin.Write([]byte(cmdStr))
		return err
	}

	// Fallback to one-off process
	cmd := exec.Command("xdotool", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[Input] xdotool error: %s %v: %s", strings.Join(args, " "), err, string(output))
		return fmt.Errorf("xdotool %s: %w", args[0], err)
	}
	return nil
}

// mapKeyToXdotool maps JavaScript key names to xdotool key names
func mapKeyToXdotool(key, code string) string {
	// Special keys mapping
	keyMap := map[string]string{
		"Enter":       "Return",
		"Backspace":   "BackSpace",
		"Tab":         "Tab",
		"Escape":      "Escape",
		"Delete":      "Delete",
		"Home":        "Home",
		"End":         "End",
		"PageUp":      "Prior",
		"PageDown":    "Next",
		"ArrowUp":     "Up",
		"ArrowDown":   "Down",
		"ArrowLeft":   "Left",
		"ArrowRight":  "Right",
		"Insert":      "Insert",
		"F1":          "F1",
		"F2":          "F2",
		"F3":          "F3",
		"F4":          "F4",
		"F5":          "F5",
		"F6":          "F6",
		"F7":          "F7",
		"F8":          "F8",
		"F9":          "F9",
		"F10":         "F10",
		"F11":         "F11",
		"F12":         "F12",
		"Control":     "Control_L",
		"Shift":       "Shift_L",
		"Alt":         "Alt_L",
		"Meta":        "Super_L",
		" ":           "space",
		"CapsLock":    "Caps_Lock",
		"PrintScreen": "Print",
		"NumLock":     "Num_Lock",
		"ScrollLock":  "Scroll_Lock",
		"Pause":       "Pause",
		"ContextMenu": "Menu",
	}

	if mapped, ok := keyMap[key]; ok {
		return mapped
	}

	// Special characters → X11 keysym names
	charMap := map[string]string{
		"!":  "exclam",
		"@":  "at",
		"#":  "numbersign",
		"$":  "dollar",
		"%":  "percent",
		"^":  "asciicircum",
		"&":  "ampersand",
		"*":  "asterisk",
		"(":  "parenleft",
		")":  "parenright",
		"_":  "underscore",
		"+":  "plus",
		"{":  "braceleft",
		"}":  "braceright",
		"|":  "bar",
		":":  "colon",
		"\"": "quotedbl",
		"<":  "less",
		">":  "greater",
		"?":  "question",
		"~":  "asciitilde",
		"-":  "minus",
		"=":  "equal",
		"[":  "bracketleft",
		"]":  "bracketright",
		"\\": "backslash",
		";":  "semicolon",
		"'":  "apostrophe",
		",":  "comma",
		".":  "period",
		"/":  "slash",
		"`":  "grave",
	}

	if mapped, ok := charMap[key]; ok {
		return mapped
	}

	// Single character keys (letters, digits)
	if len(key) == 1 {
		return key
	}

	return ""
}
