package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const endpointReadyPath = "/api/readyz"

func newEndpointReadyServer(t *testing.T, ping func(context.Context) error) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	srv := httptest.NewServer(NewRouter(&fakePeople{}, ping, slog.New(slog.NewJSONHandler(&logs, nil))))
	t.Cleanup(srv.Close)
	return srv, &logs
}

func doEndpointReady(t *testing.T, srv *httptest.Server, method string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+endpointReadyPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestEndpointReadyPingSuccess(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			srv, _ := newEndpointReadyServer(t, func(context.Context) error {
				calls++
				return nil
			})
			resp := doEndpointReady(t, srv, method)
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if contentType := resp.Header.Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("content type %q", contentType)
			}
			if calls != 1 {
				t.Fatalf("ping calls %d", calls)
			}
			if method == http.MethodHead {
				if len(body) != 0 {
					t.Fatalf("HEAD body %q", body)
				}
				return
			}
			var payload map[string]string
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["status"] != "ok" {
				t.Fatalf("payload %v", payload)
			}
		})
	}
}

func TestEndpointReadyFailureIsSanitized(t *testing.T) {
	const secret = "password=SECRET-MARKER refused"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			srv, logs := newEndpointReadyServer(t, func(context.Context) error {
				return errors.New(secret)
			})
			resp := doEndpointReady(t, srv, method)
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if strings.Contains(logs.String(), "SECRET-MARKER") || strings.Contains(logs.String(), "refused") {
				t.Fatalf("log leaked details: %s", logs.String())
			}
			if method == http.MethodHead {
				if len(body) != 0 {
					t.Fatalf("HEAD body %q", body)
				}
				return
			}
			if strings.Contains(string(body), "SECRET-MARKER") || strings.Contains(string(body), "refused") || strings.Contains(string(body), "password") {
				t.Fatalf("body leaked details: %s", body)
			}
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != "unavailable" || payload.Error.Message != "service unavailable" {
				t.Fatalf("unexpected error payload: %s", body)
			}
		})
	}
}

func TestEndpointReadyPingDeadlineBounded(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			type pingContext struct {
				remaining time.Duration
				ok        bool
			}
			captured := make(chan pingContext, 1)
			srv, _ := newEndpointReadyServer(t, func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				var remaining time.Duration
				if ok {
					remaining = time.Until(deadline)
				}
				captured <- pingContext{remaining: remaining, ok: ok}
				return nil
			})
			resp := doEndpointReady(t, srv, method)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			var ping pingContext
			select {
			case ping = <-captured:
			case <-time.After(5 * time.Second):
				t.Fatal("ping did not receive a context")
			}
			if !ping.ok {
				t.Fatal("ping context has no deadline")
			}
			if ping.remaining <= 0 || ping.remaining > 2*time.Second {
				t.Fatalf("deadline remaining outside 2s: %v", ping.remaining)
			}
		})
	}
}

func TestEndpointReadyPropagatesRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	observed := make(chan error, 1)
	srv, _ := newEndpointReadyServer(t, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		observed <- ctx.Err()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+endpointReadyPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, _ := srv.Client().Do(req)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ping did not start")
	}
	cancel()
	select {
	case err := <-observed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ping context error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation not propagated to ping")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish")
	}
}
