package shell

import (
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Shell manages a persistent shell session
type Shell struct {
	cmd    *exec.Cmd
	ptmx   *os.File
	stdin  io.WriteCloser
	mu     sync.Mutex
	output chan string
	active bool
}

// New creates a new Shell session
func New() (*Shell, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoExit", "-Command", "-")
	} else {
		// Use login shell for proper environment
		cmd = exec.Command("bash", "--login", "-i")
		// Disable line buffering for child processes
		cmd.Env = append(os.Environ(),
			"TERM=xterm-256color",
			"PYTHONUNBUFFERED=1",
			"NODE_OPTIONS=--max-old-space-size=4096",
		)
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[Shell] PTY failed (%v), falling back to pipes", err)
		return newFallbackPipeShell(cmd)
	}

	// Set initial PTY size
	pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 40})

	s := &Shell{
		cmd:    cmd,
		ptmx:   ptmx,
		output: make(chan string, 4096),
		active: true,
	}

	// Read from PTY with small buffer for real-time output
	go s.readPTY(ptmx)

	// Monitor process
	go func() {
		cmd.Wait()
		s.mu.Lock()
		s.active = false
		s.mu.Unlock()
		if s.ptmx != nil {
			s.ptmx.Close()
		}
		log.Println("[Shell] Process exited")
	}()

	log.Println("[Shell] Started PTY shell session (xterm-256color)")
	return s, nil
}

func newFallbackPipeShell(cmd *exec.Cmd) (*Shell, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // Merge stderr to stdout pipe

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	s := &Shell{
		cmd:    cmd,
		stdin:  stdin,
		output: make(chan string, 4096),
		active: true,
	}

	go s.readPTY(stdout)
	go func() {
		cmd.Wait()
		s.mu.Lock()
		s.active = false
		s.mu.Unlock()
		stdin.Close()
	}()

	log.Println("[Shell] Started fallback pipe shell session")
	return s, nil
}

// Write sends input to the shell
func (s *Shell) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return io.ErrClosedPipe
	}

	if s.ptmx != nil {
		_, err := s.ptmx.Write([]byte(data))
		return err
	}

	if s.stdin != nil {
		_, err := s.stdin.Write([]byte(data))
		return err
	}

	return io.ErrClosedPipe
}

// Resize changes the PTY window size
func (s *Shell) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active || s.ptmx == nil {
		return nil
	}

	size := &pty.Winsize{
		Cols: cols,
		Rows: rows,
	}

	err := pty.Setsize(s.ptmx, size)
	if err != nil {
		log.Printf("[Shell] Resize error: %v", err)
		return err
	}

	log.Printf("[Shell] Resized to %dx%d", cols, rows)
	return nil
}

// Output returns the channel for reading shell output
func (s *Shell) Output() <-chan string {
	return s.output
}

// IsActive returns whether the shell is still running
func (s *Shell) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Close terminates the shell session
func (s *Shell) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		if s.ptmx != nil {
			s.ptmx.Close()
		}
		if s.stdin != nil {
			s.stdin.Close()
		}
		if s.cmd != nil && s.cmd.Process != nil {
			s.cmd.Process.Kill()
		}
		s.active = false
	}
}

// readPTY reads from PTY/pipe with small buffer for real-time character-by-character output
func (s *Shell) readPTY(r io.Reader) {
	// Small buffer = more frequent, smaller chunks = real-time output
	buf := make([]byte, 256)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			// Use blocking send with a short timeout — never silently drop output
			select {
			case s.output <- chunk:
				// sent successfully
			case <-time.After(50 * time.Millisecond):
				// Channel is full, try to drain and send
				select {
				case s.output <- chunk:
				default:
					log.Printf("[Shell] Warning: output channel full, dropping %d bytes", n)
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[Shell] Read error: %v", err)
			}
			break
		}
	}
}
