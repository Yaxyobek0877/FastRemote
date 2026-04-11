package filetransfer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const chunkSize = 64 * 1024 // 64KB chunks

// FileEntry represents a file/directory in a listing
type FileEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// FileListResponse is the directory listing response
type FileListResponse struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
	Error   string      `json:"error,omitempty"`
}

// FileData carries file content
type FileData struct {
	Path     string `json:"path"`
	FileName string `json:"fileName"`
	Data     string `json:"data"` // base64
	Offset   int64  `json:"offset"`
	Total    int64  `json:"total"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

// Handler handles file transfer operations
type Handler struct {
	sendMsg func(msgType string, payload interface{})
}

// New creates a new file transfer handler
func New(sender func(msgType string, payload interface{})) *Handler {
	return &Handler{sendMsg: sender}
}

// ListDirectory lists contents of a directory
func (h *Handler) ListDirectory(path string) {
	if path == "" || path == "~" {
		if runtime.GOOS == "windows" {
			path = os.Getenv("USERPROFILE")
		} else {
			path, _ = os.UserHomeDir()
		}
	}

	// Security: prevent path traversal
	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	if err != nil {
		h.sendMsg("file_list_response", FileListResponse{
			Path:  path,
			Error: err.Error(),
		})
		return
	}

	var fileEntries []FileEntry
	for _, entry := range entries {
		// Skip hidden files on Linux (optional, show all for remote admin)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileEntries = append(fileEntries, FileEntry{
			Name:    entry.Name(),
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}

	h.sendMsg("file_list_response", FileListResponse{
		Path:    path,
		Entries: fileEntries,
	})

	log.Printf("[FileTransfer] Listed directory: %s (%d entries)", path, len(fileEntries))
}

// DownloadFile reads a file and sends it in chunks
func (h *Handler) DownloadFile(path string) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		h.sendMsg("file_data", FileData{
			Path:  path,
			Error: err.Error(),
			Done:  true,
		})
		return
	}

	if info.IsDir() {
		h.sendMsg("file_data", FileData{
			Path:  path,
			Error: "cannot download a directory",
			Done:  true,
		})
		return
	}

	file, err := os.Open(path)
	if err != nil {
		h.sendMsg("file_data", FileData{
			Path:  path,
			Error: err.Error(),
			Done:  true,
		})
		return
	}
	defer file.Close()

	totalSize := info.Size()
	fileName := filepath.Base(path)
	buf := make([]byte, chunkSize)
	var offset int64

	for {
		n, err := file.Read(buf)
		if n > 0 {
			chunk := FileData{
				Path:     path,
				FileName: fileName,
				Data:     base64.StdEncoding.EncodeToString(buf[:n]),
				Offset:   offset,
				Total:    totalSize,
				Done:     false,
			}

			offset += int64(n)

			if err == io.EOF || offset >= totalSize {
				chunk.Done = true
			}

			h.sendMsg("file_data", chunk)
		}

		if err != nil {
			if err != io.EOF {
				h.sendMsg("file_data", FileData{
					Path:  path,
					Error: err.Error(),
					Done:  true,
				})
			}
			break
		}
	}

	log.Printf("[FileTransfer] Downloaded: %s (%d bytes)", path, totalSize)
}

// UploadFileChunk receives and writes a file chunk
func (h *Handler) UploadFileChunk(rawPayload json.RawMessage) {
	var data FileData
	if err := json.Unmarshal(rawPayload, &data); err != nil {
		log.Printf("[FileTransfer] Invalid upload data: %v", err)
		return
	}

	path := filepath.Clean(data.Path)

	// Security: basic path validation
	if strings.Contains(path, "..") {
		h.sendMsg("file_upload_status", map[string]interface{}{
			"path":  path,
			"error": "invalid path",
		})
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)

	// Open file for writing
	var flag int
	if data.Offset == 0 {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	} else {
		flag = os.O_CREATE | os.O_WRONLY
	}

	file, err := os.OpenFile(path, flag, 0644)
	if err != nil {
		h.sendMsg("file_upload_status", map[string]interface{}{
			"path":  path,
			"error": fmt.Sprintf("failed to open file: %v", err),
		})
		return
	}
	defer file.Close()

	// Seek to offset
	if data.Offset > 0 {
		file.Seek(data.Offset, io.SeekStart)
	}

	// Decode and write
	decoded, err := base64.StdEncoding.DecodeString(data.Data)
	if err != nil {
		h.sendMsg("file_upload_status", map[string]interface{}{
			"path":  path,
			"error": fmt.Sprintf("failed to decode data: %v", err),
		})
		return
	}

	_, err = file.Write(decoded)
	if err != nil {
		h.sendMsg("file_upload_status", map[string]interface{}{
			"path":  path,
			"error": fmt.Sprintf("failed to write: %v", err),
		})
		return
	}

	if data.Done {
		log.Printf("[FileTransfer] Upload complete: %s", path)
		h.sendMsg("file_upload_status", map[string]interface{}{
			"path":    path,
			"success": true,
		})
	}
}
