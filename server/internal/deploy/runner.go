package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chinmay28/deployer/server/internal/hosts"
	"github.com/chinmay28/deployer/server/internal/store"
)

// Timeout bounds a single deployment. Installs that build from source on a Pi
// can take a while, so this is generous.
const Timeout = 45 * time.Minute

// maxLogBytes caps what is retained per deployment. Output past the cap is
// still streamed live, just not stored.
const maxLogBytes = 1 << 20

// ErrAlreadyRunning means this app is already deploying to this host.
var ErrAlreadyRunning = errors.New("a deployment for this app and host is already running")

// Runner starts deployments and keeps their live output available.
type Runner struct {
	db     *store.DB
	hosts  *hosts.Service
	health *Checker
	log    *slog.Logger

	mu     sync.Mutex
	active map[int64]*Run     // by deployment id
	busy   map[[2]int64]int64 // app+host -> deployment id
}

// NewRunner builds a Runner.
func NewRunner(db *store.DB, h *hosts.Service, health *Checker, log *slog.Logger) *Runner {
	return &Runner{
		db:     db,
		hosts:  h,
		health: health,
		log:    log,
		active: map[int64]*Run{},
		busy:   map[[2]int64]int64{},
	}
}

// Run is a deployment in flight, with its output fanned out to subscribers.
type Run struct {
	ID int64
	// detached marks a run whose command lives on the host rather than in this
	// process, so a shutdown must leave it alone.
	detached bool

	cancel context.CancelFunc
	done   chan struct{}
	// abandon tells a detached follower to stop watching without recording an
	// outcome, so the next process owns the deployment outright.
	abandon chan struct{}

	mu        sync.Mutex
	buf       []byte
	truncated bool
	subs      map[chan []byte]bool
}

func newRun(id int64, cancel context.CancelFunc) *Run {
	return &Run{
		ID:      id,
		cancel:  cancel,
		done:    make(chan struct{}),
		abandon: make(chan struct{}),
		subs:    map[chan []byte]bool{},
	}
}

// Write records output and fans it out. It satisfies io.Writer for the SSH
// session.
func (r *Run) Write(p []byte) (int, error) {
	r.mu.Lock()
	if len(r.buf)+len(p) <= maxLogBytes {
		r.buf = append(r.buf, p...)
	} else if !r.truncated {
		r.truncated = true
		room := maxLogBytes - len(r.buf)
		if room > 0 {
			r.buf = append(r.buf, p[:room]...)
		}
		r.buf = append(r.buf, "\n[output truncated]\n"...)
	}
	chunk := make([]byte, len(p))
	copy(chunk, p)
	for ch := range r.subs {
		select {
		case ch <- chunk:
		default:
			// A subscriber that cannot keep up is dropped; the client
			// reconnects and replays the log from the start.
			delete(r.subs, ch)
			close(ch)
		}
	}
	r.mu.Unlock()
	return len(p), nil
}

// Subscribe returns the output so far plus a channel of subsequent chunks. The
// channel closes when the deployment ends. Call the returned function to
// detach early.
func (r *Run) Subscribe() (backlog []byte, updates <-chan []byte, cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	backlog = make([]byte, len(r.buf))
	copy(backlog, r.buf)
	ch := make(chan []byte, 256)
	r.subs[ch] = true
	return backlog, ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.subs[ch] {
			delete(r.subs, ch)
			close(ch)
		}
	}
}

// Done is closed when the deployment finishes.
func (r *Run) Done() <-chan struct{} { return r.done }

func (r *Run) finish() {
	r.mu.Lock()
	for ch := range r.subs {
		delete(r.subs, ch)
		close(ch)
	}
	r.mu.Unlock()
	close(r.done)
}

func (r *Run) log() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// Start renders the install command, records a deployment and runs it in the
// background. It returns as soon as the deployment row exists.
func (rn *Runner) Start(ctx context.Context, appID, hostID int64, submitted map[string]string) (*store.Deployment, error) {
	app, err := rn.db.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	host, err := rn.db.GetHost(ctx, hostID)
	if err != nil {
		return nil, err
	}
	command, params, err := BuildCommand(app, host, submitted)
	if err != nil {
		return nil, err
	}

	key := [2]int64{appID, hostID}
	rn.mu.Lock()
	if _, busy := rn.busy[key]; busy {
		rn.mu.Unlock()
		return nil, ErrAlreadyRunning
	}
	rn.busy[key] = 0 // reserved; filled in with the id below
	rn.mu.Unlock()

	dep, err := rn.db.CreateDeployment(ctx, &store.Deployment{
		AppID: appID, HostID: hostID, Command: command, Params: params,
	})
	if err != nil {
		rn.mu.Lock()
		delete(rn.busy, key)
		rn.mu.Unlock()
		return nil, err
	}

	// Updating Deployer on the machine Deployer runs on restarts the process
	// watching the deployment, so that one runs detached and is followed
	// through a file on the host. The log path needs the id, which only exists
	// once the row does.
	detached := app.SelfUpdate && host.IsSelf
	if detached {
		dep.DetachedLog = detachedLogPath(dep.ID)
		if err := rn.db.SetDetachedLog(ctx, dep.ID, dep.DetachedLog); err != nil {
			rn.mu.Lock()
			delete(rn.busy, key)
			rn.mu.Unlock()
			return nil, err
		}
		dep.AppName, dep.HostName = app.Name, host.Name
	}

	// The deployment outlives the request that started it.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), Timeout)
	run := newRun(dep.ID, cancel)

	rn.mu.Lock()
	rn.busy[key] = dep.ID
	rn.active[dep.ID] = run
	rn.mu.Unlock()

	if detached {
		run.detached = true
		go rn.runDetached(runCtx, cancel, run, dep, host, false)
	} else {
		go rn.execute(runCtx, cancel, run, app, host, dep, params)
	}
	return dep, nil
}

func (rn *Runner) execute(ctx context.Context, cancel context.CancelFunc, run *Run, app *store.App, host *store.Host, dep *store.Deployment, params map[string]string) {
	defer cancel()
	defer func() {
		rn.mu.Lock()
		delete(rn.active, dep.ID)
		delete(rn.busy, [2]int64{app.ID, host.ID})
		rn.mu.Unlock()
		run.finish()
	}()

	fmt.Fprintf(run, "==> Deploying %s to %s (%s@%s)\n$ %s\n\n",
		app.Name, host.Name, host.Username, host.Address, dep.Command)

	status, exitCode, failure := rn.runCommand(ctx, run, host, dep.Command)

	// Persist with a context that is still alive even if the run was cancelled.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer saveCancel()

	switch status {
	case store.DeploySucceeded:
		fmt.Fprintf(run, "\n==> Succeeded in %s\n", time.Since(dep.StartedAt).Round(time.Second))
	case store.DeployCanceled:
		fmt.Fprintf(run, "\n==> Canceled\n")
	default:
		fmt.Fprintf(run, "\n==> Failed: %s\n", failure)
	}

	if err := rn.db.FinishDeployment(saveCtx, dep.ID, status, exitCode, failure, run.log()); err != nil {
		rn.log.Error("deploy: record outcome", "deployment", dep.ID, "err", err)
	}
	if status != store.DeploySucceeded {
		rn.log.Warn("deployment did not succeed", "app", app.Name, "host", host.Name, "status", status, "err", failure)
		return
	}

	if err := rn.db.UpsertInstallation(saveCtx, app.ID, host.ID, params, dep.ID); err != nil {
		rn.log.Error("deploy: record installation", "deployment", dep.ID, "err", err)
		return
	}
	rn.log.Info("deployment succeeded", "app", app.Name, "host", host.Name)
	if rn.health != nil {
		// Give the app a moment to come up before judging it.
		rn.health.CheckSoon(app.ID, host.ID, 5*time.Second)
	}
}

// runCommand executes the install command, returning the deployment status.
func (rn *Runner) runCommand(ctx context.Context, run *Run, host *store.Host, command string) (status string, exitCode *int, failure string) {
	client, err := rn.hosts.Connect(ctx, host)
	if err != nil {
		// A cancel during the connect phase is still a cancel, not a failure.
		if status, failure, ok := interrupted(ctx); ok {
			return status, nil, failure
		}
		fmt.Fprintf(run, "!! Could not connect: %v\n", err)
		return store.DeployFailed, nil, err.Error()
	}
	defer client.Close()

	code, err := client.Stream(ctx, WrapCommand(command), run)
	if err != nil {
		if status, failure, ok := interrupted(ctx); ok {
			return status, nil, failure
		}
		return store.DeployFailed, nil, err.Error()
	}
	if code != 0 {
		return store.DeployFailed, &code, fmt.Sprintf("install command exited with status %d", code)
	}
	return store.DeploySucceeded, &code, ""
}

// interrupted maps a cancelled or expired context onto a deployment outcome.
func interrupted(ctx context.Context) (status, failure string, ok bool) {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return store.DeployCanceled, "canceled", true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return store.DeployFailed, fmt.Sprintf("timed out after %s", Timeout), true
	}
	return "", "", false
}

// Active returns the in-flight run for a deployment, if any.
func (rn *Runner) Active(deploymentID int64) *Run {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.active[deploymentID]
}

// Cancel stops a running deployment. The remote command is killed, but work it
// already did on the host is not undone.
func (rn *Runner) Cancel(deploymentID int64) error {
	rn.mu.Lock()
	run := rn.active[deploymentID]
	rn.mu.Unlock()
	if run == nil {
		return store.ErrNotFound
	}
	run.cancel()
	return nil
}

// Shutdown cancels in-flight deployments and waits for them to record their
// outcome.
//
// Detached deployments are deliberately left alone. `systemctl restart
// deployer` — which is what a self-update does — arrives here as a SIGTERM,
// and cancelling then would mark an update that is running perfectly well on
// the host as canceled. Leaving the row running is what lets the next process
// pick it back up.
func (rn *Runner) Shutdown(ctx context.Context) {
	rn.mu.Lock()
	var runs, left []*Run
	for _, r := range rn.active {
		if r.detached {
			left = append(left, r)
			continue
		}
		runs = append(runs, r)
	}
	rn.mu.Unlock()

	if len(left) > 0 {
		rn.log.Info("leaving detached deployments running; they resume on restart", "count", len(left))
		// Stop watching without recording anything. Without this the old
		// follower and the resumed one would both finalize the same
		// deployment, and whichever finished last would win.
		for _, r := range left {
			close(r.abandon)
		}
	}

	for _, r := range runs {
		r.cancel()
	}
	for _, r := range runs {
		select {
		case <-r.Done():
		case <-ctx.Done():
			return
		}
	}
}
