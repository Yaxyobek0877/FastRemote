package terminal

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const recoveryNote = "Server yoki terminal xizmati qayta ishga tushgan. Oldingi ekran saqlandi, shu papkada yangi shell ochildi. Oldingi jarayonlar avtomatik davom ettirilmaydi."

func validID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && strings.ToLower(id) == id
}

func decodeCatalogue(b []byte, data *catalogue) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	if fields["sessions"] == nil || fields["active"] == nil {
		return errors.New("incomplete terminal catalogue")
	}
	if err := json.Unmarshal(b, data); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, r := range data.Sessions {
		if !validID(r.ID) || r.Owner == "" || seen[r.ID] {
			return errors.New("invalid terminal identity in catalogue")
		}
		seen[r.ID] = true
	}
	return nil
}

func (m *Manager) load() error {
	var primaryErr error
	for _, name := range []string{"sessions.json", "sessions.json.bak"} {
		b, err := os.ReadFile(filepath.Join(m.dir, name))
		if err == nil {
			var data catalogue
			err = decodeCatalogue(b, &data)
			if err == nil {
				m.data = data
				if name != "sessions.json" {
					log.Printf("terminal catalogue restored from backup")
				}
				return nil
			}
		}
		if name == "sessions.json" {
			primaryErr = err
		} else if os.IsNotExist(primaryErr) && os.IsNotExist(err) {
			return nil
		}
	}
	return fmt.Errorf("terminal catalogue: %w", primaryErr)
}

func readOptional(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// A private rc file keeps each shell's command history separate and appends
// completed commands at every prompt, rather than only at a clean shell exit.
// No history entry or saved foreground command is ever executed on recovery.
func (m *Manager) startShell(s Session) error {
	history := shellQuote(filepath.Join(m.dir, s.ID+".bash_history"))
	rc := "# FastRemote shell: retain the user's usual interactive configuration.\n" +
		"HISTFILE=" + history + "\n" +
		"if [[ -r /etc/profile ]]; then source /etc/profile; fi\n" +
		"for profile in \"$HOME/.bash_profile\" \"$HOME/.bash_login\" \"$HOME/.profile\"; do\n" +
		"  if [[ -r \"$profile\" ]]; then source \"$profile\"; break; fi\n" +
		"done\n" +
		"unset profile\n" +
		"HISTFILE=" + history + "\n" +
		"HISTSIZE=10000\nHISTFILESIZE=20000\nshopt -s histappend\n" +
		"if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == 'declare -a '* ]]; then\n" +
		"  PROMPT_COMMAND+=('history -a')\n" +
		"else\n" +
		"  PROMPT_COMMAND=\"${PROMPT_COMMAND:+${PROMPT_COMMAND}; }history -a\"\n" +
		"fi\n"
	rcPath := filepath.Join(m.dir, s.ID+".bashrc")
	if err := atomicWrite(rcPath, []byte(rc)); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.dir, s.ID+".bash_history"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	_, err = m.run("new-session", "-d", "-s", target(s.ID), "-x", "120", "-y", "36", "-c", s.Cwd,
		"/bin/bash", "--rcfile", rcPath, "-i")
	return err
}

// CheckpointNow saves the latest pane output, directory and deliberate exit
// state. Missing panes keep their last observed state so an unavailable tmux
// service cannot accidentally turn a recoverable session into a stopped one.
func (m *Manager) CheckpointNow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var failures []error
	for i := range m.data.Sessions {
		r := &m.data.Sessions[i]
		state := m.status(r.ID)
		if state == "stopped" {
			continue
		}
		r.Status = state
		if b, err := m.run("display-message", "-p", "-t", target(r.ID), "#{pane_current_path}"); err == nil {
			if cwd := strings.TrimSpace(string(b)); filepath.IsAbs(cwd) && !strings.ContainsAny(cwd, "\r\n\x00") {
				r.Cwd = cwd
			}
		}
		if _, err := m.snapshot(r.ID); err != nil {
			failures = append(failures, fmt.Errorf("snapshot %s: %w", r.ID, err))
		}
		// history -a writes after each command; make those writes durable with
		// the same checkpoint as the screen and catalogue.
		if f, err := os.OpenFile(filepath.Join(m.dir, r.ID+".bash_history"), os.O_WRONLY, 0); err == nil {
			if err = f.Sync(); err != nil {
				failures = append(failures, err)
			}
			f.Close()
		} else if !os.IsNotExist(err) {
			failures = append(failures, err)
		}
	}
	if err := m.save(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// Limit archived output to the same order of magnitude as the tmux history.
// Every recovery archives the previous combined snapshot once; live snapshots
// are always archive + current pane, never appended to an earlier snapshot.
func boundedArchive(b []byte) []byte {
	const maxBytes = 4 * 1024 * 1024
	if len(b) > maxBytes {
		b = b[len(b)-maxBytes:]
		if newline := strings.IndexByte(string(b), '\n'); newline >= 0 {
			b = b[newline+1:]
		}
	}
	return b
}

func (m *Manager) archive(id string, history []byte, at time.Time) error {
	history = boundedArchive(history)
	separator := "\r\n\x1b[0m── Saqlangan ekran · " + at.Format(time.RFC3339) + " · quyida yangi shell ──\r\n"
	return atomicWrite(filepath.Join(m.dir, id+".archive.ansi"), append(history, []byte(separator)...))
}

func recoveryDirectory(cwd string) (string, bool, error) {
	resolved, err := resolveDirectory(cwd)
	if err == nil {
		return resolved, false, nil
	}
	resolved, err = resolveDirectory("")
	return resolved, true, err
}

func (m *Manager) recoverRecord(i int, manual bool) (Session, error) {
	r := &m.data.Sessions[i]
	state := m.status(r.ID)
	if state == "running" {
		s := r.Session
		s.Status = state
		return s, nil
	}
	if !manual && (state != "stopped" || r.Status != "running") {
		return r.Session, nil
	}
	// A running terminal server must exist before new-session; never allow
	// tmux to create its daemon in the web process's cgroup.
	if _, err := m.run("show-options", "-s", "exit-empty"); err != nil {
		return Session{}, errors.New("terminal xizmati ishlamayapti")
	}
	history, err := m.snapshot(r.ID)
	if err != nil {
		return Session{}, err
	}
	// A transient status-command failure must not replace a shell which still
	// exists. new-session also enforces unique names if the state changes again.
	if current := m.status(r.ID); current == "running" {
		s := r.Session
		s.Status = current
		return s, nil
	}
	archivePath := filepath.Join(m.dir, r.ID+".archive.ansi")
	previousArchive, err := readOptional(archivePath)
	if err != nil {
		return Session{}, err
	}
	cwd, fallback, err := recoveryDirectory(r.Cwd)
	if err != nil {
		return Session{}, err
	}
	at := time.Now().UTC()
	if state == "exited" {
		if _, err = m.run("kill-session", "-t", target(r.ID)); err != nil {
			return Session{}, err
		}
	}
	if err = m.archive(r.ID, history, at); err != nil {
		return Session{}, errors.Join(err, atomicWrite(archivePath, previousArchive))
	}
	old := r.Session
	r.Cwd, r.Status, r.RecoveredAt, r.RecoveryNote = cwd, "running", &at, recoveryNote
	if manual {
		r.RecoveryNote = "Sessiya uchun yangi shell ochildi. Oldingi ekran saqlandi; buyruqlar avtomatik qayta bajarilmaydi."
	}
	if fallback {
		r.RecoveryNote += " Oldingi papka mavjud emas; uy papkasi ochildi."
	}
	if err = m.startShell(r.Session); err != nil {
		r.Session = old
		return Session{}, fmt.Errorf("terminal tiklanmadi: %w", errors.Join(err, atomicWrite(archivePath, previousArchive)))
	}
	delete(m.sizes, r.ID)
	if err = m.save(); err != nil {
		// The shell already exists; preserve it and report the persistence error.
		return r.Session, err
	}
	return r.Session, nil
}

// Recover only recreates missing panes whose last durable state was running.
// Intentionally exited sessions remain stopped until Restart is requested.
func (m *Manager) Recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var failures []error
	for i := range m.data.Sessions {
		if _, err := m.recoverRecord(i, false); err != nil {
			failures = append(failures, fmt.Errorf("recover %s: %w", m.data.Sessions[i].ID, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) Restart(owner, id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(owner, id)
	if err != nil {
		return Session{}, err
	}
	return m.recoverRecord(i, true)
}

// ImportRecovered converts a boot-bound legacy checkpoint into an owned
// persistent terminal. The stable supplied ID makes repeated imports safe.
func (m *Manager) ImportRecovered(owner, id, title, cwd string, history []byte, resumeCommand string) (Session, error) {
	if owner == "" || !validID(id) || !titleValid(title) {
		return Session{}, errors.New("invalid recovered terminal identity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.data.Sessions {
		if r.ID == id {
			if r.Owner != owner {
				return Session{}, ErrNotFound
			}
			s := r.Session
			s.Status = m.status(id)
			return s, nil
		}
	}
	if _, err := m.run("show-options", "-s", "exit-empty"); err != nil {
		return Session{}, errors.New("terminal xizmati ishlamayapti")
	}
	cwd, fallback, err := recoveryDirectory(cwd)
	if err != nil {
		return Session{}, err
	}
	at := time.Now().UTC()
	s := Session{ID: id, Owner: owner, Title: title, CreatedAt: at, Status: "running", Cwd: cwd,
		RecoveredAt: &at, RecoveryNote: recoveryNote, ResumeCommand: resumeCommand}
	if fallback {
		s.RecoveryNote += " Oldingi papka mavjud emas; uy papkasi ochildi."
	}
	if err = m.archive(id, history, at); err != nil {
		return Session{}, err
	}
	// Preserve the imported screen even if a restart happens before the first
	// periodic snapshot captures the new shell.
	if err = atomicWrite(filepath.Join(m.dir, id+".ansi"), boundedArchive(history)); err != nil {
		return Session{}, err
	}
	if err = m.startShell(s); err != nil {
		return Session{}, err
	}
	m.data.Sessions = append(m.data.Sessions, record{Session: s, Owner: owner})
	previous := m.data.Active[owner]
	if previous == "" || previous == "legacy" {
		m.data.Active[owner] = id
	}
	if err = m.save(); err != nil {
		m.data.Sessions = m.data.Sessions[:len(m.data.Sessions)-1]
		m.data.Active[owner] = previous
		m.run("kill-session", "-t", target(id))
		return Session{}, err
	}
	return s, nil
}
