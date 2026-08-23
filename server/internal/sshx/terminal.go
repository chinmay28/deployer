package sshx

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Term is what the shell is told it is talking to. 256-colour because anything
// less renders a modern prompt as a line of escape codes.
const Term = "xterm-256color"

// ErrTerminalClosed is what a read returns once Close has been called, as
// opposed to the shell having exited on its own.
var ErrTerminalClosed = errors.New("terminal closed")

// Terminal is an interactive login shell running on a pseudo-terminal on the
// host. Unlike Run it does not end with a command: it stays open, carrying
// keystrokes one way and screen updates the other, until the shell exits or
// Close is called.
//
// It reads as the screen and writes as the keyboard. A pty merges the shell's
// stderr into its output on the host's side, so a read is the whole screen
// rather than only stdout — which is also why the two can never arrive out of
// order.
type Terminal struct {
	out     io.Reader
	session *ssh.Session
	stdin   io.WriteCloser
	pipe    *io.PipeWriter

	done chan struct{}
	// Written before done is closed, read after: no lock needed.
	exitCode int

	mu     sync.Mutex
	closed bool
}

// Shell requests a pty of the given size and starts the login shell on it. The
// caller must Close the Terminal; that leaves the Client open, since one
// connection can carry more than one session.
func (c *Client) Shell(cols, rows int) (*Terminal, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	// ECHO on because the shell is the thing that echoes: without it every
	// keystroke would have to be drawn locally and guessed at, and a password
	// prompt could no longer stop it being drawn at all.
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.ICRNL:         1,
		ssh.ONLCR:         1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}
	if err := session.RequestPty(Term, rows, cols, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("request a terminal: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("open stdin: %w", err)
	}

	// An unbuffered pipe rather than a growing buffer, so a client that stops
	// reading slows the host down instead of filling this process's memory.
	// The session's own goroutines are what block, and Close unblocks them.
	pr, pw := io.Pipe()
	var mu sync.Mutex
	w := &lockedWriter{mu: &mu, w: pw}
	session.Stdout = w
	session.Stderr = w

	if err := session.Shell(); err != nil {
		session.Close()
		pw.Close()
		return nil, fmt.Errorf("start the shell: %w", err)
	}

	t := &Terminal{out: pr, session: session, stdin: stdin, pipe: pw, done: make(chan struct{})}
	go func() {
		err := session.Wait()
		var exitErr *ssh.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr):
			t.exitCode = exitErr.ExitStatus()
		default:
			t.exitCode = -1
		}
		// The pipe has no buffer, so every write has already been read: closing
		// it now loses nothing and turns the reader's next call into EOF.
		pw.Close()
		close(t.done)
	}()
	return t, nil
}

// Read takes whatever the shell has drawn. It ends in io.EOF when the shell
// exited on its own and in ErrTerminalClosed when Close was what ended it.
func (t *Terminal) Read(p []byte) (int, error) { return t.out.Read(p) }

// Write sends keystrokes to the shell.
func (t *Terminal) Write(p []byte) (int, error) { return t.stdin.Write(p) }

// Resize tells the shell its window changed, which is what makes a full-screen
// program redraw at the new size rather than into the old one.
func (t *Terminal) Resize(cols, rows int) error { return t.session.WindowChange(rows, cols) }

// Status blocks until the shell has exited and returns its status. -1 means it
// ended without one — killed by a signal, or the connection went away.
func (t *Terminal) Status() int {
	<-t.done
	return t.exitCode
}

// Close ends the session. Safe to call more than once, and safe to call while
// another goroutine is blocked reading the screen or writing to the host.
func (t *Terminal) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	t.stdin.Close()
	err := t.session.Close()
	// Unblocks the session's copy goroutines if nobody is reading the screen,
	// which is what lets Wait return and the connection go.
	t.pipe.CloseWithError(ErrTerminalClosed)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
