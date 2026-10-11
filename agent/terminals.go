package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"fastremote-agent/terminal"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var terminals *terminal.Manager

func registerTerminals(mux *http.ServeMux) {
	mux.HandleFunc("/api/terminals", handleTerminals)
	mux.HandleFunc("/ws/terminal", handleTerminalSocket)
	// A migration bridge keeps the already-open old shell alive during rollout.
	// It is visible only to the explicitly configured owner, never all users.
	if raw := os.Getenv("LEGACY_TERMINAL_URL"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		proxy := httputil.NewSingleHostReverseProxy(u)
		mux.HandleFunc("/ws/legacy-terminal", func(w http.ResponseWriter, r *http.Request) {
			owner, _, ok := validateToken(r.URL.Query().Get("token"))
			if !ok || owner != os.Getenv("LEGACY_TERMINAL_OWNER") {
				http.Error(w, "Unauthorized", 401)
				return
			}
			r.URL.Path = "/ws"
			proxy.ServeHTTP(w, r)
		})
	}
}
func terminalError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, terminal.ErrNotFound) {
		status = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func handleTerminals(w http.ResponseWriter, r *http.Request) {
	owner, _, valid := validateToken(r.Header.Get("Authorization"))
	if !valid || owner == "" {
		http.Error(w, "Unauthorized", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if terminals == nil {
		http.Error(w, `{"error":"Terminal xizmati mavjud emas"}`, 503)
		return
	}
	if r.Method == http.MethodGet {
		sessions, active := terminals.List(owner)
		if owner == os.Getenv("LEGACY_TERMINAL_OWNER") && os.Getenv("LEGACY_TERMINAL_URL") != "" {
			sessions = append(sessions, terminal.Session{ID: "legacy", Title: "Avvalgi terminal", Status: "legacy"})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"sessions": sessions, "activeId": active, "projects": terminal.Projects()})
		return
	}
	var req struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Active bool   `json:"active"`
		Cwd    string `json:"cwd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		terminalError(w, errors.New("noto‘g‘ri so‘rov"))
		return
	}
	switch r.Method {
	case http.MethodPost:
		s, err := terminals.CreateInDirectory(owner, req.Title, req.Cwd)
		if err != nil {
			terminalError(w, err)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(s)
	case http.MethodPatch:
		if err := terminals.Update(owner, req.ID, req.Title, req.Active); err != nil {
			terminalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	case http.MethodDelete:
		if err := terminals.Delete(owner, req.ID); err != nil {
			terminalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		w.Header().Set("Allow", "GET, POST, PATCH, DELETE")
		w.WriteHeader(405)
	}
}
func terminalSize(raw string, fallback int) uint16 {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 2 || n > 500 {
		return uint16(fallback)
	}
	return uint16(n)
}

var terminalUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}}

func handleTerminalSocket(w http.ResponseWriter, r *http.Request) {
	owner, _, valid := validateToken(r.URL.Query().Get("token"))
	if !valid || owner == "" {
		http.Error(w, "Unauthorized", 401)
		return
	}
	if terminals == nil {
		http.Error(w, "Terminal unavailable", 503)
		return
	}
	id := r.URL.Query().Get("id")
	s, err := terminals.Get(owner, id)
	if err != nil {
		terminalError(w, err)
		return
	}
	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 * 1024)
	conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	write := func(kind int, b []byte) error {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(kind, b)
	}
	if s.Status == "stopped" {
		b, e := terminals.Snapshot(owner, id)
		if e == nil {
			write(websocket.BinaryMessage, b)
		}
		write(websocket.TextMessage, []byte(`{"type":"stopped"}`))
		conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4004, "Session stopped"), time.Now().Add(time.Second))
		return
	}
	cmd, err := terminals.Attach(owner, id)
	if err != nil {
		return
	}
	cols, rows := int(terminalSize(r.URL.Query().Get("cols"), 120)), int(terminalSize(r.URL.Query().Get("rows"), 36))
	terminals.Resize(owner, id, cols, rows)
	history, _ := terminals.History(owner, id)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		write(websocket.TextMessage, []byte(`{"type":"error","message":"Terminalga ulanib bo‘lmadi"}`))
		return
	}
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}()
	if err = write(websocket.BinaryMessage, history); err != nil {
		return
	}
	// Only this goroutine writes data frames. No lossy queues or video priority.
	ready := make(chan struct{})
	go func() {
		announced := false
		buf := make([]byte, 32*1024)
		for {
			n, e := ptmx.Read(buf)
			if n > 0 {
				if write(websocket.BinaryMessage, buf[:n]) != nil {
					break
				}
				if !announced {
					if write(websocket.TextMessage, []byte(`{"type":"ready"}`)) != nil {
						break
					}
					announced = true
					close(ready)
				}
			}
			if e != nil {
				break
			}
		}
		conn.Close()
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
		return
	}
	for {
		_, b, e := conn.ReadMessage()
		if e != nil {
			return
		}
		var msg struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(b, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if len(msg.Data) <= 32768 {
				terminals.Resize(owner, id, cols, rows)
				if _, e = ptmx.Write([]byte(msg.Data)); e != nil {
					return
				}
			}
		case "resize":
			if msg.Cols >= 2 && msg.Cols <= 500 && msg.Rows >= 2 && msg.Rows <= 500 {
				cols, rows = msg.Cols, msg.Rows
				pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
				terminals.Resize(owner, id, cols, rows)
			}
		}
	}
}
