package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func EndpointHealthServer(t *testing.T, ping func(context.Context) error) *httptest.Server {
	t.Helper()
	router := NewRouter(&fakePeople{}, ping, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func EndpointHealthRequest(t *testing.T, srv *httptest.Server, method string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+"/api/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func EndpointHealthStatus(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid health JSON %q: %v", body, err)
	}
	return payload
}

func TestEndpointHealthGetReportsProcessUp(t *testing.T) {
	pingCalls := 0
	srv := EndpointHealthServer(t, func(context.Context) error {
		pingCalls++
		return errors.New("database down")
	})

	resp, body := EndpointHealthRequest(t, srv, http.MethodGet)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q, want application/json", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %v", resp.Header)
	}
	payload := EndpointHealthStatus(t, body)
	if len(payload) != 1 || payload["status"] != "ok" {
		t.Fatalf("payload %v, want only status ok", payload)
	}
	if string(body) != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body %q, want exact status document", body)
	}
	if pingCalls != 0 {
		t.Fatalf("ping called %d times, healthz must not probe dependencies", pingCalls)
	}
}

func TestEndpointHealthNilPingIsServed(t *testing.T) {
	srv := EndpointHealthServer(t, nil)

	resp, body := EndpointHealthRequest(t, srv, http.MethodGet)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 with nil ping", resp.StatusCode)
	}
	if payload := EndpointHealthStatus(t, body); payload["status"] != "ok" {
		t.Fatalf("payload %v, want status ok", payload)
	}
}

func TestEndpointHealthHeadHasNoBody(t *testing.T) {
	pingCalls := 0
	srv := EndpointHealthServer(t, func(context.Context) error {
		pingCalls++
		return nil
	})

	resp, body := EndpointHealthRequest(t, srv, http.MethodHead)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q, want application/json", ct)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD body %q, want empty", body)
	}
	if pingCalls != 0 {
		t.Fatalf("ping called %d times, healthz must not probe dependencies", pingCalls)
	}
}

func TestEndpointHealthWrongMethodIsJSON405(t *testing.T) {
	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		pingCalls := 0
		srv := EndpointHealthServer(t, func(context.Context) error {
			pingCalls++
			return nil
		})

		resp, body := EndpointHealthRequest(t, srv, method)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status %d, want 405", method, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("%s: content-type %q, want application/json", method, ct)
		}
		allow := resp.Header.Get("Allow")
		if allow != "GET, HEAD" {
			t.Fatalf("%s: Allow %q, want GET, HEAD", method, allow)
		}
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("%s: invalid error JSON %q: %v", method, body, err)
		}
		if payload.Error.Code != "method_not_allowed" || strings.TrimSpace(payload.Error.Message) == "" {
			t.Fatalf("%s: error %+v, want method_not_allowed with message", method, payload.Error)
		}
		if pingCalls != 0 {
			t.Fatalf("%s: ping called %d times, healthz must not probe dependencies", method, pingCalls)
		}
	}
}
