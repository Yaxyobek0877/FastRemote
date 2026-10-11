// Package terminal owns the persistent terminal catalogue. The tmux server runs
// in a separate service: disconnecting clients or restarting HTTP cannot kill it.
package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("terminal topilmadi")

type Session struct {
	ID        string    `json:"id"`
	Owner     string    `json:"-"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	Status    string    `json:"status"`
	Cwd       string    `json:"cwd,omitempty"`
}
type record struct {
	Session
	Owner string `json:"owner"`
}
type catalogue struct {
	Sessions []record          `json:"sessions"`
	Active   map[string]string `json:"active"`
}
type Manager struct {
	mu          sync.Mutex
	dir, socket string
	data        catalogue
	sizes       map[string][2]int
}

func New(dir, socket string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, socket: socket, data: catalogue{Active: map[string]string{}}}
	m.sizes = make(map[string][2]int)
	b, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err == nil {
		if err = json.Unmarshal(b, &m.data); err != nil {
			return nil, fmt.Errorf("terminal catalogue: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if m.data.Active == nil {
		m.data.Active = map[string]string{}
	}
	for i := range m.data.Sessions {
		m.data.Sessions[i].Session.Owner = m.data.Sessions[i].Owner
	}
	return m, nil
}
func (m *Manager) command(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-S", m.socket}, args...)...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	return cmd
}
func (m *Manager) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", append([]string{"-S", m.socket}, args...)...)
	return cmd.Output()
}
func (m *Manager) save() error {
	b, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(m.dir, "sessions.json"), b)
}
func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".terminal-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func target(id string) string { return "fr-" + id }
func (m *Manager) find(owner, id string) (int, error) {
	for i, r := range m.data.Sessions {
		if r.Owner == owner && r.ID == id {
			return i, nil
		}
	}
	return -1, ErrNotFound
}
func (m *Manager) status(id string) string {
	b, err := m.run("display-message", "-p", "-t", target(id), "#{pane_dead}")
	if err != nil {
		return "stopped"
	}
	if strings.TrimSpace(string(b)) == "1" {
		return "exited"
	}
	return "running"
}
func (m *Manager) List(owner string) ([]Session, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sessions := []Session{}
	for _, r := range m.data.Sessions {
		if r.Owner == owner {
			s := r.Session
			s.Status = m.status(s.ID)
			sessions = append(sessions, s)
		}
	}
	return sessions, m.data.Active[owner]
}
func (m *Manager) Get(owner, id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(owner, id)
	if err != nil {
		return Session{}, err
	}
	s := m.data.Sessions[i].Session
	s.Status = m.status(id)
	return s, nil
}
func titleValid(title string) bool {
	return strings.TrimSpace(title) != "" && utf8.RuneCountInString(title) <= 80 && !strings.ContainsAny(title, "\r\n\x00")
}
func (m *Manager) Create(owner, title string) (Session, error) {
	return m.CreateInDirectory(owner, title, "")
}

func (m *Manager) CreateInDirectory(owner, title, cwd string) (Session, error) {
	cwd, err := resolveDirectory(cwd)
	if err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, r := range m.data.Sessions {
		if r.Owner == owner {
			count++
		}
	}
	if count >= 24 {
		return Session{}, errors.New("24 ta terminal chegarasiga yetildi")
	}
	if title == "" {
		title = fmt.Sprintf("Terminal %d", count+1)
	}
	if !titleValid(title) {
		return Session{}, errors.New("nom 1–80 belgidan iborat bo‘lsin")
	}
	// Never autostart tmux inside the web service's cgroup.
	if _, err := m.run("show-options", "-s", "exit-empty"); err != nil {
		return Session{}, errors.New("terminal xizmati ishlamayapti")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Session{}, err
	}
	s := Session{ID: hex.EncodeToString(id), Owner: owner, Title: strings.TrimSpace(title), CreatedAt: time.Now().UTC(), Status: "running", Cwd: cwd}
	if _, err = m.run("new-session", "-d", "-s", target(s.ID), "-x", "120", "-y", "36", "-c", cwd); err != nil {
		return Session{}, fmt.Errorf("terminal ochilmadi: %w", err)
	}
	m.data.Sessions = append(m.data.Sessions, record{Session: s, Owner: owner})
	prev := m.data.Active[owner]
	m.data.Active[owner] = s.ID
	if err = m.save(); err != nil {
		m.data.Sessions = m.data.Sessions[:len(m.data.Sessions)-1]
		m.data.Active[owner] = prev
		m.run("kill-session", "-t", target(s.ID))
		return Session{}, err
	}
	return s, nil
}
func (m *Manager) Update(owner, id, title string, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(owner, id)
	if err != nil {
		return err
	}
	oldTitle, oldActive := m.data.Sessions[i].Title, m.data.Active[owner]
	if title != "" {
		if !titleValid(title) {
			return errors.New("nom 1–80 belgidan iborat bo‘lsin")
		}
		m.data.Sessions[i].Title = strings.TrimSpace(title)
	}
	if active {
		m.data.Active[owner] = id
	}
	if err = m.save(); err != nil {
		m.data.Sessions[i].Title = oldTitle
		m.data.Active[owner] = oldActive
	}
	return err
}
func (m *Manager) Delete(owner, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(owner, id)
	if err != nil {
		return err
	}
	if m.status(id) != "stopped" {
		if _, err = m.run("kill-session", "-t", target(id)); err != nil {
			return err
		}
	}
	old := append([]record(nil), m.data.Sessions...)
	oldActive := m.data.Active[owner]
	m.data.Sessions = append(m.data.Sessions[:i], m.data.Sessions[i+1:]...)
	if m.data.Active[owner] == id {
		delete(m.data.Active, owner)
		for _, r := range m.data.Sessions {
			if r.Owner == owner {
				m.data.Active[owner] = r.ID
			}
		}
	}
	if err = m.save(); err != nil {
		m.data.Sessions = old
		m.data.Active[owner] = oldActive
		return err
	}
	os.Remove(filepath.Join(m.dir, id+".ansi"))
	return nil
}
func (m *Manager) Snapshot(owner, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(owner, id); err != nil {
		return nil, err
	}
	return m.snapshot(id)
}
func (m *Manager) snapshot(id string) ([]byte, error) {
	if b, err := m.run("capture-pane", "-p", "-e", "-t", target(id), "-S", "-10000"); err == nil {
		b = []byte(strings.ReplaceAll(string(b), "\n", "\r\n"))
		if err = atomicWrite(filepath.Join(m.dir, id+".ansi"), b); err != nil {
			return nil, err
		}
		return b, nil
	}
	b, err := os.ReadFile(filepath.Join(m.dir, id+".ansi"))
	if os.IsNotExist(err) {
		return []byte{}, nil
	}
	return b, err
}
func (m *Manager) Checkpoint(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			for _, r := range m.data.Sessions {
				m.snapshot(r.ID)
			}
			m.mu.Unlock()
		}
	}
}

// History excludes the visible screen: attach-session redraws that screen and
// its cursor/alternate-buffer state. Binary WS frames preserve split UTF-8.
func (m *Manager) History(owner, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(owner, id); err != nil {
		return nil, err
	}
	b, err := m.run("capture-pane", "-p", "-e", "-t", target(id), "-S", "-10000", "-E", "-1")
	return []byte(strings.ReplaceAll(string(b), "\n", "\r\n")), err
}
func (m *Manager) Attach(owner, id string) (*exec.Cmd, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(owner, id); err != nil {
		return nil, err
	}
	if m.status(id) == "stopped" {
		return nil, errors.New("sessiya to‘xtagan; yangi terminal oching")
	}
	return m.command("attach-session", "-t", target(id)), nil
}

// Resize follows the device currently typing or resizing, rather than pinning
// all clients to the smallest connected phone. The tmux pane remains the same.
func (m *Manager) Resize(owner, id string, cols, rows int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(owner, id); err != nil {
		return err
	}
	if cols < 2 || cols > 500 || rows < 2 || rows > 500 {
		return errors.New("invalid terminal size")
	}
	size := [2]int{cols, rows}
	if m.sizes[id] == size {
		return nil
	}
	if _, err := m.run("resize-window", "-t", target(id), "-x", fmt.Sprint(cols), "-y", fmt.Sprint(rows)); err != nil {
		return err
	}
	m.sizes[id] = size
	return nil
}
