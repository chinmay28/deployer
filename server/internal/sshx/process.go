package sshx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Process is a command running on the host with its standard streams held
// open: writes go to its stdin, reads come from its stdout, and it stays up
// until it exits or Close is called. It is what a Terminal is without the pty
// — which is what a program that speaks a line protocol wants, because a pty
// would echo every line back and dress the output in escape codes.
//
// Stderr is not streamed. It is kept, bounded, for the moment the process ends
// without saying why on stdout: a program that speaks JSON on stdout says its
// complaints on stderr, and the last of them is the most useful thing to have.
type Process struct {
	out     io.Reader
	session *ssh.Session
	stdin   io.WriteCloser

	done chan struct{}
	// Written before done is closed, read after: no lock needed.
	exitCode int

	mu     sync.Mutex
	closed bool
	stderr tailBuffer
}

// stderrKeep is how much of the end of stderr a Process holds on to.
const stderrKeep = 16 << 10

// Start runs cmd on the host and returns while it is running. The caller must
// Close the Process; that leaves the Client open, since one connection can
// carry more than one session.
func (c *Client) Start(cmd string) (*Process, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("open stdout: %w", err)
	}
	p := &Process{out: stdout, session: session, stdin: stdin, done: make(chan struct{})}
	p.stderr.keep = stderrKeep
	session.Stderr = &p.stderr

	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, fmt.Errorf("start %q: %w", firstLine(cmd), err)
	}
	go func() {
		err := session.Wait()
		var exitErr *ssh.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr):
			p.exitCode = exitErr.ExitStatus()
		default:
			p.exitCode = -1
		}
		close(p.done)
	}()
	return p, nil
}

// Read takes what the process has written to stdout. It ends in io.EOF when
// the process closes its end, which is what exiting does.
func (p *Process) Read(b []byte) (int, error) { return p.out.Read(b) }

// Write sends bytes to the process's stdin.
func (p *Process) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Status blocks until the process has exited and returns its status. -1 means
// it ended without one — killed by a signal, or the connection went away.
func (p *Process) Status() int {
	<-p.done
	return p.exitCode
}

// Stderr is the tail of what the process wrote to stderr so far.
func (p *Process) Stderr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stderr.String()
}

// Close ends the process: its stdin is closed, which is the polite way to end
// a program reading it, and then the session is torn down, which is the other
// way. Safe to call more than once.
func (p *Process) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	p.stdin.Close()
	_ = p.session.Signal(ssh.SIGTERM)
	err := p.session.Close()
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// tailBuffer keeps the last keep bytes written to it.
type tailBuffer struct {
	buf  bytes.Buffer
	keep int
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.buf.Write(b)
	if t.buf.Len() > t.keep {
		rest := t.buf.Bytes()[t.buf.Len()-t.keep:]
		t.buf = *bytes.NewBuffer(append([]byte(nil), rest...))
	}
	return len(b), nil
}

func (t *tailBuffer) String() string { return t.buf.String() }
