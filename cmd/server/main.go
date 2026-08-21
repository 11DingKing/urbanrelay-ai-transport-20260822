package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"urbanrelay/internal/app"
	"urbanrelay/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	application, err := app.New(ctx, cfg)
	if err != nil {
		slog.Error("initialize application", "error", err)
		os.Exit(1)
	}
	defer application.Close()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	application.Worker.Run(workerCtx)
	application.JobRunner.Run(workerCtx)
	server := &http.Server{Addr: cfg.Addr, Handler: application.HTTP.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	serveErr := make(chan error, 1)
	go func() {
		application.Logger.Info("server listening", "addr", cfg.Addr)
		serveErr <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		application.Logger.Info("shutdown requested")
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			application.Logger.Error("server stopped", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	cancelWorker()
	application.Worker.Wait()
	application.JobRunner.Wait()
}
