package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"fastremote-agent/capture"
	"fastremote-agent/filetransfer"
	"fastremote-agent/input"
	"fastremote-agent/shell"
)

//go:embed dist
var embeddedFiles embed.FS

// WSMessage matches the server message format
type WSMessage struct {
	Type     string          `json:"type"`
	DeviceID string          `json:"deviceId,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type MouseEvent struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Button    string  `json:"button"`
	Action    string  `json:"action"`
	DeltaY    float64 `json:"deltaY"`
	MovementX float64 `json:"movementX"`
	MovementY float64 `json:"movementY"`
}

type KeyboardEvent struct {
	Key    string `json:"key"`
	Code   string `json:"code"`
	Action string `json:"action"`
	Ctrl   bool   `json:"ctrl"`
	Alt    bool   `json:"alt"`
	Shift  bool   `json:"shift"`
	Meta   bool   `json:"meta"`
}

type ShellInput struct {
	Data string `json:"data"`
}

type ShellResize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type FileListRequest struct {
	Path string `json:"path"`
}

type FileDownloadRequest struct {
	Path string `json:"path"`
}

type FileDeleteRequest struct {
	Path string `json:"path"`
}

type FileMkdirRequest struct {
	Path string `json:"path"`
}

type FileRenameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

type QualitySettings struct {
	FPS       int `json:"fps"`
	Quality   int `json:"quality"`
	MaxWidth  int `json:"maxWidth"`
	MaxHeight int `json:"maxHeight"`
}

type SystemInfo struct {
	DeviceName   string `json:"deviceName"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Hostname     string `json:"hostname"`
	IP           string `json:"ip"`
	ScreenWidth  int    `json:"screenWidth"`
	ScreenHeight int    `json:"screenHeight"`
}

// wsMsg is a message for the per-viewer write channels
type wsMsg struct {
	msgType int    // websocket.BinaryMessage or websocket.TextMessage
	data    []byte
}

type Viewer struct {
	FrameChan chan []byte  // video frames (high priority)
	JsonChan  chan wsMsg   // JSON messages (cursor, shell output, etc.)
	WriteMu   *sync.Mutex // fallback mutex for direct writes (pre-registration)
	Done      chan struct{}
}

var (
	capturer      *capture.Capturer
	cursorTracker *capture.CursorTracker
	audioCapturer *capture.AudioCapturer
	inputH        *input.Handler
	shellSess     *shell.Shell
	deviceID      string
	userStore     *UserStore
	startTime     = time.Now() // agent boshlangan vaqt (uptime hisoblash uchun)

	viewersMu     sync.RWMutex
	directViewers map[*websocket.Conn]*Viewer
)

const appVersion = "1.0.0"

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 64,
	WriteBufferSize: 1024 * 64,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for standalone usage
	},
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("=== FastRemote Standalone Agent ===")

	// Configuration
	adminPassword := getEnv("ADMIN_PASSWORD", "admin123")
	port := getEnv("PORT", "9090")
	envDeviceName := getEnv("DEVICE_NAME", "")

	// Initialize JWT (persistent secret)
	initJWTSecret()

	// Initialize User Store (file-based, bcrypt encrypted)
	userStore = NewUserStore("users.json")
	userStore.EnsureDefaultAdmin(adminPassword)
	log.Printf("[Auth] User store initialized (%d users)", len(userStore.ListUsers()))

	// Generate device ID from hostname
	hostname, _ := os.Hostname()
	if envDeviceName != "" {
		deviceName = envDeviceName
	} else {
		deviceName = hostname
	}
	deviceID = generateDeviceID(hostname)

	// Initialize components — native resolution, high quality
	capturer = capture.New(15, 85, 0, 0) // 0,0 = auto-detect (up to 4K)
	cursorTracker = capture.NewCursorTracker(capturer.ActualWidth, capturer.ActualHeight)
	audioCapturer = capture.NewAudioCapturer()
	inputH = input.New()
	startMouseCoalescer()
	directViewers = make(map[*websocket.Conn]*Viewer)

	log.Printf("Device: %s", deviceName)
	log.Printf("IP Address: %s", getLocalIP())
	log.Printf("Screen: %dx%d", capturer.ActualWidth, capturer.ActualHeight)
	log.Printf("Starting standalone server on port %s", port)

	// Start system resource sampler (CPU/RAM/Disk usage)
	go startStatsSampler()
	// Start frame distribution goroutine
	go frameDistributor()
	// Start cursor distribution goroutine
	go cursorDistributor()
	// Start audio distribution goroutine
	go audioDistributor()

	// Start HTTP Server
	startServer(port, deviceName, hostname)
}

// ============================================================
// HTTP SERVER 
// ============================================================

func startServer(port, dn, hostname string) {
	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/device-info", handleDeviceInfo)
	mux.HandleFunc("/api/settings", handleSettings)
	mux.HandleFunc("/api/change-password", handleChangePassword)
	mux.HandleFunc("/api/users", handleUsers)
	mux.HandleFunc("/api/users/reset-password", handleResetPassword)
	mux.HandleFunc("/api/server-info", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		_, _, valid := validateToken(token)
		if !valid {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"port":       port,
			"screenW":    capturer.ActualWidth,
			"screenH":    capturer.ActualHeight,
			"deviceName": deviceName,
			"hostname":   hostname,
		})
	})

	// WebSocket Endpoint
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, hostname)
	})

	// WebRTC signaling
	mux.HandleFunc("/ws/webrtc", handleWebRTCSignaling)

	// Serve Static Embedded Files
	distFS, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		log.Fatalf("Failed to initialize embedded static files: %v", err)
	}

	// SPA File Server Handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to read the file
		f, err := distFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err == nil {
			f.Close()
			http.FileServer(http.FS(distFS)).ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for client-side routing
		b, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(b)
	})

	handler := corsMiddleware(mux)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Printf("[Server] Error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================
// API HANDLERS
// ============================================================

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"Username and password required"}`, http.StatusBadRequest)
		return
	}

	user, ok := userStore.Authenticate(req.Username, req.Password)
	if !ok {
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	token, err := generateToken(user.Username, user.Role)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token":      token,
		"username":   user.Username,
		"role":       user.Role,
		"deviceName": deviceName,
	})
}

func handleDeviceInfo(w http.ResponseWriter, r *http.Request) {
	// Public qurilma holati — login talab qilinmaydi (home page uchun)
	uptime := time.Since(startTime)
	viewersMu.RLock()
	viewerCount := len(directViewers)
	viewersMu.RUnlock()
	stats := getStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deviceName":      deviceName,
		"hostname":        func() string { h, _ := os.Hostname(); return h }(),
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
		"cpuCores":        runtime.NumCPU(),
		"goroutines":      runtime.NumGoroutine(),
		"ip":              getLocalIP(),
		"screenWidth":     capturer.ActualWidth,
		"screenHeight":    capturer.ActualHeight,
		"online":          true,
		"viewers":         viewerCount,
		"uptimeSeconds":   int64(uptime.Seconds()),
		"version":         appVersion,
		"serverTime":      time.Now().Format(time.RFC3339),
		"cpuPercent":     stats.CPUPercent,
		"memUsed":        stats.MemUsed,
		"memTotal":       stats.MemTotal,
		"memUsedPercent": stats.MemUsedPercent,
		"disks":          stats.Disks,
		"gpus":           stats.GPUs,
		"statsSupported": stats.Supported,
	})
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	username, _, valid := validateToken(token)
	if !valid {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"deviceName": deviceName,
			"username":   username,
			"users":      userStore.ListUsers(),
		})
	case http.MethodPost:
		var req struct {
			DeviceName string `json:"deviceName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.DeviceName != "" {
			deviceName = req.DeviceName
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "deviceName": deviceName})
	default:
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	token := r.Header.Get("Authorization")
	username, _, valid := validateToken(token)
	if !valid {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	if err := userStore.ChangePassword(username, req.CurrentPassword, req.NewPassword); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	_, role, valid := validateToken(token)
	if !valid {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if role != "admin" {
		http.Error(w, `{"error":"Admin access required"}`, http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(userStore.ListUsers())
	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
			return
		}
		if err := userStore.AddUser(req.Username, req.Password, req.Role); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	case http.MethodDelete:
		var req struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
			return
		}
		if err := userStore.DeleteUser(req.Username); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	default:
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	token := r.Header.Get("Authorization")
	_, role, valid := validateToken(token)
	if !valid {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if role != "admin" {
		http.Error(w, `{"error":"Admin access required"}`, http.StatusForbidden)
		return
	}

	var req struct {
		Username    string `json:"username"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}
	if err := userStore.AdminResetPassword(req.Username, req.NewPassword); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ============================================================
// WEBSOCKET HANDLER
// ============================================================

func handleWebSocket(w http.ResponseWriter, r *http.Request, hostname string) {
	// Authenticate WebSocket connection
	token := r.URL.Query().Get("token")
	_, _, valid := validateToken(token)
	if !valid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		log.Printf("[WS] Unauthorized connection attempt from %s", r.RemoteAddr)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}

	log.Printf("[WS] Viewer connected from %s", r.RemoteAddr)

	// Send initial system info
	sendSystemInfo(conn, deviceName, hostname)

	// Register viewer
	v := &Viewer{
		FrameChan: make(chan []byte, 3),  // video frames (high priority)
		JsonChan:  make(chan wsMsg, 30),  // JSON messages (lower priority)
		WriteMu:   &sync.Mutex{},
		Done:      make(chan struct{}),
	}
	viewersMu.Lock()
	directViewers[conn] = v
	viewerCount := len(directViewers)
	viewersMu.Unlock()

	// PRIORITY WRITER: frames always take priority over JSON messages.
	// This prevents cursor/shell updates from starving frame delivery.
	go func(c *websocket.Conn, viewer *Viewer) {
		defer func() {
			select {
			case <-viewer.Done:
			default:
			}
		}()
		for {
			// Priority 1: always try to send a frame first
			select {
			case frame, ok := <-viewer.FrameChan:
				if !ok {
					return
				}
				c.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if err := c.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					return
				}
				continue // Check for more frames before processing JSON
			default:
			}

			// Priority 2: no frame pending — wait for either frame or JSON
			select {
			case frame, ok := <-viewer.FrameChan:
				if !ok {
					return
				}
				c.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if err := c.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					return
				}
			case msg, ok := <-viewer.JsonChan:
				if !ok {
					return
				}
				c.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if err := c.WriteMessage(msg.msgType, msg.data); err != nil {
					return
				}
			case <-viewer.Done:
				return
			}
		}
	}(conn, v)

	// Start screen capture and cursor tracking if this is the first viewer
	if viewerCount == 1 {
		log.Println("[WS] First viewer connected, starting screen capture + cursor tracking...")
		capturer.Start()
		cursorTracker.Start()
	}

	// Create file handler for this viewer
	viewerFileH := filetransfer.New(func(msgType string, payload interface{}) {
		sendJSONToConn(conn, msgType, payload)
	})

	// Read messages from viewer — with keepalive
	conn.SetReadLimit(10 * 1024 * 1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping ticker
	pingTicker := time.NewTicker(30 * time.Second)
	go func() {
		defer pingTicker.Stop()
		for range pingTicker.C {
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("[WS] Invalid message: %v", err)
			continue
		}

		// Handle viewer messages
		handleViewerMessage(msg, conn, viewerFileH)
	}

	// Unregister viewer
	pingTicker.Stop()
	viewersMu.Lock()
	if v, ok := directViewers[conn]; ok {
		delete(directViewers, conn)
		close(v.Done)      // Signal the priority writer to stop
		close(v.FrameChan) // Stop frame delivery
		close(v.JsonChan)  // Stop JSON delivery
	}
	viewerCount = len(directViewers)
	viewersMu.Unlock()
	conn.Close()

	log.Printf("[WS] Viewer disconnected, %d viewers remaining", viewerCount)

	// Stop capture if no viewers
	if viewerCount == 0 {
		log.Println("[WS] No viewers left, stopping screen capture + cursor tracking...")
		capturer.Stop()
		cursorTracker.Stop()
	}
}

func handleViewerMessage(msg WSMessage, viewerConn *websocket.Conn, fileHandler *filetransfer.Handler) {
	switch msg.Type {
	case "mouse_event":
		var event MouseEvent
		if err := json.Unmarshal(msg.Payload, &event); err == nil {
			handleMouseEvent(event)
		}

	case "keyboard_event":
		var event KeyboardEvent
		if err := json.Unmarshal(msg.Payload, &event); err == nil {
			handleKeyboardEvent(event)
		}

	case "shell_input":
		var input ShellInput
		if err := json.Unmarshal(msg.Payload, &input); err == nil {
			handleShellInput(input.Data, viewerConn)
		}

	case "shell_resize":
		var resize ShellResize
		if err := json.Unmarshal(msg.Payload, &resize); err == nil {
			handleShellResize(resize, viewerConn)
		}

	case "quality_settings":
		var settings QualitySettings
		if err := json.Unmarshal(msg.Payload, &settings); err == nil {
			handleQualitySettings(settings)
		}

	case "file_list":
		var req FileListRequest
		if err := json.Unmarshal(msg.Payload, &req); err == nil {
			go fileHandler.ListDirectory(req.Path)
		}

	case "file_download":
		var req FileDownloadRequest
		if err := json.Unmarshal(msg.Payload, &req); err == nil {
			go fileHandler.DownloadFile(req.Path)
		}

	case "file_upload_data":
		go fileHandler.UploadFileChunk(msg.Payload)

	case "file_delete":
		var req FileDeleteRequest
		if err := json.Unmarshal(msg.Payload, &req); err == nil {
			go fileHandler.DeletePath(req.Path)
		}

	case "file_mkdir":
		var req FileMkdirRequest
		if err := json.Unmarshal(msg.Payload, &req); err == nil {
			go fileHandler.CreateDirectory(req.Path)
		}

	case "file_rename":
		var req FileRenameRequest
		if err := json.Unmarshal(msg.Payload, &req); err == nil {
			go fileHandler.RenamePath(req.OldPath, req.NewPath)
		}

	case "audio_start":
		handleAudioStart()

	case "audio_stop":
		handleAudioStop()

	default:
		log.Printf("[WS] Unknown message type: %s", msg.Type)
	}
}

// ============================================================
// FRAME DISTRIBUTOR
// ============================================================

func frameDistributor() {
	for frame := range capturer.Frames() {
		// Send to all viewers with "latest frame swap" — drain stale, push fresh
		viewersMu.RLock()
		for _, v := range directViewers {
			// Drain any stale frame first
			select {
			case <-v.FrameChan:
			default:
			}
			// Push new frame
			select {
			case v.FrameChan <- frame:
			default:
			}
		}
		viewersMu.RUnlock()
	}
}

// ============================================================
// CURSOR DISTRIBUTOR
// ============================================================

func cursorDistributor() {
	for cursorInfo := range cursorTracker.Updates() {
		payloadBytes, _ := json.Marshal(cursorInfo)
		msg := WSMessage{
			Type:     "cursor_position",
			DeviceID: deviceID,
			Payload:  payloadBytes,
		}
		data, _ := json.Marshal(msg)

		viewersMu.RLock()
		for _, v := range directViewers {
			select {
			case v.JsonChan <- wsMsg{msgType: websocket.TextMessage, data: data}:
			default:
			}
		}
		viewersMu.RUnlock()
	}
}

// ============================================================
// AUDIO DISTRIBUTOR
// ============================================================

func audioDistributor() {
	for chunk := range audioCapturer.Chunks() {
		viewersMu.RLock()
		for _, v := range directViewers {
			audioMsg := make([]byte, 1+len(chunk))
			audioMsg[0] = 'A'
			copy(audioMsg[1:], chunk)

			select {
			case v.JsonChan <- wsMsg{msgType: websocket.BinaryMessage, data: audioMsg}:
			default:
			}
		}
		viewersMu.RUnlock()
	}
}

func handleAudioStart() {
	log.Println("[Audio] Starting audio capture...")
	audioCapturer.Start()
}

func handleAudioStop() {
	log.Println("[Audio] Stopping audio capture...")
	audioCapturer.Stop()
}

// Mouse move coalescing for smooth movement
var (
	pendingMouseX, pendingMouseY float64
	mouseMovePending             bool
	mouseMoveMu                  sync.Mutex
	// Relative mode accumulators
	pendingRelDX int
	pendingRelDY int
	relativeMode bool
)

func startMouseCoalescer() {
	// Mouse coalescer: executes latest mouse position at 125Hz
	go func() {
		ticker := time.NewTicker(4 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			mouseMoveMu.Lock()
			if mouseMovePending {
				if relativeMode {
					dx, dy := pendingRelDX, pendingRelDY
					pendingRelDX = 0
					pendingRelDY = 0
					mouseMovePending = false
					mouseMoveMu.Unlock()
					inputH.MouseMoveRelative(dx, dy)
				} else {
					x, y := pendingMouseX, pendingMouseY
					mouseMovePending = false
					mouseMoveMu.Unlock()
					inputH.MouseMove(x, y)
				}
			} else {
				mouseMoveMu.Unlock()
			}
		}
	}()
}

func handleMouseEvent(event MouseEvent) {
	// Force frame capture to bypass delta detection during mouse interaction
	capturer.ForceNextFrame()

	switch event.Action {
	case "move":
		// Use relative mode if movementX/Y are provided
		if event.MovementX != 0 || event.MovementY != 0 {
			mouseMoveMu.Lock()
			pendingRelDX += int(event.MovementX)
			pendingRelDY += int(event.MovementY)
			relativeMode = true
			mouseMovePending = true
			mouseMoveMu.Unlock()
		} else {
			mouseMoveMu.Lock()
			pendingMouseX = event.X
			pendingMouseY = event.Y
			relativeMode = false
			mouseMovePending = true
			mouseMoveMu.Unlock()
		}
	case "click":
		log.Printf("[MouseDbg] click btn=%s x=%.3f y=%.3f", event.Button, event.X, event.Y)
		inputH.MouseClick(event.X, event.Y, event.Button)
	case "dblclick":
		log.Printf("[MouseDbg] dblclick btn=%s x=%.3f y=%.3f", event.Button, event.X, event.Y)
		inputH.MouseDoubleClick(event.X, event.Y, event.Button)
	case "down":
		log.Printf("[MouseDbg] down btn=%s x=%.3f y=%.3f", event.Button, event.X, event.Y)
		inputH.MouseDown(event.X, event.Y, event.Button)
	case "up":
		log.Printf("[MouseDbg] up btn=%s x=%.3f y=%.3f", event.Button, event.X, event.Y)
		inputH.MouseUp(event.X, event.Y, event.Button)
	case "scroll":
		inputH.MouseScroll(event.X, event.Y, event.DeltaY)
	}
}

func handleKeyboardEvent(event KeyboardEvent) {
	// Force frame capture to ensure screen updates after keyboard input
	capturer.ForceNextFrame()

	switch event.Action {
	case "keydown":
		if event.Ctrl || event.Alt || event.Meta {
			// Modifier combos (Ctrl+C, Alt+Tab, etc.) — use KeyPress with modifiers
			inputH.KeyPress(event.Key, event.Code, event.Ctrl, event.Alt, event.Shift, event.Meta)
		} else if len(event.Key) == 1 {
			// Single printable character (a, A, ?, !, @, etc.)
			// Use TypeChar for accurate input — handles all shifted chars correctly
			inputH.TypeChar(event.Key)
		} else {
			// Special keys (Enter, Backspace, Arrow keys, F-keys, etc.)
			if event.Shift {
				inputH.KeyPress(event.Key, event.Code, false, false, true, false)
			} else {
				inputH.KeyDown(event.Key, event.Code)
			}
		}
	case "keyup":
		// Only send keyup for special keys (multi-char key names)
		// Single characters are handled by TypeChar (press+release in one call)
		if len(event.Key) > 1 && !event.Ctrl && !event.Alt && !event.Meta {
			inputH.KeyUp(event.Key, event.Code)
		}
	case "keypress":
		inputH.KeyPress(event.Key, event.Code, event.Ctrl, event.Alt, event.Shift, event.Meta)
	}
}

func ensureShellStarted(viewerConn *websocket.Conn) {
	if shellSess == nil || !shellSess.IsActive() {
		var err error
		shellSess, err = shell.New()
		if err != nil {
			log.Printf("Failed to start shell: %v", err)
			errorMsg := "Error: " + err.Error() + "\r\n"
			if viewerConn != nil {
				sendJSONToConn(viewerConn, "shell_output", map[string]string{"data": errorMsg})
			}
			return
		}
		go shellOutputForwarder()
	}
}

func handleShellInput(data string, viewerConn *websocket.Conn) {
	ensureShellStarted(viewerConn)
	if shellSess != nil && shellSess.IsActive() {
		shellSess.Write(data)
	}
}

func handleShellResize(resize ShellResize, viewerConn *websocket.Conn) {
	ensureShellStarted(viewerConn)
	if shellSess != nil && shellSess.IsActive() {
		if resize.Cols > 0 && resize.Rows > 0 {
			shellSess.Resize(uint16(resize.Cols), uint16(resize.Rows))
		}
	}
}

func handleQualitySettings(settings QualitySettings) {
	log.Printf("[Quality] Received settings: FPS=%d, Quality=%d, Resolution=%dx%d",
		settings.FPS, settings.Quality, settings.MaxWidth, settings.MaxHeight)
	capturer.SetSettings(settings.FPS, settings.Quality, settings.MaxWidth, settings.MaxHeight)

	// Send ack with current settings
	fps, quality, maxW, maxH := capturer.GetSettings()
	viewersMu.RLock()
	for viewer := range directViewers {
		// sendJSONToConn handles its own locking for the conn
		sendJSONToConn(viewer, "quality_ack", map[string]int{
			"fps": fps, "quality": quality, "maxWidth": maxW, "maxHeight": maxH,
		})
	}
	viewersMu.RUnlock()
}

func shellOutputForwarder() {
	for output := range shellSess.Output() {
		payload := map[string]string{"data": output}

		// Send shell output to ALL viewers
		viewersMu.RLock()
		for viewer := range directViewers {
			sendJSONToConn(viewer, "shell_output", payload)
		}
		viewersMu.RUnlock()
	}
}

// ============================================================
// MESSAGE SENDING HELPERS
// ============================================================

func sendJSONToConn(conn *websocket.Conn, msgType string, payload interface{}) {
	if conn == nil {
		return
	}
	payloadBytes, _ := json.Marshal(payload)
	msg := WSMessage{
		Type:     msgType,
		DeviceID: deviceID,
		Payload:  payloadBytes,
	}
	data, _ := json.Marshal(msg)

	viewersMu.RLock()
	v, ok := directViewers[conn]
	viewersMu.RUnlock()

	if ok && v.JsonChan != nil {
		// Route through JSON write channel (non-blocking)
		select {
		case v.JsonChan <- wsMsg{msgType: websocket.TextMessage, data: data}:
		default:
		}
	}
}

func sendSystemInfo(conn *websocket.Conn, deviceName, hostname string) {
	info := SystemInfo{
		DeviceName:   deviceName,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Hostname:     hostname,
		IP:           getLocalIP(),
		ScreenWidth:  capturer.ActualWidth,
		ScreenHeight: capturer.ActualHeight,
	}
	// Direct write — called before viewer registration, no concurrent writers yet
	payloadBytes, _ := json.Marshal(info)
	msg := WSMessage{
		Type:     "system_info",
		DeviceID: deviceID,
		Payload:  payloadBytes,
	}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)
}

// ============================================================
// UTILITIES
// ============================================================

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "unknown"
}

func generateDeviceID(hostname string) string {
	id := hostname + "-" + runtime.GOOS
	result := ""
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			result += string(c)
		}
	}
	if result == "" {
		result = "unknown-device"
	}
	return result
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
