// Command deployer runs the Deployer server: REST API plus the PWA, from a
// single binary.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chinmay28/deployer/server/internal/api"
	"github.com/chinmay28/deployer/server/internal/deploy"
	"github.com/chinmay28/deployer/server/internal/hosts"
	"github.com/chinmay28/deployer/server/internal/sshx"
	"github.com/chinmay28/deployer/server/internal/store"
	"github.com/chinmay28/deployer/server/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		addr    = flag.String("addr", envOr("DEPLOYER_ADDR", ":8899"), "listen address (host:port)")
		dbPath  = flag.String("db", envOr("DEPLOYER_DB", "data/deployer.db"), "path to the SQLite database")
		pin     = flag.String("pin", os.Getenv("DEPLOYER_PIN"), "optional PIN required to use the UI; empty disables authentication")
		verbose = flag.Bool("v", false, "verbose logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	identity, err := sshx.EnsureIdentity(ctx, db)
	if err != nil {
		return err
	}
	log.Info("ssh identity ready", "fingerprint", identity.Fingerprint())

	hostSvc := hosts.NewService(db, identity)
	poller := hosts.NewPoller(hostSvc, db, log)
	go poller.Run(ctx)

	// Deployments cannot survive a restart, so make sure none are left
	// claiming to be running.
	if n, err := db.InterruptRunningDeployments(ctx); err != nil {
		return err
	} else if n > 0 {
		log.Warn("marked deployments as interrupted after restart", "count", n)
	}

	health := deploy.NewChecker(db, hostSvc, log)
	go health.Run(ctx)
	runner := deploy.NewRunner(db, hostSvc, health, log)

	auth := api.NewPinAuth(*pin)
	apiSrv := &api.Server{
		DB: db, Hosts: hostSvc, Poller: poller,
		Runner: runner, Health: health, Log: log, Auth: auth,
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", apiSrv.Routes())
	mux.Handle("/", web.Handler())

	srv := &http.Server{
		Addr:              *addr,
		Handler:           auth.Middleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: deployment log streaming holds responses open.
		IdleTimeout: 2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("deployer listening", "addr", *addr, "db", *dbPath, "auth", authMode(auth))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Stop in-flight deployments first so each one records an outcome instead
	// of being left as "running" in the database.
	runner.Shutdown(shutdownCtx)
	return srv.Shutdown(shutdownCtx)
}

func authMode(a *api.PinAuth) string {
	if a == nil {
		return "none"
	}
	return "pin"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
