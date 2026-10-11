package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fastremote-agent/terminal"
	"github.com/gorilla/websocket"
)

func TestTerminalSocketReconnectAndAuthorization(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("", "fr-ws-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "tmux.sock")
	config, _ := filepath.Abs("../deploy/terminal.tmux.conf")
	if b, err := exec.Command("tmux", "-f", config, "-S", socket, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %s %v", b, err)
	}
	defer exec.Command("tmux", "-S", socket, "kill-server").Run()
	oldManager, oldSecret := terminals, jwtSecret
	defer func() { terminals = oldManager; jwtSecret = oldSecret }()
	terminals, err = terminal.New(filepath.Join(dir, "data"), socket)
	if err != nil {
		t.Fatal(err)
	}
	jwtSecret = []byte("isolated-test-signing-key")
	token, _ := generateToken("alice", "user")
	other, _ := generateToken("bob", "user")
	s, err := terminals.Create("alice", "Socket test")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerTerminals(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/terminal?id=" + s.ID + "&cols=90&rows=24&token="
	if c, res, e := websocket.DefaultDialer.Dial(endpoint+url.QueryEscape(other), nil); e == nil {
		c.Close()
		t.Fatal("other owner attached")
	} else if res.StatusCode != 404 {
		t.Fatalf("owner status %d", res.StatusCode)
	}
	if c, _, e := websocket.DefaultDialer.Dial(endpoint+url.QueryEscape(token), http.Header{"Origin": []string{"https://evil.example"}}); e == nil {
		c.Close()
		t.Fatal("cross-origin socket accepted")
	}
	c, _, err := websocket.DefaultDialer.Dial(endpoint+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil := func(c *websocket.Conn, needle string) {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		var buf []byte
		for !bytes.Contains(buf, []byte(needle)) {
			_, b, e := c.ReadMessage()
			if e != nil {
				t.Fatalf("missing %q: %v (%q)", needle, e, buf)
			}
			buf = append(buf, b...)
		}
	}
	readUntil(c, "$ ")
	c.WriteJSON(map[string]interface{}{"type": "resize", "cols": 91, "rows": 25})
	script := base64.StdEncoding.EncodeToString([]byte("printf '\\nWS_FIRST_世界\\n'; sleep 0.3; printf '\\nWS_OFFLINE_DONE\\n'"))
	c.WriteJSON(map[string]string{"type": "input", "data": "eval \"$(printf %s " + script + " | base64 -d)\"\r"})
	readUntil(c, "WS_FIRST_世界")
	c.Close()
	time.Sleep(450 * time.Millisecond)
	c, _, err = websocket.DefaultDialer.Dial(endpoint+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(c, "WS_OFFLINE_DONE")
	c.Close()
	// Authenticated catalogue and unauthenticated denial.
	req, _ := http.NewRequest("GET", server.URL+"/api/terminals", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var result struct {
		Sessions []terminal.Session `json:"sessions"`
	}
	json.NewDecoder(res.Body).Decode(&result)
	if len(result.Sessions) != 1 || result.Sessions[0].ID != s.ID {
		t.Fatal("catalogue mismatch")
	}
	res2, err := http.Get(server.URL + "/api/terminals")
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != 401 {
		t.Fatal("unauthenticated catalogue exposed")
	}
}
