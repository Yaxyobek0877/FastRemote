package handlers

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"

	"fastremote-server/auth"
	"fastremote-server/hub"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 64,
	WriteBufferSize: 1024 * 64,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for MVP
	},
}

// WSHandler handles WebSocket upgrade endpoints
type WSHandler struct {
	hub *hub.Hub
}

// NewWSHandler creates a new WSHandler
func NewWSHandler(h *hub.Hub) *WSHandler {
	return &WSHandler{hub: h}
}

// AgentWS handles GET /ws/agent — agent connects here
func (h *WSHandler) AgentWS(w http.ResponseWriter, r *http.Request) {
	// Validate agent key
	agentKey := os.Getenv("AGENT_KEY")
	if agentKey == "" {
		agentKey = "fastremote-agent-key-change-me"
	}

	key := r.URL.Query().Get("key")
	if key != agentKey {
		http.Error(w, "invalid agent key", http.StatusUnauthorized)
		log.Printf("[WS] Agent connection rejected: invalid key")
		return
	}

	deviceID := r.URL.Query().Get("deviceId")
	if deviceID == "" {
		http.Error(w, "deviceId required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Agent upgrade error: %v", err)
		return
	}

	log.Printf("[WS] Agent connected: %s from %s", deviceID, r.RemoteAddr)

	agent := h.hub.RegisterAgent(deviceID, conn)

	// Start reader and writer goroutines
	go h.hub.RunAgentWriter(agent)
	h.hub.RunAgentReader(agent) // blocks until disconnect
}

// ViewerWS handles GET /ws/viewer/{deviceId} — viewer connects here
func (h *WSHandler) ViewerWS(w http.ResponseWriter, r *http.Request) {
	// Validate JWT
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 {
				tokenStr = parts[1]
			}
		}
	}

	if tokenStr == "" {
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return
	}

	_, err := auth.ValidateToken(tokenStr)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	// Extract device ID from URL path: /ws/viewer/{deviceId}
	path := strings.TrimPrefix(r.URL.Path, "/ws/viewer/")
	deviceID := strings.TrimSuffix(path, "/")
	if deviceID == "" {
		http.Error(w, "device ID required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Viewer upgrade error: %v", err)
		return
	}

	log.Printf("[WS] Viewer connected for device: %s from %s", deviceID, r.RemoteAddr)

	viewer := h.hub.RegisterViewer(deviceID, conn)

	// Start reader and writer goroutines
	go h.hub.RunViewerWriter(viewer)
	h.hub.RunViewerReader(viewer) // blocks until disconnect
}
