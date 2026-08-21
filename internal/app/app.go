package app

import (
	"context"
	"log/slog"
	"os"
	"time"
	"urbanrelay/internal/audit"
	"urbanrelay/internal/auth"
	"urbanrelay/internal/catalog"
	"urbanrelay/internal/clock"
	"urbanrelay/internal/config"
	"urbanrelay/internal/httpapi"
	"urbanrelay/internal/idempotency"
	"urbanrelay/internal/incident"
	"urbanrelay/internal/inspection"
	"urbanrelay/internal/jobqueue"
	"urbanrelay/internal/maintenance"
	"urbanrelay/internal/mission"
	"urbanrelay/internal/outbox"
	"urbanrelay/internal/platformdb"
	"urbanrelay/internal/reservation"
	"urbanrelay/internal/sorting"
	"urbanrelay/internal/transfer"
	"urbanrelay/internal/worker"
)

type App struct {
	Config       config.Config
	DB           *platformdb.DB
	Auth         *auth.Service
	Catalog      *catalog.Service
	Reservations *reservation.Service
	Jobs         *jobqueue.Repository
	HTTP         *httpapi.Server
	Worker       *worker.Worker
	JobRunner    *jobqueue.Runner
	Logger       *slog.Logger
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	db, err := platformdb.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	clk := clock.Real{}
	if err := auth.SeedUsers(ctx, db.SQL, clk.Now()); err != nil {
		db.Close()
		return nil, err
	}
	if err := catalog.Seed(ctx, db.SQL, clk.Now()); err != nil {
		db.Close()
		return nil, err
	}
	authService := auth.NewService(db.SQL, clk, cfg.SessionTTL)
	auditRepo := audit.NewRepository(db.SQL)
	idem := idempotency.NewRepository(db.SQL)
	_ = idem
	events := outbox.NewRepository(db.SQL)
	catalogRepo := catalog.NewRepository(db.SQL)
	catalogService := catalog.NewService(db.SQL, catalogRepo, auditRepo, events, clk)
	reservationRepo := reservation.NewRepository(db.SQL)
	reservationService := reservation.NewService(db.SQL, reservationRepo, auditRepo, events, clk)
	jobs := jobqueue.NewRepository(db.SQL)
	business := httpapi.BusinessServices{
		Mission:     mission.NewService(db.SQL, mission.NewRepository(db.SQL), auditRepo, idem, events, clk),
		Inspection:  inspection.NewService(db.SQL, inspection.NewRepository(db.SQL), auditRepo, idem, events, clk),
		Transfer:    transfer.NewService(db.SQL, transfer.NewRepository(db.SQL), auditRepo, idem, events, clk),
		Sorting:     sorting.NewService(db.SQL, sorting.NewRepository(db.SQL), auditRepo, idem, events, clk),
		Incident:    incident.NewService(db.SQL, incident.NewRepository(db.SQL), auditRepo, idem, events, clk),
		Maintenance: maintenance.NewService(db.SQL, maintenance.NewRepository(db.SQL), auditRepo, idem, events, clk),
	}
	publisher := worker.NewLogPublisher(logger)
	background := worker.New(events, publisher, clk, cfg.WorkerPoll, cfg.WorkerAttempts, logger)
	jobRunner := jobqueue.NewRunner(jobs, jobqueue.NewDispatchHandler(logger), clk, "urbanrelay-local", cfg.WorkerPoll, 30*time.Second, 4, logger)
	server := httpapi.New(db, authService, business, logger)
	return &App{Config: cfg, DB: db, Auth: authService, Catalog: catalogService, Reservations: reservationService, Jobs: jobs, HTTP: server, Worker: background, JobRunner: jobRunner, Logger: logger}, nil
}
func (a *App) Close() error { return a.DB.Close() }
