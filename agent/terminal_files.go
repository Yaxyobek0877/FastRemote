package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const terminalFileLimit int64 = 25 << 20
const terminalUploadLimit int64 = 50 << 20
const terminalUploadHeaderLimit int64 = 1 << 20
const terminalFileCountLimit = 10

type terminalAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	MIME string `json:"mime"`
}

type terminalUploadLimits struct {
	file, total int64
	count       int
}

type terminalUploadError struct {
	status  int
	message string
}

func (e *terminalUploadError) Error() string { return e.message }

func uploadError(status int, message string) error {
	return &terminalUploadError{status: status, message: message}
}

func writeTerminalUploadError(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "Faylni saqlab bo‘lmadi"
	var uploadErr *terminalUploadError
	if errors.As(err, &uploadErr) {
		status, message = uploadErr.status, uploadErr.message
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func terminalUploadReadError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return uploadError(http.StatusRequestEntityTooLarge, "Jami fayllar 50 MiB dan oshmasin")
	}
	return uploadError(http.StatusBadRequest, "Fayl yuborilishi tugallanmagan yoki noto‘g‘ri")
}

// Keep the filename recognizable while removing paths, control characters and
// shell syntax. The browser still quotes the returned path before pasting it.
func terminalAttachmentName(raw string) string {
	base := filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
	var name strings.Builder
	for _, ch := range base {
		if !unicode.IsLetter(ch) && !unicode.IsNumber(ch) && !unicode.IsMark(ch) && !strings.ContainsRune(" ._()-", ch) {
			ch = '_'
		}
		if name.Len()+len(string(ch)) > 160 {
			break
		}
		name.WriteRune(ch)
	}
	cleaned := strings.TrimSpace(name.String())
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "attachment"
	}
	return cleaned
}

func saveTerminalAttachments(r *http.Request, dir string, limits terminalUploadLimits) (files []terminalAttachment, err error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, uploadError(http.StatusBadRequest, "Fayllarni multipart/form-data shaklida yuboring")
	}
	var created []string
	complete := false
	defer func() {
		if !complete {
			for _, path := range created {
				os.Remove(path)
			}
		}
	}()
	var total int64
	parts := 0
	for {
		part, readErr := reader.NextPart()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, terminalUploadReadError(readErr)
		}
		parts++
		if parts > limits.count*2 {
			return nil, uploadError(http.StatusBadRequest, "So‘rovdagi fayl maydonlari juda ko‘p")
		}
		if part.FileName() == "" {
			if err = part.Close(); err != nil {
				return nil, terminalUploadReadError(err)
			}
			continue
		}
		if len(files) >= limits.count {
			return nil, uploadError(http.StatusRequestEntityTooLarge, "Bir martada 10 tagacha fayl yuboring")
		}
		name := terminalAttachmentName(part.FileName())
		var nonce [12]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, hex.EncodeToString(nonce[:])+"-"+name)
		file, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if openErr != nil {
			return nil, openErr
		}
		created = append(created, path)
		allowed := limits.file
		if remaining := limits.total - total; remaining < allowed {
			allowed = remaining
		}
		head := make([]byte, 512)
		headSize, readErr := io.ReadFull(part, head)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			file.Close()
			return nil, terminalUploadReadError(readErr)
		}
		head = head[:headSize]
		if int64(headSize) > allowed {
			file.Close()
			return nil, uploadError(http.StatusRequestEntityTooLarge, "Fayl 25 MiB, jami fayllar 50 MiB dan oshmasin")
		}
		if _, err = file.Write(head); err != nil {
			file.Close()
			return nil, err
		}
		copied, copyErr := io.Copy(file, io.LimitReader(part, allowed-int64(headSize)+1))
		size := int64(headSize) + copied
		if copyErr != nil {
			file.Close()
			return nil, terminalUploadReadError(copyErr)
		}
		if size > allowed {
			file.Close()
			return nil, uploadError(http.StatusRequestEntityTooLarge, "Fayl 25 MiB, jami fayllar 50 MiB dan oshmasin")
		}
		if err = file.Sync(); err != nil {
			file.Close()
			return nil, err
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
		if err = part.Close(); err != nil {
			return nil, terminalUploadReadError(err)
		}
		files = append(files, terminalAttachment{Name: name, Path: path, Size: size, MIME: http.DetectContentType(head)})
		total += size
	}
	if len(files) == 0 {
		return nil, uploadError(http.StatusBadRequest, "Kamida bitta fayl tanlang")
	}
	// Persist directory entries too, so a completed upload survives a restart.
	directory, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	complete = true
	return files, nil
}

func handleTerminalFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	owner, _, valid := validateToken(r.Header.Get("Authorization"))
	if !valid || owner == "" {
		writeTerminalUploadError(w, uploadError(http.StatusUnauthorized, "Hisobga qayta kiring"))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeTerminalUploadError(w, uploadError(http.StatusMethodNotAllowed, "Faqat POST so‘rovi qo‘llanadi"))
		return
	}
	id := r.URL.Query().Get("id")
	dirID := id
	if id == "legacy" {
		if owner != os.Getenv("LEGACY_TERMINAL_OWNER") || !legacyBridgeLive.Load() {
			writeTerminalUploadError(w, uploadError(http.StatusNotFound, "Terminal topilmadi"))
			return
		}
		hash := sha256.Sum256([]byte(owner))
		dirID = "legacy-" + hex.EncodeToString(hash[:12])
	} else {
		if terminals == nil {
			writeTerminalUploadError(w, uploadError(http.StatusServiceUnavailable, "Terminal xizmati mavjud emas"))
			return
		}
		session, err := terminals.Get(owner, id)
		if err != nil {
			writeTerminalUploadError(w, uploadError(http.StatusNotFound, "Terminal topilmadi"))
			return
		}
		if session.Status != "running" {
			writeTerminalUploadError(w, uploadError(http.StatusConflict, "Avval terminalni qayta oching"))
			return
		}
	}
	if dirID == "" || filepath.Base(dirID) != dirID || dirID == "." || dirID == ".." {
		writeTerminalUploadError(w, uploadError(http.StatusNotFound, "Terminal topilmadi"))
		return
	}
	if r.ContentLength > terminalUploadLimit+terminalUploadHeaderLimit {
		writeTerminalUploadError(w, uploadError(http.StatusRequestEntityTooLarge, "Jami fayllar 50 MiB dan oshmasin"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, terminalUploadLimit+terminalUploadHeaderLimit)
	defer r.Body.Close()
	root, err := filepath.Abs(filepath.Join(getEnv("TERMINAL_DATA_DIR", "data/terminals"), "attachments"))
	if err != nil {
		writeTerminalUploadError(w, err)
		return
	}
	dir := filepath.Join(root, dirID)
	for _, path := range []string{root, dir} {
		if err = os.MkdirAll(path, 0700); err == nil {
			err = os.Chmod(path, 0700)
		}
		if err != nil {
			writeTerminalUploadError(w, err)
			return
		}
	}
	files, err := saveTerminalAttachments(r, dir, terminalUploadLimits{file: terminalFileLimit, total: terminalUploadLimit, count: terminalFileCountLimit})
	if err != nil {
		writeTerminalUploadError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"files": files})
}
