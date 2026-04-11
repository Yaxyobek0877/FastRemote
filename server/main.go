package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fastremote-server/handlers"
	"fastremote-server/hub"
	"fastremote-server/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("=== FastRemote Server ===")

	// Initialize store & hub
	s := store.New()
	h := hub.New(s)

	// Create handlers
	apiHandler := handlers.NewAPIHandler(s)
	wsHandler := handlers.NewWSHandler(h)

	// Setup routes
	mux := http.NewServeMux()

	// REST API
	mux.HandleFunc("/api/login", apiHandler.Login)
	mux.HandleFunc("/api/devices", apiHandler.Devices)

	// WebSocket endpoints
	mux.HandleFunc("/ws/agent", wsHandler.AgentWS)
	mux.HandleFunc("/ws/viewer/", wsHandler.ViewerWS)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	// Configure server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	log.Printf("Server starting on port %s", port)
	log.Printf("Default admin credentials: admin / admin123")
	log.Printf("Agent key: %s", getAgentKey())

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}

func getAgentKey() string {
	key := os.Getenv("AGENT_KEY")
	if key == "" {
		key = "fastremote-agent-key-change-me"
	}
	return key
}

// corsMiddleware adds CORS headers for the React dev server
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Don't set write timeout for WebSocket connections
		if strings.HasPrefix(r.URL.Path, "/ws/") {
			// WebSocket connections shouldn't have a write deadline set by the server
		}

		next.ServeHTTP(w, r)
	})
}
