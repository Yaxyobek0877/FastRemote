package models

import (
	"encoding/json"
	"time"
)

// User represents an authenticated user
type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
}

// Device represents a connected agent/remote machine
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	IP         string    `json:"ip"`
	Status     string    `json:"status"` // "online" or "offline"
	LastSeen   time.Time `json:"lastSeen"`
	Arch       string    `json:"arch,omitempty"`
	Hostname   string    `json:"hostname,omitempty"`
	DirectPort int       `json:"directPort,omitempty"` // Port for direct viewer connections
}

// WSMessage is the standard WebSocket message envelope
type WSMessage struct {
	Type     string          `json:"type"`
	DeviceID string          `json:"deviceId,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// --- Payload types ---

// SystemInfoPayload is sent by the agent on connect
type SystemInfoPayload struct {
	DeviceName string `json:"deviceName"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Hostname   string `json:"hostname"`
	IP         string `json:"ip"`
	DirectPort int    `json:"directPort,omitempty"`
}

// MouseEventPayload represents a mouse action
type MouseEventPayload struct {
	X      float64 `json:"x"`      // 0.0 - 1.0 relative position
	Y      float64 `json:"y"`      // 0.0 - 1.0 relative position
	Button string  `json:"button"` // "left", "right", "middle"
	Action string  `json:"action"` // "move", "click", "dblclick", "down", "up", "scroll"
	DeltaY float64 `json:"deltaY,omitempty"` // scroll delta
}

// KeyboardEventPayload represents a keyboard action
type KeyboardEventPayload struct {
	Key    string `json:"key"`
	Code   string `json:"code"`
	Action string `json:"action"` // "keydown", "keyup", "keypress"
	Ctrl   bool   `json:"ctrl,omitempty"`
	Alt    bool   `json:"alt,omitempty"`
	Shift  bool   `json:"shift,omitempty"`
	Meta   bool   `json:"meta,omitempty"`
}

// ShellInputPayload is a shell command from the viewer
type ShellInputPayload struct {
	Data string `json:"data"`
}

// ShellOutputPayload is shell output from the agent
type ShellOutputPayload struct {
	Data string `json:"data"`
}

// FileListRequestPayload requests directory listing
type FileListRequestPayload struct {
	Path string `json:"path"`
}

// FileEntry represents a file/directory in a listing
type FileEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// FileListResponsePayload is the directory listing response
type FileListResponsePayload struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
	Error   string      `json:"error,omitempty"`
}

// FileDownloadRequestPayload requests a file download
type FileDownloadRequestPayload struct {
	Path string `json:"path"`
}

// FileDataPayload carries file content chunks
type FileDataPayload struct {
	Path     string `json:"path"`
	FileName string `json:"fileName"`
	Data     string `json:"data"` // base64 encoded
	Offset   int64  `json:"offset"`
	Total    int64  `json:"total"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

// FileUploadStartPayload initiates a file upload
type FileUploadStartPayload struct {
	Path     string `json:"path"` // destination path on remote
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
}

// LoginRequest is the login API request body
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the login API response
type LoginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
}

// ErrorResponse is a generic API error
type ErrorResponse struct {
	Error string `json:"error"`
}
