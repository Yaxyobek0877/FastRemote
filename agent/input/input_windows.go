//go:build windows
// +build windows

package input

import (
	"log"
	"math"
	"syscall"
	"unsafe"

	"github.com/kbinani/screenshot"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procSendInput       = user32.NewProc("SendInput")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	INPUT_MOUSE    = 0
	INPUT_KEYBOARD = 1

	MOUSEEVENTF_MOVE       = 0x0001
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040
	MOUSEEVENTF_WHEEL      = 0x0800
	MOUSEEVENTF_ABSOLUTE   = 0x8000

	KEYEVENTF_KEYDOWN = 0x0000
	KEYEVENTF_KEYUP   = 0x0002

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	WHEEL_DELTA = 120
)

type mouseInput struct {
	dx, dy     int32
	mouseData  uint32
	flags      uint32
	time       uint32
	extraInfo  uintptr
}

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type inputUnion struct {
	inputType uint32
	padding   [40]byte
}

// Handler handles input injection on Windows
type Handler struct {
	screenW int
	screenH int
}

// New creates a new input handler
func New() *Handler {
	h := &Handler{}
	h.updateScreenSize()
	return h
}

func (h *Handler) updateScreenSize() {
	n := screenshot.NumActiveDisplays()
	if n > 0 {
		bounds := screenshot.GetDisplayBounds(0)
		h.screenW = bounds.Dx()
		h.screenH = bounds.Dy()
	}
	if h.screenW == 0 {
		h.screenW = 1920
	}
	if h.screenH == 0 {
		h.screenH = 1080
	}
}

func (h *Handler) sendMouseInput(flags uint32, dx, dy int32, mouseData uint32) error {
	var input inputUnion
	input.inputType = INPUT_MOUSE

	mi := mouseInput{
		dx:        dx,
		dy:        dy,
		mouseData: mouseData,
		flags:     flags,
	}

	copy(input.padding[:], (*[unsafe.Sizeof(mi)]byte)(unsafe.Pointer(&mi))[:])

	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if ret == 0 {
		return err
	}
	return nil
}

func (h *Handler) toAbsolute(x, y float64) (int32, int32) {
	absX := int32(x * 65535.0)
	absY := int32(y * 65535.0)
	return absX, absY
}

// MouseMove moves the cursor
func (h *Handler) MouseMove(x, y float64) error {
	absX, absY := h.toAbsolute(x, y)
	return h.sendMouseInput(MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
}

// MouseClick performs a click
func (h *Handler) MouseClick(x, y float64, button string) error {
	absX, absY := h.toAbsolute(x, y)

	var downFlag, upFlag uint32
	switch button {
	case "right":
		downFlag = MOUSEEVENTF_RIGHTDOWN
		upFlag = MOUSEEVENTF_RIGHTUP
	case "middle":
		downFlag = MOUSEEVENTF_MIDDLEDOWN
		upFlag = MOUSEEVENTF_MIDDLEUP
	default:
		downFlag = MOUSEEVENTF_LEFTDOWN
		upFlag = MOUSEEVENTF_LEFTUP
	}

	h.sendMouseInput(MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
	h.sendMouseInput(downFlag|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
	return h.sendMouseInput(upFlag|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
}

// MouseDoubleClick performs a double click
func (h *Handler) MouseDoubleClick(x, y float64, button string) error {
	h.MouseClick(x, y, button)
	return h.MouseClick(x, y, button)
}

// MouseDown presses button
func (h *Handler) MouseDown(x, y float64, button string) error {
	absX, absY := h.toAbsolute(x, y)
	var flag uint32
	switch button {
	case "right":
		flag = MOUSEEVENTF_RIGHTDOWN
	case "middle":
		flag = MOUSEEVENTF_MIDDLEDOWN
	default:
		flag = MOUSEEVENTF_LEFTDOWN
	}
	h.sendMouseInput(MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
	return h.sendMouseInput(flag|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
}

// MouseUp releases button
func (h *Handler) MouseUp(x, y float64, button string) error {
	absX, absY := h.toAbsolute(x, y)
	var flag uint32
	switch button {
	case "right":
		flag = MOUSEEVENTF_RIGHTUP
	case "middle":
		flag = MOUSEEVENTF_MIDDLEUP
	default:
		flag = MOUSEEVENTF_LEFTUP
	}
	h.sendMouseInput(MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
	return h.sendMouseInput(flag|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)
}

// MouseScroll scrolls
func (h *Handler) MouseScroll(x, y float64, deltaY float64) error {
	absX, absY := h.toAbsolute(x, y)
	h.sendMouseInput(MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE, absX, absY, 0)

	scrollAmount := int32(deltaY / 100.0 * float64(WHEEL_DELTA))
	if math.Abs(float64(scrollAmount)) < WHEEL_DELTA {
		if deltaY > 0 {
			scrollAmount = WHEEL_DELTA
		} else {
			scrollAmount = -WHEEL_DELTA
		}
	}
	return h.sendMouseInput(MOUSEEVENTF_WHEEL, 0, 0, uint32(scrollAmount))
}

func (h *Handler) sendKeyInput(vk uint16, flags uint32) error {
	var input inputUnion
	input.inputType = INPUT_KEYBOARD

	ki := keybdInput{
		wVk:     vk,
		dwFlags: flags,
	}

	copy(input.padding[:], (*[unsafe.Sizeof(ki)]byte)(unsafe.Pointer(&ki))[:])

	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if ret == 0 {
		return err
	}
	return nil
}

// KeyPress simulates a key press and release
func (h *Handler) KeyPress(key, code string, ctrl, alt, shift, meta bool) error {
	vk := mapKeyToVK(key, code)
	if vk == 0 {
		return nil
	}

	if ctrl {
		h.sendKeyInput(0x11, KEYEVENTF_KEYDOWN) // VK_CONTROL
	}
	if alt {
		h.sendKeyInput(0x12, KEYEVENTF_KEYDOWN) // VK_MENU
	}
	if shift {
		h.sendKeyInput(0x10, KEYEVENTF_KEYDOWN) // VK_SHIFT
	}
	if meta {
		h.sendKeyInput(0x5B, KEYEVENTF_KEYDOWN) // VK_LWIN
	}

	h.sendKeyInput(vk, KEYEVENTF_KEYDOWN)
	h.sendKeyInput(vk, KEYEVENTF_KEYUP)

	if meta {
		h.sendKeyInput(0x5B, KEYEVENTF_KEYUP)
	}
	if shift {
		h.sendKeyInput(0x10, KEYEVENTF_KEYUP)
	}
	if alt {
		h.sendKeyInput(0x12, KEYEVENTF_KEYUP)
	}
	if ctrl {
		h.sendKeyInput(0x11, KEYEVENTF_KEYUP)
	}

	return nil
}

// KeyDown presses a key
func (h *Handler) KeyDown(key, code string) error {
	vk := mapKeyToVK(key, code)
	if vk == 0 {
		return nil
	}
	return h.sendKeyInput(vk, KEYEVENTF_KEYDOWN)
}

// KeyUp releases a key
func (h *Handler) KeyUp(key, code string) error {
	vk := mapKeyToVK(key, code)
	if vk == 0 {
		return nil
	}
	return h.sendKeyInput(vk, KEYEVENTF_KEYUP)
}

// TypeText types a string
func (h *Handler) TypeText(text string) error {
	for _, ch := range text {
		vk := uint16(ch)
		if ch >= 'a' && ch <= 'z' {
			vk = uint16(ch - 32) // uppercase VK
		}
		h.sendKeyInput(vk, KEYEVENTF_KEYDOWN)
		h.sendKeyInput(vk, KEYEVENTF_KEYUP)
	}
	return nil
}

func mapKeyToVK(key, code string) uint16 {
	vkMap := map[string]uint16{
		"Enter":       0x0D,
		"Backspace":   0x08,
		"Tab":         0x09,
		"Escape":      0x1B,
		"Delete":      0x2E,
		"Home":        0x24,
		"End":         0x23,
		"PageUp":      0x21,
		"PageDown":    0x22,
		"ArrowUp":     0x26,
		"ArrowDown":   0x28,
		"ArrowLeft":   0x25,
		"ArrowRight":  0x27,
		"Insert":      0x2D,
		"F1":          0x70,
		"F2":          0x71,
		"F3":          0x72,
		"F4":          0x73,
		"F5":          0x74,
		"F6":          0x75,
		"F7":          0x76,
		"F8":          0x77,
		"F9":          0x78,
		"F10":         0x79,
		"F11":         0x7A,
		"F12":         0x7B,
		"Control":     0x11,
		"Shift":       0x10,
		"Alt":         0x12,
		"Meta":        0x5B,
		" ":           0x20,
		"CapsLock":    0x14,
		"PrintScreen": 0x2C,
	}

	if vk, ok := vkMap[key]; ok {
		return vk
	}

	// Handle alphanumeric
	if len(key) == 1 {
		ch := key[0]
		if ch >= 'a' && ch <= 'z' {
			return uint16(ch - 32) // VK codes are uppercase
		}
		if ch >= 'A' && ch <= 'Z' {
			return uint16(ch)
		}
		if ch >= '0' && ch <= '9' {
			return uint16(ch)
		}
	}

	log.Printf("[Input] Unmapped key: %s (code: %s)", key, code)
	return 0
}
