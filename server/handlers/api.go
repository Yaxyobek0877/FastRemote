package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"fastremote-server/auth"
	"fastremote-server/models"
	"fastremote-server/store"
)

// APIHandler handles REST API endpoints
type APIHandler struct {
	store *store.Store
}

// NewAPIHandler creates a new APIHandler
func NewAPIHandler(s *store.Store) *APIHandler {
	return &APIHandler{store: s}
}

// Login handles POST /api/login
func (h *APIHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Error: "method not allowed"})
		return
	}

	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "username and password required"})
		return
	}

	user, ok := h.store.GetUser(req.Username)
	if !ok || !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeJSON(w, http.StatusUnauthorized, models.ErrorResponse{Error: "invalid credentials"})
		return
	}

	token, err := auth.GenerateToken(req.Username)
	if err != nil {
		log.Printf("[API] Token generation error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal server error"})
		return
	}

	log.Printf("[API] User logged in: %s", req.Username)
	writeJSON(w, http.StatusOK, models.LoginResponse{
		Token:    token,
		Username: req.Username,
	})
}

// Devices handles GET /api/devices
func (h *APIHandler) Devices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, models.ErrorResponse{Error: "method not allowed"})
		return
	}

	// Validate JWT
	tokenStr := extractToken(r)
	if tokenStr == "" {
		writeJSON(w, http.StatusUnauthorized, models.ErrorResponse{Error: "authorization required"})
		return
	}

	_, err := auth.ValidateToken(tokenStr)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, models.ErrorResponse{Error: "invalid or expired token"})
		return
	}

	devices := h.store.GetAllDevices()
	writeJSON(w, http.StatusOK, devices)
}

// extractToken extracts JWT from Authorization header or query param
func extractToken(r *http.Request) string {
	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Check query param
	return r.URL.Query().Get("token")
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
