package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

var legacyBridgeLive atomic.Bool

const legacyHistoryLimit = 2 * 1024 * 1024

var codexResumePattern = regexp.MustCompile(`^codex resume [a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type legacyCheckpoint struct {
	BootID        string    `json:"bootId"`
	Owner         string    `json:"owner"`
	ID            string    `json:"id"`
	Cwd           string    `json:"cwd"`
	ResumeCommand string    `json:"resumeCommand,omitempty"`
	ForegroundPID int       `json:"foregroundPid,omitempty"`
	CapturedAt    time.Time `json:"capturedAt"`
	Imported      bool      `json:"imported"`
}

type legacyRecorder struct {
	mu         sync.Mutex
	dir        string
	meta       legacyCheckpoint
	history    []byte
	ansiState  byte
	previousCR bool
}

func durableLegacyFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".legacy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func startLegacyRecovery(ctx context.Context) *legacyRecorder {
	legacyBridgeLive.Store(false)
	owner, endpoint := os.Getenv("LEGACY_TERMINAL_OWNER"), os.Getenv("LEGACY_TERMINAL_URL")
	if owner == "" || endpoint == "" {
		return nil
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		log.Printf("[Legacy] boot identity unavailable: %v", err)
		return nil
	}
	dir := getEnv("TERMINAL_DATA_DIR", "data/terminals")
	r := &legacyRecorder{dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "legacy-recovery.json"))
	if err == nil {
		if err = json.Unmarshal(raw, &r.meta); err != nil || r.meta.Owner != owner || r.meta.ID == "" {
			log.Printf("[Legacy] invalid recovery checkpoint; original archive retained")
			return nil
		}
	} else if os.IsNotExist(err) {
		id := make([]byte, 16)
		if _, err = rand.Read(id); err != nil {
			return nil
		}
		home, _ := os.UserHomeDir()
		r.meta = legacyCheckpoint{BootID: strings.TrimSpace(string(boot)), Owner: owner, ID: hex.EncodeToString(id), Cwd: home}
	} else {
		log.Printf("[Legacy] checkpoint unreadable: %v", err)
		return nil
	}
	if !codexResumePattern.MatchString(r.meta.ResumeCommand) {
		r.meta.ResumeCommand = ""
	}
	r.history, err = os.ReadFile(filepath.Join(dir, "legacy-screen.txt"))
	if err != nil && !os.IsNotExist(err) {
		log.Printf("[Legacy] screen archive unreadable: %v", err)
		return nil
	}
	if r.meta.Imported {
		return r
	}
	if r.meta.BootID != strings.TrimSpace(string(boot)) {
		// The old PTY cannot survive a machine boot. Never reconnect the old tab to
		// the unrelated fresh shell created by the legacy daemon on the new boot.
		go r.importAfterBoot(ctx)
		return r
	}
	legacyBridgeLive.Store(true)
	r.updateProcessContext()
	if err := r.flush(); err != nil {
		log.Printf("[Legacy] initial checkpoint: %v", err)
	}
	go r.record(ctx, endpoint)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.updateProcessContext()
				if err := r.flush(); err != nil {
					log.Printf("[Legacy] checkpoint: %v", err)
				}
			}
		}
	}()
	return r
}

func (r *legacyRecorder) importAfterBoot(ctx context.Context) {
	for {
		r.mu.Lock()
		meta := r.meta
		history := []byte(strings.ReplaceAll(strings.ReplaceAll(string(r.history), "\r\n", "\n"), "\n", "\r\n"))
		r.mu.Unlock()
		_, err := terminals.ImportRecovered(meta.Owner, meta.ID, "Avvalgi terminal", meta.Cwd, history, meta.ResumeCommand)
		if err == nil {
			r.mu.Lock()
			r.meta.Imported = true
			r.mu.Unlock()
			if err = r.flush(); err != nil {
				log.Printf("[Legacy] import checkpoint: %v", err)
			}
			return
		}
		log.Printf("[Legacy] archive restoration pending: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// A reader observes output only. It never sends shell input or resize commands.
// This temporary bridge is retired after the first host reboot; all subsequent
// sessions use the regular tmux persistence/recovery mechanism.
func (r *legacyRecorder) record(ctx context.Context, endpoint string) {
	for ctx.Err() == nil {
		u, err := url.Parse(endpoint)
		if err != nil {
			return
		}
		if u.Scheme == "https" {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
		u.Path = "/ws"
		token, err := generateToken(r.meta.Owner, "user")
		if err != nil {
			return
		}
		query := u.Query()
		query.Set("token", token)
		u.RawQuery = query.Encode()
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
		if err == nil {
			conn.SetReadLimit(16 * 1024 * 1024)
			done := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					conn.Close()
				case <-done:
				}
			}()
			for {
				kind, data, readErr := conn.ReadMessage()
				if readErr != nil {
					break
				}
				if kind != websocket.TextMessage {
					continue
				}
				var message struct {
					Type    string `json:"type"`
					Payload struct {
						Data string `json:"data"`
					} `json:"payload"`
				}
				if json.Unmarshal(data, &message) == nil && message.Type == "shell_output" {
					r.appendOutput(message.Payload.Data)
				}
			}
			close(done)
			conn.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// Strip terminal controls from legacy output before archiving. Keeping parser
// state across WebSocket frames also handles split CSI and OSC sequences.
func (r *legacyRecorder) appendOutput(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range []byte(s) {
		switch r.ansiState {
		case 1:
			switch b {
			case '[':
				r.ansiState = 2
			case ']':
				r.ansiState = 3
			case 'P', '^', '_':
				r.ansiState = 3
			case '(', ')':
				r.ansiState = 5
			default:
				r.ansiState = 0
			}
		case 2:
			if b >= 0x40 && b <= 0x7e {
				r.ansiState = 0
			}
		case 3:
			if b == 7 {
				r.ansiState = 0
			} else if b == 27 {
				r.ansiState = 4
			}
		case 4:
			if b == '\\' {
				r.ansiState = 0
			} else {
				r.ansiState = 3
			}
		case 5:
			r.ansiState = 0
		default:
			if b == 27 {
				r.ansiState = 1
			} else if b == '\r' {
				r.history = append(r.history, '\n')
				r.previousCR = true
			} else if b == '\n' {
				if !r.previousCR {
					r.history = append(r.history, b)
				}
				r.previousCR = false
			} else if b == '\t' || b >= 32 {
				r.history = append(r.history, b)
				r.previousCR = false
			}
		}
	}
	if len(r.history) > legacyHistoryLimit {
		start := len(r.history) - legacyHistoryLimit
		for start < len(r.history) && !utf8.RuneStart(r.history[start]) {
			start++
		}
		r.history = append([]byte(nil), r.history[start:]...)
	}
}

func (r *legacyRecorder) flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.CapturedAt = time.Now().UTC()
	// Write output first, then metadata. A failed metadata rename cannot make a
	// prior saved screen disappear. Both files remain private to the service user.
	if err := durableLegacyFile(filepath.Join(r.dir, "legacy-screen.txt"), r.history); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.meta, "", "  ")
	if err != nil {
		return err
	}
	return durableLegacyFile(filepath.Join(r.dir, "legacy-recovery.json"), b)
}

func processChildren(pid int) []int {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
	if err != nil {
		return nil
	}
	var result []int
	for _, word := range strings.Fields(string(b)) {
		n, err := strconv.Atoi(word)
		if err == nil {
			result = append(result, n)
		}
	}
	return result
}
func (r *legacyRecorder) updateProcessContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "systemctl", "--user", "show", "fastremote-agent.service", "--property=MainPID", "--value").Output()
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return
	}
	var shellPID int
	for _, child := range processChildren(pid) {
		comm, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(child), "comm"))
		if strings.TrimSpace(string(comm)) == "bash" {
			shellPID = child
			break
		}
	}
	if shellPID == 0 {
		return
	}
	foreground := shellPID
	queue := processChildren(shellPID)
	for depth := 0; len(queue) > 0 && depth < 128; depth++ {
		child := queue[0]
		queue = queue[1:]
		comm, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(child), "comm"))
		if strings.TrimSpace(string(comm)) == "codex" {
			foreground = child
		}
		queue = append(queue, processChildren(child)...)
	}
	cwd, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(foreground), "cwd"))
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.Cwd = cwd
	if r.meta.ForegroundPID != 0 && r.meta.ForegroundPID != foreground {
		r.meta.ResumeCommand = ""
	}
	r.meta.ForegroundPID = foreground
	// Only explicit UUID resume invocations are inferred. Initial known sessions
	// can be seeded by migration tooling after checking the local session log.
	raw, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(foreground), "cmdline"))
	args := strings.Split(string(raw), "\x00")
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "resume" {
			candidate := "codex resume " + args[i+1]
			if codexResumePattern.MatchString(candidate) {
				r.meta.ResumeCommand = candidate
			}
		}
	}
}
