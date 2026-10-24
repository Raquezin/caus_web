package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/adapters/httpapi"
	"caus-platform/api/internal/adapters/postgres"
	"caus-platform/api/internal/application"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := envConfig()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCfg, err := pgxpool.ParseConfig(cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("invalid DATABASE_URL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect to database: %T", err)
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		cancel()
		return fmt.Errorf("ping database: %T", err)
	}
	cancel()

	people := application.NewPeople(postgres.NewPeopleRepository(pool))
	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", fmt.Sprint(cfg.port)),
		Handler:           httpapi.NewRouter(people, pool.Ping, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %T", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %T", err)
	}
	return nil
}
