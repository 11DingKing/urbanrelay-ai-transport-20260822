package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr            string
	DataDir         string
	LogLevel        string
	SessionTTL      time.Duration
	WorkerPoll      time.Duration
	WorkerAttempts  int
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Addr:            env("URBANRELAY_ADDR", ":8080"),
		DataDir:         env("URBANRELAY_DATA_DIR", "./data"),
		LogLevel:        env("URBANRELAY_LOG_LEVEL", "info"),
		WorkerAttempts:  4,
		ShutdownTimeout: 10 * time.Second,
	}
	var err error
	if cfg.SessionTTL, err = duration("URBANRELAY_SESSION_TTL", 8*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerPoll, err = duration("URBANRELAY_WORKER_POLL", 250*time.Millisecond); err != nil {
		return Config{}, err
	}
	if raw := os.Getenv("URBANRELAY_WORKER_ATTEMPTS"); raw != "" {
		cfg.WorkerAttempts, err = strconv.Atoi(raw)
		if err != nil || cfg.WorkerAttempts < 1 {
			return Config{}, fmt.Errorf("URBANRELAY_WORKER_ATTEMPTS must be positive")
		}
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}
