package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBackendCORSSerializedRequestsLogResolvedPattern(t *testing.T) {
	f := newIntegrationFixture(t)
	talkPath := "/api/admin/talks/" + uuid.NewString()
	cases := []struct {
		name    string
		request integrationRequest
		status  int
		pattern string
	}{
		{
			name: "accepted preflight",
			request: integrationRequest{
				method:  http.MethodOptions,
				path:    talkPath,
				origins: []string{f.origin},
				headers: map[string]string{
					"Access-Control-Request-Method":  http.MethodGet,
					"Access-Control-Request-Headers": "Accept, Content-Type",
				},
			},
			status:  http.StatusNoContent,
			pattern: "/api/admin/talks/{id}",
		},
		{
			name: "denied origin",
			request: integrationRequest{
				method:  http.MethodOptions,
				path:    talkPath,
				origins: []string{"https://evil.example"},
				headers: map[string]string{"Access-Control-Request-Method": http.MethodGet},
			},
			status:  http.StatusForbidden,
			pattern: "/api/admin/talks/{id}",
		},
		{
			name: "unsupported preflight",
			request: integrationRequest{
				method:  http.MethodOptions,
				path:    "/api/talks",
				origins: []string{f.origin},
				headers: map[string]string{"Access-Control-Request-Method": http.MethodDelete},
			},
			status:  http.StatusMethodNotAllowed,
			pattern: "/api/talks",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.logs.Reset()
			status, header, body := f.do(tc.request)
			if status != tc.status {
				t.Fatalf("status %d, want %d (body %s)", status, tc.status, body)
			}
			requestID := header.Get("X-Request-ID")
			if _, err := uuid.Parse(requestID); err != nil {
				t.Fatalf("request id %q", requestID)
			}
			entries := f.logs.requestLogs(t)
			if len(entries) != 1 {
				t.Fatalf("request logs %v", entries)
			}
			entry := entries[0]
			if entry["request_id"] != requestID || entry["status"] != float64(tc.status) ||
				entry["path"] != tc.pattern || entry["method"] != http.MethodOptions {
				t.Fatalf("request log %v", entry)
			}
			if _, ok := entry["user_id"]; ok {
				t.Fatalf("unauthenticated reply logged a user_id: %v", entry)
			}
			if tc.request.path != tc.pattern && strings.Contains(f.logs.String(), tc.request.path) {
				t.Fatalf("log used the concrete path: %s", f.logs.String())
			}
		})
	}
}

func TestBackendUnknownRouteLogOmitsQuery(t *testing.T) {
	f := newIntegrationFixture(t)
	f.logs.Reset()
	status, header, body := f.do(integrationRequest{
		method: http.MethodGet, path: "/api/unknown?token=SECRET-MARKER", origins: []string{f.origin},
	})
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
	entries := f.logs.requestLogs(t)
	if len(entries) != 1 {
		t.Fatalf("request logs %v", entries)
	}
	entry := entries[0]
	if entry["path"] != "/" || entry["request_id"] != header.Get("X-Request-ID") ||
		entry["status"] != float64(http.StatusNotFound) {
		t.Fatalf("unknown route log %v", entry)
	}
	if strings.Contains(f.logs.String(), "SECRET-MARKER") {
		t.Fatalf("log leaked the query: %s", f.logs.String())
	}
}
