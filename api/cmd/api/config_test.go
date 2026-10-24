package main

import (
	"log/slog"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestMissingDatabaseURL(t *testing.T) {
	if _, err := loadConfig(env(nil)); err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.port != 8080 || cfg.logLevel != slog.LevelInfo {
		t.Fatalf("got port=%d level=%v", cfg.port, cfg.logLevel)
	}
}

func TestValidOverrides(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"DATABASE_URL": "postgres://x", "PORT": "9090", "LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.port != 9090 || cfg.logLevel != slog.LevelDebug {
		t.Fatalf("got port=%d level=%v", cfg.port, cfg.logLevel)
	}
}

func TestInvalidPort(t *testing.T) {
	for _, v := range []string{"abc", "0", "65536", "-1"} {
		if _, err := loadConfig(env(map[string]string{
			"DATABASE_URL": "postgres://x", "PORT": v,
		})); err == nil {
			t.Fatalf("expected error for PORT=%q", v)
		}
	}
}

func TestLogLevels(t *testing.T) {
	for v, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo,
		"warn": slog.LevelWarn, "error": slog.LevelError,
	} {
		cfg, err := loadConfig(env(map[string]string{
			"DATABASE_URL": "postgres://x", "LOG_LEVEL": v,
		}))
		if err != nil || cfg.logLevel != want {
			t.Fatalf("LOG_LEVEL=%q: cfg %v err %v", v, cfg, err)
		}
	}
	for _, v := range []string{"verbose", "INFO+1", "DEBUG-2", "warn+3"} {
		if _, err := loadConfig(env(map[string]string{
			"DATABASE_URL": "postgres://x", "LOG_LEVEL": v,
		})); err == nil {
			t.Fatalf("expected error for LOG_LEVEL=%q", v)
		}
	}
}
