package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fastremote-agent/terminal"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLegacyArchiveStripsSplitControlSequences(t *testing.T) {
	r := &legacyRecorder{dir: t.TempDir(), meta: legacyCheckpoint{Owner: "alice", ID: strings.Repeat("a", 32), BootID: "old-boot"}}
	r.appendOutput("hello\r")
	r.appendOutput("\n\x1b[3")
	r.appendOutput("1mworld\x1b[0m\r\n\x1b]0;private window")
	r.appendOutput(" title\a世界\r\n")
	if got := string(r.history); got != "hello\nworld\n世界\n" {
		t.Fatalf("archive %q", got)
	}
	if err := r.flush(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"legacy-screen.txt", "legacy-recovery.json"} {
		info, err := os.Stat(filepath.Join(r.dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private file %s %v", name, err)
		}
	}
	r.appendOutput(strings.Repeat("世界", legacyHistoryLimit/2))
	if len(r.history) > legacyHistoryLimit || !utf8.Valid(r.history) {
		t.Fatal("bounded UTF-8 archive invalid")
	}
}

func TestLegacyBootArchiveImportsOnlyOnce(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "tmux.sock")
	config, _ := filepath.Abs("../deploy/terminal.tmux.conf")
	if b, err := exec.Command("tmux", "-f", config, "-S", socket, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("tmux %s %v", b, err)
	}
	defer exec.Command("tmux", "-S", socket, "kill-server").Run()
	old := terminals
	defer func() { terminals = old }()
	var err error
	terminals, err = terminal.New(filepath.Join(dir, "data"), socket)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 32)
	r := &legacyRecorder{dir: filepath.Join(dir, "data"), meta: legacyCheckpoint{Owner: "alice", ID: id, BootID: "old-boot", Cwd: dir, ResumeCommand: "codex resume 01a12bd4-ad37-77f2-bd78-82de3b945b3c"}, history: []byte("LEGACY_CONTEXT\n世界\n")}
	r.importAfterBoot(context.Background())
	r.importAfterBoot(context.Background())
	sessions, active := terminals.List("alice")
	if len(sessions) != 1 || sessions[0].ID != id || active != id || sessions[0].RecoveredAt == nil {
		t.Fatalf("restored %+v %s", sessions, active)
	}
	if !r.meta.Imported {
		t.Fatal("migration not marked complete")
	}
	b, err := terminals.History("alice", id)
	if err != nil || !bytes.Contains(b, []byte("LEGACY_CONTEXT\r\n世界\r\n")) {
		t.Fatalf("archive not restored with terminal newlines %v", err)
	}
	if _, err = terminals.Get("bob", id); err != terminal.ErrNotFound {
		t.Fatal("archive exposed to another owner")
	}
	raw, _ := os.ReadFile(filepath.Join(r.dir, "legacy-recovery.json"))
	var saved legacyCheckpoint
	if json.Unmarshal(raw, &saved) != nil || !saved.Imported {
		t.Fatal("migration not persisted")
	}
}
