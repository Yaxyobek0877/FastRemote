package terminal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("", "fr-test-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "tmux.sock")
	config, err := filepath.Abs("../../deploy/terminal.tmux.conf")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("tmux", "-f", config, "-S", socket, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %s %v", b, err)
	}
	t.Cleanup(func() { exec.Command("tmux", "-S", socket, "kill-server").Run(); os.RemoveAll(dir) })
	m, err := New(filepath.Join(dir, "data"), socket)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func TestPersistenceIsolationAndOfflineProcess(t *testing.T) {
	m := testManager(t)
	a, err := m.Create("alice", "Build")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("bob", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Get("bob", a.ID); err != ErrNotFound {
		t.Fatalf("cross-user read: %v", err)
	}
	if err = m.Delete("bob", a.ID); err != ErrNotFound {
		t.Fatalf("cross-user delete: %v", err)
	}
	if _, err = m.Attach("bob", a.ID); err != ErrNotFound {
		t.Fatalf("cross-user attach: %v", err)
	}
	if err = m.Update("alice", a.ID, "Claude work", true); err != nil {
		t.Fatal(err)
	}
	// This process finishes while no terminal/browser is attached.
	m.run("send-keys", "-t", target(a.ID), "sleep 0.2; printf '\\nOFFLINE_DONE_世界\\n'", "Enter")
	eventually(t, func() bool {
		buf, _ := m.Snapshot("alice", a.ID)
		return bytes.Contains(buf, []byte("\r\nOFFLINE_DONE_世界\r\n"))
	})
	// Construct a fresh manager, exactly as after an HTTP-service restart.
	restored, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	list, active := restored.List("alice")
	if len(list) != 1 || list[0].Title != "Claude work" || list[0].Status != "running" || active != a.ID {
		t.Fatalf("restore: %+v %s", list, active)
	}
	buf, _ := restored.Snapshot("bob", b.ID)
	if bytes.Contains(buf, []byte("OFFLINE_DONE")) {
		t.Fatal("sessions share output")
	}
	// Host/tmux loss cannot restore a running process; persisted history remains.
	m.run("kill-session", "-t", target(a.ID))
	s, err := restored.Get("alice", a.ID)
	if err != nil || s.Status != "stopped" {
		t.Fatalf("stopped: %+v %v", s, err)
	}
	buf, err = restored.Snapshot("alice", a.ID)
	if err != nil || !bytes.Contains(buf, []byte("OFFLINE_DONE_世界")) {
		t.Fatal("saved history lost", err)
	}
	if err = restored.Delete("alice", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(m.dir, a.ID+".ansi")); !os.IsNotExist(err) {
		t.Fatal("deleted snapshot remains")
	}
}
func TestExitedSessionAndValidation(t *testing.T) {
	m := testManager(t)
	s, err := m.Create("alice", "Shell")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Create("alice", strings.Repeat("x", 81)); err == nil {
		t.Fatal("accepted long title")
	}
	if err = m.Update("alice", s.ID, "bad\nname", false); err == nil {
		t.Fatal("accepted newline")
	}
	m.run("send-keys", "-t", target(s.ID), "exit", "Enter")
	eventually(t, func() bool { s, _ := m.Get("alice", s.ID); return s.Status == "exited" })
	if _, err = m.Attach("alice", s.ID); err != nil {
		t.Fatal("exited screen should remain attachable", err)
	}
}

func TestProjectDirectoryPersists(t *testing.T) {
	m := testManager(t)
	dir := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateInDirectory("alice", "Project", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Delete("alice", s.ID)
	out, err := m.run("display-message", "-p", "-t", target(s.ID), "#{pane_current_path}")
	if err != nil || strings.TrimSpace(string(out)) != dir {
		t.Fatalf("cwd %s %v", out, err)
	}
	restored, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.Get("alice", s.ID)
	if err != nil || got.Cwd != dir {
		t.Fatalf("restore cwd %+v %v", got, err)
	}
	if _, err = m.CreateInDirectory("alice", "Bad", filepath.Join(dir, "missing")); err == nil {
		t.Fatal("accepted missing directory")
	}
	if _, err = m.CreateInDirectory("alice", "Bad", "relative"); err == nil {
		t.Fatal("accepted relative directory")
	}
	sessions, _ := m.List("alice")
	if len(sessions) != 1 {
		t.Fatal("invalid directories created sessions")
	}
}
