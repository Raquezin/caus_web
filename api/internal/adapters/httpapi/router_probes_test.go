package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(&fakePeople{}, nil)
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	var gotCtx context.Context
	srv, _ := newTestServer(&fakePeople{}, func(ctx context.Context) error {
		gotCtx = ctx
		return nil
	})
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/readyz")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	deadline, ok := gotCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > readyTimeout {
		t.Fatalf("expected bounded deadline within %v, got %v", readyTimeout, deadline)
	}
}

func TestReadyzFailure(t *testing.T) {
	srv, logBuf := newTestServer(&fakePeople{}, func(context.Context) error {
		return errors.New("password=SECRET-MARKER refused")
	})
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/readyz")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "SECRET-MARKER") || strings.Contains(string(body), "refused") {
		t.Fatalf("details leaked: %s", body)
	}
	if strings.Contains(logBuf.String(), "SECRET-MARKER") {
		t.Fatalf("secret leaked in logs: %s", logBuf.String())
	}
}
