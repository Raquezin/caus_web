package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestCORSPublicOrigin(t *testing.T) {
	cases := map[string][]string{
		"http://localhost:8080": {
			"http://localhost:4321",
			"https://localhost:8080",
			"http://localhost:8080/path",
			"https://clubalgoritmiaus.es",
			"http://localhost",
			"null",
		},
		"https://app.example:8443": {
			"http://localhost:8080",
			"https://clubalgoritmiaus.es",
			"https://app.example",
			"http://app.example:8443",
			"https://app.example:443",
			"https://app.example:8444",
			"https://app.example:8443/path",
			"https://user@app.example:8443",
			"https://other.example:8443",
			"https://sub.app.example:8443",
			"null",
		},
		"https://[::1]": {
			"https://[0:0:0:0:0:0:0:1]",
			"https://[::1]:443",
			"https://[::1]:8443",
			"http://[::1]",
			"https://[::2]",
			"null",
		},
		"https://app.example": {
			"https://app.example:443",
			"https://app.example:8443",
			"http://app.example",
			"http://app.example:80",
			"https://app.example/path",
			"https://user@app.example",
			"https://other.example",
			"https://sub.app.example",
			"null",
		},
	}
	for origin, denied := range cases {
		t.Run(origin, func(t *testing.T) {
			people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
			log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
			auth := NewAuth(callbackProviderStub{
				exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
					return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
				},
			}, nil, AuthConfig{PublicURL: origin, SessionTTL: time.Hour}, log)
			router := NewRouter(people, func(context.Context) error { return nil }, log, auth)

			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, path := range []string{"/api/auth/google/login", "/api/people/" + people.person.ID.String()} {
					req := httptest.NewRequest(method, path, nil)
					req.Header.Set("Origin", origin)
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					want := http.StatusUnauthorized
					if path == "/api/auth/google/login" {
						want = http.StatusFound
					}
					if rec.Code != want {
						t.Fatalf("%s %s: status %d", method, path, rec.Code)
					}
					if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
						t.Fatalf("%s %s: allow-origin %q", method, path, got)
					}
				}
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				req := httptest.NewRequest(http.MethodOptions, "/api/auth/google/login", nil)
				req.Header.Set("Origin", origin)
				req.Header.Set("Access-Control-Request-Method", method)
				req.Header.Set("Access-Control-Request-Headers", "Accept, Content-Type")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusNoContent {
					t.Fatalf("preflight %s: status %d", method, rec.Code)
				}
				if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
					t.Fatalf("preflight %s: allow-origin %q", method, got)
				}
				if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD, OPTIONS" {
					t.Fatalf("preflight %s: allow-methods %q", method, got)
				}
			}
			for _, value := range denied {
				req := httptest.NewRequest(http.MethodGet, "/api/people/"+uuid.NewString(), nil)
				req.Header.Set("Origin", value)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%q: status %d, want 403", value, rec.Code)
				}
			}
		})
	}
}
