package main

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	values := map[string]string{
		"DATABASE_URL":         "postgres://x",
		"PUBLIC_URL":           "http://localhost:8080",
		"GOOGLE_CLIENT_ID":     "test-client",
		"GOOGLE_CLIENT_SECRET": "test-secret",
	}
	for key, value := range vars {
		values[key] = value
	}
	return func(k string) string { return values[k] }
}

func TestMissingDatabaseURL(t *testing.T) {
	if _, err := loadConfig(env(map[string]string{"DATABASE_URL": ""})); err == nil {
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
	if cfg.publicURL != "http://localhost:8080" || cfg.sessionTTL != 720*time.Hour ||
		cfg.googleClientID != "test-client" || cfg.googleClientSecret != "test-secret" || len(cfg.adminEmails) != 0 {
		t.Fatal("unexpected OAuth defaults")
	}
}

func TestRequiredOAuthConfig(t *testing.T) {
	for _, key := range []string{"PUBLIC_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"} {
		t.Run(key, func(t *testing.T) {
			_, err := loadConfig(env(map[string]string{key: ""}))
			if err == nil || !strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("missing %s: error %v", key, err)
			}
		})
	}
}

func TestPublicURL(t *testing.T) {
	for input, want := range map[string]string{
		"https://clubalgoritmiaus.es":  "https://clubalgoritmiaus.es",
		"https://clubalgoritmiaus.es/": "https://clubalgoritmiaus.es",
		"https://api.example.com:8443": "https://api.example.com:8443",
		"https://localhost:4321":       "https://localhost:4321",
		"http://localhost:8080":        "http://localhost:8080",
		"http://localhost:4321/":       "http://localhost:4321",
		"https://[::1]:443":            "https://[::1]:443",
	} {
		t.Run(input, func(t *testing.T) {
			cfg, err := loadConfig(env(map[string]string{"PUBLIC_URL": input}))
			if err != nil || cfg.publicURL != want {
				t.Fatalf("PUBLIC_URL %q: got %q error %v", input, cfg.publicURL, err)
			}
		})
	}
	for _, input := range []string{
		"http://clubalgoritmiaus.es", "http://localhost", "http://127.0.0.1:8080",
		"https://", "https:///example.com", "//example.com", "ftp://example.com",
		"https://user:password@example.com", "https://example.com/path", "https://example.com//",
		"https://example.com/?q=x", "https://example.com?", "https://example.com#",
		"https://example.com/#fragment", "https://example.com:", "https://example.com:0",
		"https://example.com:65536", "https://example.com:abc", "https://example.com:+80",
		"https://[localhost]:8080", "https://[example.com]", "https://bad_host",
		"https://-host.example", "https://host-.example", "https://example..com",
		"https://example.com\\@evil.test", " https://example.com", "https://example.com\n",
		"https://example.com/%2f", "https://example.com/%2e", "http://localhost:9999999999999999999999",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := loadConfig(env(map[string]string{"PUBLIC_URL": input})); err == nil {
				t.Fatalf("accepted PUBLIC_URL %q", input)
			}
		})
	}
}

func TestSessionTTL(t *testing.T) {
	for input, want := range map[string]time.Duration{"1s": time.Second, "48h": 48 * time.Hour, "8760h": 8760 * time.Hour} {
		cfg, err := loadConfig(env(map[string]string{"SESSION_TTL": input}))
		if err != nil || cfg.sessionTTL != want {
			t.Fatalf("SESSION_TTL %q: got %v error %v", input, cfg.sessionTTL, err)
		}
	}
	for _, input := range []string{"0", "-1h", "1ns", "8761h", "999999999999999h", "30d", "abc"} {
		if _, err := loadConfig(env(map[string]string{"SESSION_TTL": input})); err == nil {
			t.Fatalf("accepted SESSION_TTL %q", input)
		}
	}
}

func TestAdminEmails(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"ADMIN_EMAILS": " Ada@Example.com, grace@example.com,ada@example.com "}))
	if err != nil || !reflect.DeepEqual(cfg.adminEmails, []string{"ada@example.com", "grace@example.com"}) {
		t.Fatalf("emails %v error %v", cfg.adminEmails, err)
	}
	for _, input := range []string{"not-an-email", "Ada <ada@example.com>", "ada@example.com,", ",ada@example.com", " ", "a@example.com,bad"} {
		if _, err := loadConfig(env(map[string]string{"ADMIN_EMAILS": input})); err == nil {
			t.Fatalf("accepted ADMIN_EMAILS %q", input)
		}
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
