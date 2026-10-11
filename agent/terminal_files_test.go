package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fastremote-agent/terminal"
)

type uploadTestFile struct {
	name string
	data []byte
}

func terminalUploadRequest(t *testing.T, files ...uploadTestFile) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		part, err := writer.CreateFormFile("files", file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/terminals/files?id=legacy", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestTerminalAttachmentsPrivateFilesAndNames(t *testing.T) {
	dir := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 600)...)
	r := terminalUploadRequest(t,
		uploadTestFile{name: "../../photo $(touch unwanted).png", data: png},
		uploadTestFile{name: `C:\Users\somebody\hisobot.txt`, data: []byte("ma’lumot")},
		uploadTestFile{name: "empty.txt"},
	)
	files, err := saveTerminalAttachments(r, dir, terminalUploadLimits{file: 4096, total: 8192, count: 10})
	if err != nil || len(files) != 3 {
		t.Fatalf("upload result: %v, %v", files, err)
	}
	if files[0].MIME != "image/png" || files[1].Name != "hisobot.txt" || files[2].Size != 0 {
		t.Fatalf("unexpected file metadata: %+v", files)
	}
	for _, file := range files {
		if filepath.Dir(file.Path) != dir || !filepath.IsAbs(file.Path) || strings.ContainsAny(file.Name, "/\\$`\r\n") {
			t.Fatalf("unsafe path or filename: %+v", file)
		}
		info, err := os.Stat(file.Path)
		if err != nil || info.Mode().Perm() != 0600 || info.Size() != file.Size {
			t.Fatalf("file permissions or size: %v, %v", info, err)
		}
	}
	got, err := os.ReadFile(files[0].Path)
	if err != nil || !bytes.Equal(got, png) {
		t.Fatal("binary image content changed")
	}
	if got := terminalAttachmentName("../\r\n'\"`$(bad).png"); strings.ContainsAny(got, "\r\n'\"`$") {
		t.Fatalf("unsafe name: %q", got)
	}
}

func TestTerminalAttachmentsLimitsRollbackWholeRequest(t *testing.T) {
	tests := []struct {
		name   string
		limits terminalUploadLimits
		files  []uploadTestFile
		status int
	}{
		{"per file", terminalUploadLimits{file: 1024, total: 4096, count: 10}, []uploadTestFile{{"first.txt", []byte("saved first")}, {"large.bin", bytes.Repeat([]byte{1}, 1025)}}, 413},
		{"aggregate", terminalUploadLimits{file: 1024, total: 1100, count: 10}, []uploadTestFile{{"first.bin", bytes.Repeat([]byte{1}, 600)}, {"second.bin", bytes.Repeat([]byte{2}, 501)}}, 413},
		{"count", terminalUploadLimits{file: 1024, total: 4096, count: 1}, []uploadTestFile{{"first.txt", []byte("first")}, {"second.txt", []byte("second")}}, 413},
		{"empty request", terminalUploadLimits{file: 1024, total: 4096, count: 10}, nil, 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := saveTerminalAttachments(terminalUploadRequest(t, tt.files...), dir, tt.limits)
			var uploadErr *terminalUploadError
			if !errors.As(err, &uploadErr) || uploadErr.status != tt.status {
				t.Fatalf("wrong failure: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed request left attachments: %v, %v", entries, err)
			}
		})
	}
}

func TestTerminalAttachmentsRejectTruncatedMultipartAndBodyLimit(t *testing.T) {
	for _, limited := range []bool{false, true} {
		dir := t.TempDir()
		r := terminalUploadRequest(t, uploadTestFile{"first.txt", []byte("first")}, uploadTestFile{"second.txt", bytes.Repeat([]byte{2}, 1024)})
		body, _ := io.ReadAll(r.Body)
		if limited {
			r.Body = http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(bytes.NewReader(body)), int64(len(body)-100))
		} else {
			r.Body = io.NopCloser(bytes.NewReader(body[:len(body)-100]))
		}
		_, err := saveTerminalAttachments(r, dir, terminalUploadLimits{file: 4096, total: 8192, count: 10})
		var uploadErr *terminalUploadError
		want := 400
		if limited {
			want = 413
		}
		if !errors.As(err, &uploadErr) || uploadErr.status != want {
			t.Fatalf("limited=%v, error=%v, want status=%d", limited, err, want)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("incomplete upload left files")
		}
	}
}

type unreadableUploadBody struct{}

func (unreadableUploadBody) Read([]byte) (int, error) { panic("unauthorized body was read") }
func (unreadableUploadBody) Close() error             { return nil }

func TestTerminalUploadAuthorizationAndLegacyAvailability(t *testing.T) {
	oldSecret, oldManager, oldLive := jwtSecret, terminals, legacyBridgeLive.Load()
	defer func() { jwtSecret, terminals = oldSecret, oldManager; legacyBridgeLive.Store(oldLive) }()
	jwtSecret = []byte("isolated-upload-test-key")
	t.Setenv("LEGACY_TERMINAL_OWNER", "alice")
	t.Setenv("TERMINAL_DATA_DIR", t.TempDir())
	legacyBridgeLive.Store(true)
	alice, _ := generateToken("alice", "user")
	bob, _ := generateToken("bob", "user")
	for _, tt := range []struct {
		name, token string
		live        bool
		status      int
	}{
		{"unauthenticated", "", true, 401},
		{"other owner", bob, true, 404},
		{"legacy not running", alice, false, 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			legacyBridgeLive.Store(tt.live)
			r := httptest.NewRequest(http.MethodPost, "/api/terminals/files?id=legacy", nil)
			r.Body = unreadableUploadBody{}
			r.Header.Set("Authorization", "Bearer "+tt.token)
			w := httptest.NewRecorder()
			handleTerminalFiles(w, r)
			if w.Code != tt.status {
				t.Fatalf("status %d, want %d", w.Code, tt.status)
			}
		})
	}
	legacyBridgeLive.Store(true)
	r := terminalUploadRequest(t, uploadTestFile{"image.png", []byte("image test")})
	r.Header.Set("Authorization", "Bearer "+alice)
	w := httptest.NewRecorder()
	handleTerminalFiles(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("authorized upload failed: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Files []terminalAttachment `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Files) != 1 {
		t.Fatalf("response: %s", w.Body.String())
	}
	parent := filepath.Dir(response.Files[0].Path)
	if !strings.HasPrefix(filepath.Base(parent), "legacy-") || strings.Contains(filepath.Base(parent), "alice") {
		t.Fatalf("unsafe legacy owner directory: %s", parent)
	}
	for _, dir := range []string{parent, filepath.Dir(parent)} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("attachment directory is not private: %v %v", info, err)
		}
	}
	r = httptest.NewRequest(http.MethodGet, "/api/terminals/files?id=legacy", nil)
	r.Header.Set("Authorization", "Bearer "+alice)
	w = httptest.NewRecorder()
	handleTerminalFiles(w, r)
	if w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("unsupported method accepted")
	}
}

func TestTerminalUploadCatalogueOwnershipAndStoppedSession(t *testing.T) {
	oldSecret, oldManager := jwtSecret, terminals
	defer func() { jwtSecret, terminals = oldSecret, oldManager }()
	jwtSecret = []byte("isolated-upload-test-key")
	dir := t.TempDir()
	const id = "11111111111111111111111111111111"
	catalogue := `{"sessions":[{"id":"` + id + `","owner":"alice","title":"Stopped"}],"active":{}}`
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(catalogue), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	terminals, err = terminal.New(dir, filepath.Join(dir, "missing-tmux.sock"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"alice", "bob"} {
		token, _ := generateToken(owner, "user")
		r := httptest.NewRequest(http.MethodPost, "/api/terminals/files?id="+id, nil)
		r.Body = unreadableUploadBody{}
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handleTerminalFiles(w, r)
		want := 404
		if owner == "alice" {
			want = 409
		}
		if w.Code != want {
			t.Fatalf("owner=%s status=%d want=%d", owner, w.Code, want)
		}
	}
}
