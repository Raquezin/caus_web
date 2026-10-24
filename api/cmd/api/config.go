package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

type config struct {
	databaseURL string
	port        int
	logLevel    slog.Level
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{port: 8080, logLevel: slog.LevelInfo}
	cfg.databaseURL = getenv("DATABASE_URL")
	if cfg.databaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if v := getenv("PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
		cfg.port = port
	}
	switch v := getenv("LOG_LEVEL"); v {
	case "", "info":
	case "debug":
		cfg.logLevel = slog.LevelDebug
	case "warn":
		cfg.logLevel = slog.LevelWarn
	case "error":
		cfg.logLevel = slog.LevelError
	default:
		return config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	return cfg, nil
}

func envConfig() (config, error) {
	return loadConfig(os.Getenv)
}
