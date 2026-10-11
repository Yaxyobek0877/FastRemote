package terminal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Only the temporary socket returned by testManager is touched. This models a
// host reboot without restarting the real terminal service or any user's shell.
func rebootTestServer(t *testing.T, m *Manager) {
	t.Helper()
	if !strings.HasPrefix(m.socket, os.TempDir()+"/fr-test-") {
		t.Fatal("refusing to restart a non-test tmux server")
	}
	if _, err := m.run("kill-server"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		_, err := m.run("show-options", "-s", "exit-empty")
		return err != nil
	})
	config, err := filepath.Abs("../../deploy/terminal.tmux.conf")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("tmux", "-f", config, "-S", m.socket, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("restart test tmux: %s %v", b, err)
	}
}

func panePID(t *testing.T, m *Manager, id string) string {
	t.Helper()
	b, err := m.run("display-message", "-p", "-t", target(id), "#{pane_pid}")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestRebootRecoversIdentityCwdHistoryWithoutExecutingCommands(t *testing.T) {
	m := testManager(t)
	project := t.TempDir()
	child := filepath.Join(project, "child with spaces")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateInDirectory("alice", "Persistent build", project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Create("bob", "Private"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(project, "executions")
	command := "cd " + shellQuote(child) + "; printf x >> " + shellQuote(marker) + "; printf '\\nBEFORE_REBOOT_世界\\n'"
	if _, err = m.run("send-keys", "-t", target(s.ID), command, "Enter"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		buf, _ := m.Snapshot("alice", s.ID)
		history, _ := os.ReadFile(filepath.Join(m.dir, s.ID+".bash_history"))
		return bytes.Contains(buf, []byte("\r\nBEFORE_REBOOT_世界\r\n")) && bytes.Contains(history, []byte("printf x >>"))
	})
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	oldPID := panePID(t, m, s.ID)
	rebootTestServer(t, m)
	restored, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Recover(); err != nil {
		t.Fatal(err)
	}
	list, active := restored.List("alice")
	if len(list) != 1 || list[0].ID != s.ID || list[0].Owner != "alice" || list[0].Title != s.Title ||
		!list[0].CreatedAt.Equal(s.CreatedAt) || list[0].Status != "running" || list[0].Cwd != child ||
		list[0].RecoveredAt == nil || list[0].RecoveryNote == "" || active != s.ID {
		t.Fatalf("recovered metadata: %+v, active %s", list, active)
	}
	newPID := panePID(t, restored, s.ID)
	if newPID == oldPID {
		t.Fatal("expected a new process after simulated reboot")
	}
	if _, err = restored.Get("bob", s.ID); err != ErrNotFound {
		t.Fatal("recovery broke owner isolation", err)
	}
	for i := 0; i < 3; i++ {
		if err = restored.CheckpointNow(); err != nil {
			t.Fatal(err)
		}
		buf, err := restored.History("alice", s.ID)
		if err != nil || bytes.Count(buf, []byte("\r\nBEFORE_REBOOT_世界\r\n")) != 1 {
			t.Fatalf("archive lost or duplicated after checkpoint %d: %q %v", i, buf, err)
		}
	}
	// A web-service restart reconnects to the same shell and keeps the archive.
	webRestart, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = webRestart.Recover(); err != nil || panePID(t, webRestart, s.ID) != newPID {
		t.Fatal("web restart replaced a running shell", err)
	}
	if _, err = webRestart.run("send-keys", "-t", target(s.ID), "printf '\\nSECOND_BOOT\\n'", "Enter"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		buf, _ := webRestart.Snapshot("alice", s.ID)
		return bytes.Contains(buf, []byte("\r\nSECOND_BOOT\r\n"))
	})
	if err = webRestart.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	rebootTestServer(t, webRestart)
	third, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = third.Recover(); err != nil {
		t.Fatal(err)
	}
	if err = third.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	buf, err := third.History("alice", s.ID)
	if err != nil || bytes.Count(buf, []byte("\r\nBEFORE_REBOOT_世界\r\n")) != 1 || bytes.Count(buf, []byte("\r\nSECOND_BOOT\r\n")) != 1 {
		t.Fatalf("history after second reboot: %q %v", buf, err)
	}
	if executions, err := os.ReadFile(marker); err != nil || string(executions) != "x" {
		t.Fatalf("saved command was repeated: %q %v", executions, err)
	}
	info, err := os.Stat(filepath.Join(m.dir, s.ID+".bash_history"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("shell history must remain private", err)
	}
}

func TestExitedSessionRequiresExplicitRestartAfterReboot(t *testing.T) {
	m := testManager(t)
	s, err := m.Create("alice", "Finished")
	if err != nil {
		t.Fatal(err)
	}
	m.run("send-keys", "-t", target(s.ID), "printf '\\nFINISHED_OUTPUT\\n'; exit", "Enter")
	eventually(t, func() bool { got, _ := m.Get("alice", s.ID); return got.Status == "exited" })
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	rebootTestServer(t, m)
	restored, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Recover(); err != nil {
		t.Fatal(err)
	}
	got, _ := restored.Get("alice", s.ID)
	if got.Status != "stopped" || got.RecoveredAt != nil {
		t.Fatalf("deliberately exited session was restarted: %+v", got)
	}
	if _, err = restored.Restart("bob", s.ID); err != ErrNotFound {
		t.Fatal("another owner restarted shell", err)
	}
	got, err = restored.Restart("alice", s.ID)
	if err != nil || got.Status != "running" || got.RecoveredAt == nil {
		t.Fatalf("explicit restart: %+v %v", got, err)
	}
	buf, err := restored.History("alice", s.ID)
	if err != nil || !bytes.Contains(buf, []byte("\r\nFINISHED_OUTPUT\r\n")) {
		t.Fatal("explicit restart lost archived output", err)
	}
}

func TestRecoveryFallsBackWhenProjectDirectoryIsMissing(t *testing.T) {
	m := testManager(t)
	project := filepath.Join(t.TempDir(), "removed")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateInDirectory("alice", "Removed project", project)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	rebootTestServer(t, m)
	if err = os.Remove(project); err != nil {
		t.Fatal(err)
	}
	if err = m.Recover(); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("alice", s.ID)
	home, _ := resolveDirectory("")
	if err != nil || got.Cwd != home || !strings.Contains(got.RecoveryNote, "uy papkasi") {
		t.Fatalf("directory fallback: %+v %v", got, err)
	}
}

func TestLegacyImportIsIdempotentOwnedAndNeverRunsResumeHint(t *testing.T) {
	m := testManager(t)
	id := strings.Repeat("a", 32)
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	hint := "touch " + shellQuote(marker)
	imported, err := m.ImportRecovered("alice", id, "Avvalgi terminal", "", []byte("LEGACY_SAVED_OUTPUT\r\n"), hint)
	if err != nil || imported.RecoveredAt == nil || imported.ResumeCommand != hint {
		t.Fatalf("import: %+v %v", imported, err)
	}
	pid := panePID(t, m, id)
	if _, err = m.ImportRecovered("alice", id, "Duplicate", "", nil, ""); err != nil || panePID(t, m, id) != pid {
		t.Fatal("duplicate import replaced a shell", err)
	}
	if _, err = m.ImportRecovered("bob", id, "Stolen", "", nil, ""); err != ErrNotFound {
		t.Fatal("import broke owner isolation", err)
	}
	if _, err = m.ImportRecovered("alice", "../escape", "Invalid", "", nil, ""); err == nil {
		t.Fatal("accepted invalid imported ID")
	}
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	buf, err := m.History("alice", id)
	if err != nil || !bytes.Contains(buf, []byte("LEGACY_SAVED_OUTPUT")) {
		t.Fatal("import lost snapshot", err)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("executed a resume hint")
	}
	list, active := m.List("alice")
	if len(list) != 1 || active != id {
		t.Fatalf("import catalogue: %+v, %s", list, active)
	}
}

func TestCatalogueBackupAndMissingServerDoNotDiscardRecovery(t *testing.T) {
	m := testManager(t)
	s, err := m.Create("alice", "Durable")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(m.dir, "sessions.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	fromBackup, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fromBackup.Get("alice", s.ID); err != nil || got.ID != s.ID {
		t.Fatalf("backup: %+v %v", got, err)
	}
	if _, err = m.run("kill-session", "-t", target(s.ID)); err != nil {
		t.Fatal(err)
	}
	// A missing pane is not a deliberate shell exit. Saving while unavailable
	// must retain the prior running state so the next recovery still sees it.
	if err = fromBackup.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	restored, err := New(m.dir, m.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Recover(); err != nil {
		t.Fatal(err)
	}
	if got, err := restored.Get("alice", s.ID); err != nil || got.Status != "running" {
		t.Fatalf("missing pane not recovered: %+v %v", got, err)
	}
}

func TestUnavailableServerDoesNotAutostartFromRecovery(t *testing.T) {
	m := testManager(t)
	s, err := m.Create("alice", "Service recovery")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.run("kill-server"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		_, err := m.run("show-options", "-s", "exit-empty")
		return err != nil
	})
	if err = m.Recover(); err == nil {
		t.Fatal("unavailable service did not produce a recovery error")
	}
	if _, err = m.run("show-options", "-s", "exit-empty"); err == nil {
		t.Fatal("recovery started tmux in the caller's cgroup")
	}
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	config, _ := filepath.Abs("../../deploy/terminal.tmux.conf")
	if b, err := exec.Command("tmux", "-f", config, "-S", m.socket, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("start test server: %s %v", b, err)
	}
	if err = m.Recover(); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Get("alice", s.ID); err != nil || got.Status != "running" {
		t.Fatalf("recover after service return: %+v %v", got, err)
	}
}

func TestFailedRecoveryDoesNotDuplicateOrReplaceArchivedOutput(t *testing.T) {
	m := testManager(t)
	s, err := m.Create("alice", "Retry safely")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.run("kill-session", "-t", target(s.ID)); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(m.dir, s.ID+".archive.ansi")
	previous := []byte("ARCHIVED_OUTPUT\r\n")
	if err = os.WriteFile(archivePath, previous, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(m.dir, s.ID+".ansi"), append(append([]byte(nil), previous...), []byte("RECENT_OUTPUT\r\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	// Force shell startup to fail after archiving, without changing the server.
	rcPath := filepath.Join(m.dir, s.ID+".bashrc")
	if err = os.Remove(rcPath); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(rcPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err = m.Recover(); err == nil {
		t.Fatal("expected startup failure")
	}
	if got, err := os.ReadFile(archivePath); err != nil || !bytes.Equal(got, previous) {
		t.Fatalf("failed recovery changed archive: %q %v", got, err)
	}
	if err = os.Remove(rcPath); err != nil {
		t.Fatal(err)
	}
	if err = m.Recover(); err != nil {
		t.Fatal(err)
	}
	if err = m.CheckpointNow(); err != nil {
		t.Fatal(err)
	}
	got, err := m.History("alice", s.ID)
	if err != nil || bytes.Count(got, []byte("ARCHIVED_OUTPUT")) != 1 || bytes.Count(got, []byte("RECENT_OUTPUT")) != 1 {
		t.Fatalf("retried recovery lost or duplicated history: %q %v", got, err)
	}
}
