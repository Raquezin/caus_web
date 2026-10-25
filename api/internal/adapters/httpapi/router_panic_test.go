package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestPanicRecovery(t *testing.T) {
	people := &panicPeople{}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Role: "admin"},
	}, "https://people.test")
	router := NewRouter(people, func(context.Context) error { return nil }, log, auth)

	req := sessionRequest(http.MethodGet, "/api/people/"+uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers on panic response")
	}
	if strings.Contains(rec.Body.String(), "panic") || strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatalf("panic details leaked: %s", rec.Body.String())
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Error.Code != "internal_error" {
		t.Fatalf("code %q", parsed.Error.Code)
	}
	var requestLogs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if entry["msg"] == "request" {
			requestLogs = append(requestLogs, entry)
		}
	}
	if len(requestLogs) != 1 {
		t.Fatalf("expected exactly one request log, got %d: %s", len(requestLogs), buf.String())
	}
	if requestLogs[0]["status"] != float64(500) || requestLogs[0]["path"] != "/api/people/{id}" {
		t.Fatalf("request log %v", requestLogs[0])
	}
}

type panicPeople struct{}

func (panicPeople) GetByID(context.Context, uuid.UUID) (domain.Person, error) {
	panic("SECRET-MARKER boom")
}

func (panicPeople) List(context.Context, domain.Page) (domain.PeoplePage, error) {
	panic("SECRET-MARKER boom")
}
