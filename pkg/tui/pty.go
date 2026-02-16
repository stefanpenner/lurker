package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// ptySession holds a PTY with a shell that backs an issue's entire lifecycle.
// All commands (react, clone, claude) run inside this shell.
// The user can attach/detach at any time with Ctrl+].
type ptySession struct {
	ptmx  *os.File  // master side — we read/write this
	slave *os.File  // slave side — shell runs here
	cmd   *exec.Cmd // the shell process
	mu    sync.Mutex
	sink  io.Writer // stdout when attached, io.Discard when detached
	done  bool

	// Marker-based command completion detection
	markerMu  sync.Mutex
	pendingID string   // current marker ID we're watching for
	pendingCh chan int // receives exit code when marker is found
	scanBuf   []byte  // accumulates output for marker scanning
}

// newPtySession creates a PTY with a shell running in workdir.
func newPtySession(workdir string) (*ptySession, error) {
	ptmx, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}

	s := &ptySession{
		ptmx:  ptmx,
		slave: slave,
		sink:  io.Discard,
	}

	go s.drain()

	if err := s.startShell(workdir); err != nil {
		ptmx.Close()
		slave.Close()
		return nil, err
	}

	return s, nil
}

func (s *ptySession) startShell(workdir string) error {
	s.mu.Lock()
	if s.cmd != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "zsh"
	}

	cmd := exec.Command(shell)
	cmd.Dir = workdir
	cmd.Stdin = s.slave
	cmd.Stdout = s.slave
	cmd.Stderr = s.slave
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return err
	}

	s.mu.Lock()
	s.cmd = cmd
	s.done = false
	s.mu.Unlock()

	go func() {
		cmd.Wait()
		s.mu.Lock()
		s.done = true
		s.mu.Unlock()
	}()

	// Give the shell a moment to initialize before we send commands
	time.Sleep(100 * time.Millisecond)
	return nil
}

// RunCommand writes a command to the shell and waits for it to complete.
// Uses a marker pattern to detect completion and extract the exit code.
// Implements watcher.IssuePTY.
func (s *ptySession) RunCommand(ctx context.Context, cmd string) (int, error) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	marker := fmt.Sprintf("__LURKER_DONE_%s_", id)
	resultCh := make(chan int, 1)

	s.markerMu.Lock()
	s.pendingID = id
	s.pendingCh = resultCh
	s.scanBuf = nil
	s.markerMu.Unlock()

	defer func() {
		s.markerMu.Lock()
		s.pendingID = ""
		s.pendingCh = nil
		s.scanBuf = nil
		s.markerMu.Unlock()
	}()

	// Write command + marker to the shell
	fullCmd := fmt.Sprintf("%s; echo \"%s$?\"\n", cmd, marker)
	if _, err := s.ptmx.Write([]byte(fullCmd)); err != nil {
		return -1, fmt.Errorf("write to pty: %w", err)
	}

	select {
	case <-ctx.Done():
		// Send Ctrl+C to interrupt the running command
		s.ptmx.Write([]byte{0x03})
		return -1, ctx.Err()
	case code := <-resultCh:
		return code, nil
	}
}

// checkMarker scans accumulated output for the completion marker.
// Must be called with markerMu held.
func (s *ptySession) checkMarker() {
	if s.pendingID == "" {
		return
	}

	marker := fmt.Sprintf("__LURKER_DONE_%s_", s.pendingID)
	data := string(s.scanBuf)

	// Scan all occurrences — the echo shows "$?" (not digits),
	// the real output shows the actual exit code (digits).
	search := data
	for {
		idx := strings.Index(search, marker)
		if idx < 0 {
			return
		}

		rest := search[idx+len(marker):]

		// Read digits after marker
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}

		if end > 0 {
			// Found digits — this is the real output, not the echo
			var code int
			fmt.Sscanf(rest[:end], "%d", &code)
			if s.pendingCh != nil {
				s.pendingCh <- code
			}
			return
		}

		// No digits (this is the echo) — keep scanning
		search = rest
	}
}

// drain continuously reads PTY output, forwards to sink, and scans for markers.
func (s *ptySession) drain() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			s.mu.Lock()
			w := s.sink
			s.mu.Unlock()
			w.Write(chunk)

			// Scan for completion markers
			s.markerMu.Lock()
			if s.pendingID != "" {
				s.scanBuf = append(s.scanBuf, chunk...)
				s.checkMarker()
			}
			s.markerMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *ptySession) isDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func (s *ptySession) attach(w io.Writer) {
	s.mu.Lock()
	s.sink = w
	s.mu.Unlock()
}

func (s *ptySession) detach() {
	s.mu.Lock()
	s.sink = io.Discard
	s.mu.Unlock()
}

// ptyAttacher implements tea.ExecCommand to attach to a live PTY session.
// Ctrl+] detaches and returns to the TUI. The session keeps running.
type ptyAttacher struct {
	session *ptySession
	label   string // e.g. "owner/repo#42" — shown on attach
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

func (a *ptyAttacher) SetStdin(r io.Reader)  { a.stdin = r }
func (a *ptyAttacher) SetStdout(w io.Writer) { a.stdout = w }
func (a *ptyAttacher) SetStderr(w io.Writer) { a.stderr = w }

func (a *ptyAttacher) Run() error {
	s := a.session

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, oldState)

	// Match PTY size to terminal
	if cols, rows, err := term.GetSize(fd); err == nil {
		pty.Setsize(s.ptmx, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}

	// Forward terminal resizes to the PTY
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	doneCh := make(chan struct{})
	defer func() {
		signal.Stop(sigCh)
		close(doneCh)
	}()
	go func() {
		for {
			select {
			case <-doneCh:
				return
			case <-sigCh:
				if cols, rows, err := term.GetSize(fd); err == nil {
					pty.Setsize(s.ptmx, &pty.Winsize{
						Rows: uint16(rows),
						Cols: uint16(cols),
					})
				}
			}
		}
	}()

	s.attach(a.stdout)
	defer s.detach()

	// Print banner so the user knows which issue's PTY they connected to
	if a.label != "" {
		a.stdout.Write([]byte(fmt.Sprintf("\r\n── attached: %s (Ctrl+] to detach) ──\r\n", a.label)))
	}

	// Forward stdin → PTY, scanning for Ctrl+] (0x1d) to detach
	buf := make([]byte, 4096)
	for {
		n, err := a.stdin.Read(buf)
		if err != nil {
			return nil
		}

		for i := 0; i < n; i++ {
			if buf[i] == 0x1d {
				if i > 0 {
					s.ptmx.Write(buf[:i])
				}
				return nil // detach
			}
		}

		if s.isDone() {
			return nil
		}

		if _, err := s.ptmx.Write(buf[:n]); err != nil {
			return nil
		}
	}
}
