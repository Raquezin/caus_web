package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	databaseURL        string
	port               int
	logLevel           slog.Level
	publicURL          string
	googleClientID     string
	googleClientSecret string
	adminEmails        []string
	sessionTTL         time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{port: 8080, logLevel: slog.LevelInfo, sessionTTL: 720 * time.Hour}
	cfg.databaseURL = getenv("DATABASE_URL")
	if cfg.databaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	cfg.publicURL = getenv("PUBLIC_URL")
	if cfg.publicURL == "" {
		return config{}, fmt.Errorf("PUBLIC_URL is required")
	}
	if !validPublicURL(cfg.publicURL) {
		return config{}, fmt.Errorf("PUBLIC_URL must be an HTTPS origin or HTTP localhost with an explicit port")
	}
	cfg.publicURL = strings.TrimSuffix(cfg.publicURL, "/")
	cfg.googleClientID = getenv("GOOGLE_CLIENT_ID")
	if strings.TrimSpace(cfg.googleClientID) == "" {
		return config{}, fmt.Errorf("GOOGLE_CLIENT_ID is required")
	}
	cfg.googleClientSecret = getenv("GOOGLE_CLIENT_SECRET")
	if strings.TrimSpace(cfg.googleClientSecret) == "" {
		return config{}, fmt.Errorf("GOOGLE_CLIENT_SECRET is required")
	}
	if v := getenv("SESSION_TTL"); v != "" {
		ttl, err := time.ParseDuration(v)
		if err != nil || ttl < time.Second || ttl > 8760*time.Hour {
			return config{}, fmt.Errorf("SESSION_TTL must be a duration between 1s and 8760h")
		}
		cfg.sessionTTL = ttl
	}
	if v := getenv("ADMIN_EMAILS"); v != "" {
		seen := make(map[string]bool)
		for _, entry := range strings.Split(v, ",") {
			email := strings.ToLower(strings.TrimSpace(entry))
			address, err := mail.ParseAddress(email)
			if err != nil || address.Address != email {
				return config{}, fmt.Errorf("ADMIN_EMAILS must contain comma-separated email addresses")
			}
			if !seen[email] {
				cfg.adminEmails = append(cfg.adminEmails, email)
				seen[email] = true
			}
		}
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

func validPublicURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" ||
		strings.ContainsAny(value, "?#\\") {
		return false
	}
	host := u.Hostname()
	if !validHostname(host) {
		return false
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		for _, digit := range port {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
		authority += ":" + port
	}
	if u.Host != authority || value != u.Scheme+"://"+u.Host+u.Path {
		return false
	}
	return u.Scheme == "https" || (host == "localhost" && u.Port() != "")
}

func validHostname(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func envConfig() (config, error) {
	return loadConfig(os.Getenv)
}
