package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"fastremote-server/models"
	"fastremote-server/store"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 10 * 1024 * 1024 // 10MB for file transfers
)

// AgentConn represents a connected agent
type AgentConn struct {
	DeviceID string
	Conn     *websocket.Conn
	Send     chan []byte     // for text messages
	SendBin  chan []byte     // for binary messages
	mu       sync.Mutex
}

// ViewerConn represents a connected viewer
type ViewerConn struct {
	Conn     *websocket.Conn
	DeviceID string
	Send     chan []byte
	SendBin  chan []byte
	mu       sync.Mutex
}

// Hub manages all WebSocket connections and message routing
type Hub struct {
	mu      sync.RWMutex
	agents  map[string]*AgentConn            // deviceID -> agent
	viewers map[string]map[*ViewerConn]bool   // deviceID -> set of viewers
	store   *store.Store
}

// New creates a new Hub
func New(s *store.Store) *Hub {
	return &Hub{
		agents:  make(map[string]*AgentConn),
		viewers: make(map[string]map[*ViewerConn]bool),
		store:   s,
	}
}

// RegisterAgent registers a new agent connection
func (h *Hub) RegisterAgent(deviceID string, conn *websocket.Conn) *AgentConn {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close existing connection if any
	if existing, ok := h.agents[deviceID]; ok {
		existing.Conn.Close()
	}

	agent := &AgentConn{
		DeviceID: deviceID,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		SendBin:  make(chan []byte, 64),
	}
	h.agents[deviceID] = agent

	// Register device in store
	h.store.RegisterDevice(&models.Device{
		ID:     deviceID,
		Status: "online",
	})

	log.Printf("[Hub] Agent registered: %s", deviceID)
	return agent
}

// UnregisterAgent removes an agent connection
func (h *Hub) UnregisterAgent(deviceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if agent, ok := h.agents[deviceID]; ok {
		close(agent.Send)
		close(agent.SendBin)
		delete(h.agents, deviceID)
	}

	// Notify viewers that the agent disconnected
	if viewers, ok := h.viewers[deviceID]; ok {
		disconnectMsg, _ := json.Marshal(models.WSMessage{
			Type:     "agent_disconnected",
			DeviceID: deviceID,
		})
		for v := range viewers {
			select {
			case v.Send <- disconnectMsg:
			default:
			}
		}
	}

	h.store.UnregisterDevice(deviceID)
	log.Printf("[Hub] Agent unregistered: %s", deviceID)
}

// RegisterViewer registers a new viewer connection for a device
func (h *Hub) RegisterViewer(deviceID string, conn *websocket.Conn) *ViewerConn {
	h.mu.Lock()
	defer h.mu.Unlock()

	viewer := &ViewerConn{
		Conn:     conn,
		DeviceID: deviceID,
		Send:     make(chan []byte, 256),
		SendBin:  make(chan []byte, 64),
	}

	if _, ok := h.viewers[deviceID]; !ok {
		h.viewers[deviceID] = make(map[*ViewerConn]bool)
	}
	h.viewers[deviceID][viewer] = true

	// Tell the agent to start streaming
	if agent, ok := h.agents[deviceID]; ok {
		startMsg, _ := json.Marshal(models.WSMessage{
			Type:     "start_stream",
			DeviceID: deviceID,
		})
		select {
		case agent.Send <- startMsg:
		default:
		}
	}

	log.Printf("[Hub] Viewer registered for device: %s", deviceID)
	return viewer
}

// UnregisterViewer removes a viewer connection
func (h *Hub) UnregisterViewer(viewer *ViewerConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if viewers, ok := h.viewers[viewer.DeviceID]; ok {
		delete(viewers, viewer)
		if len(viewers) == 0 {
			delete(h.viewers, viewer.DeviceID)
			// Tell agent to stop streaming if no viewers left
			if agent, ok := h.agents[viewer.DeviceID]; ok {
				stopMsg, _ := json.Marshal(models.WSMessage{
					Type:     "stop_stream",
					DeviceID: viewer.DeviceID,
				})
				select {
				case agent.Send <- stopMsg:
				default:
				}
			}
		}
	}

	close(viewer.Send)
	close(viewer.SendBin)
	log.Printf("[Hub] Viewer unregistered for device: %s", viewer.DeviceID)
}

// HasViewers checks if a device has any connected viewers
func (h *Hub) HasViewers(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	viewers, ok := h.viewers[deviceID]
	return ok && len(viewers) > 0
}

// ForwardToAgent sends a message from viewer to the target agent
func (h *Hub) ForwardToAgent(deviceID string, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if agent, ok := h.agents[deviceID]; ok {
		select {
		case agent.Send <- msg:
		default:
			log.Printf("[Hub] Agent send buffer full for device: %s", deviceID)
		}
	}
}

// ForwardToViewers sends a message from agent to all connected viewers
func (h *Hub) ForwardToViewers(deviceID string, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if viewers, ok := h.viewers[deviceID]; ok {
		for v := range viewers {
			select {
			case v.Send <- msg:
			default:
				log.Printf("[Hub] Viewer send buffer full")
			}
		}
	}
}

// ForwardBinaryToViewers sends binary data (screen frames) to all viewers
func (h *Hub) ForwardBinaryToViewers(deviceID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if viewers, ok := h.viewers[deviceID]; ok {
		for v := range viewers {
			select {
			case v.SendBin <- data:
			default:
				// Drop frame if buffer full (prefer latest frames)
			}
		}
	}
}

// GetAgent returns an agent by device ID
func (h *Hub) GetAgent(deviceID string) (*AgentConn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	a, ok := h.agents[deviceID]
	return a, ok
}

// RunAgentWriter pumps messages from the send channels to the WebSocket
func (h *Hub) RunAgentWriter(agent *AgentConn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		agent.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-agent.Send:
			agent.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				agent.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := agent.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case data, ok := <-agent.SendBin:
			agent.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			if err := agent.Conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			agent.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := agent.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// RunViewerWriter pumps messages from the send channels to the WebSocket
func (h *Hub) RunViewerWriter(viewer *ViewerConn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		viewer.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-viewer.Send:
			viewer.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				viewer.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := viewer.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case data, ok := <-viewer.SendBin:
			viewer.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			if err := viewer.Conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			viewer.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := viewer.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// RunAgentReader reads messages from an agent WebSocket
func (h *Hub) RunAgentReader(agent *AgentConn) {
	defer func() {
		h.UnregisterAgent(agent.DeviceID)
		agent.Conn.Close()
	}()

	agent.Conn.SetReadLimit(maxMessageSize)
	agent.Conn.SetReadDeadline(time.Now().Add(pongWait))
	agent.Conn.SetPongHandler(func(string) error {
		agent.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		messageType, message, err := agent.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[Hub] Agent read error (%s): %v", agent.DeviceID, err)
			}
			break
		}

		if messageType == websocket.BinaryMessage {
			// Binary = screen frame, forward to viewers
			h.ForwardBinaryToViewers(agent.DeviceID, message)
			continue
		}

		// Parse text message
		var wsMsg models.WSMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			log.Printf("[Hub] Invalid message from agent %s: %v", agent.DeviceID, err)
			continue
		}

		switch wsMsg.Type {
		case "system_info":
			var info models.SystemInfoPayload
			if err := json.Unmarshal(wsMsg.Payload, &info); err == nil {
				h.store.UpdateDeviceInfo(agent.DeviceID, info)
			}
		case "shell_output", "file_list_response", "file_data":
			// Forward to all viewers of this device
			h.ForwardToViewers(agent.DeviceID, message)
		default:
			log.Printf("[Hub] Unknown message type from agent: %s", wsMsg.Type)
		}
	}
}

// RunViewerReader reads messages from a viewer WebSocket
func (h *Hub) RunViewerReader(viewer *ViewerConn) {
	defer func() {
		h.UnregisterViewer(viewer)
		viewer.Conn.Close()
	}()

	viewer.Conn.SetReadLimit(maxMessageSize)
	viewer.Conn.SetReadDeadline(time.Now().Add(pongWait))
	viewer.Conn.SetPongHandler(func(string) error {
		viewer.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := viewer.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[Hub] Viewer read error: %v", err)
			}
			break
		}

		// Parse and forward to the target agent
		var wsMsg models.WSMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			log.Printf("[Hub] Invalid message from viewer: %v", err)
			continue
		}

		switch wsMsg.Type {
		case "mouse_event", "keyboard_event", "shell_input",
			"file_list", "file_download", "file_upload", "file_upload_data":
			// Forward to the agent
			h.ForwardToAgent(viewer.DeviceID, message)
		default:
			log.Printf("[Hub] Unknown message type from viewer: %s", wsMsg.Type)
		}
	}
}
