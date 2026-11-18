package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestRateLimitAppliesOnlyToAuthReadsAndWrites(t *testing.T) {
	person := domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}
	peoplePath := "/api/people/" + person.ID.String()
	cases := []struct {
		name        string
		method      string
		path        string
		wantLimited bool
	}{
		{"get public read", http.MethodGet, peoplePath, false},
		{"head public read", http.MethodHead, peoplePath, false},
		{"get auth route", http.MethodGet, "/api/auth/google/login", true},
		{"post known route", http.MethodPost, peoplePath, true},
		{"post unknown route", http.MethodPost, "/nope", true},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limiter := newRateLimiter(time.Now)
			router := newTestRouter(t, &fakePeople{person: person}, limiter)
			remote := "203.0.113." + strconv.Itoa(index+1) + ":1"
			send := func() *httptest.ResponseRecorder {
				req := sessionRequest(tc.method, tc.path)
				req.RemoteAddr = remote
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			for i := 0; i < rateLimitRequests; i++ {
				if rec := send(); rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d limited", i+1)
				}
			}
			rec := send()
			if !tc.wantLimited {
				if rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d limited", rateLimitRequests+1)
				}
				return
			}
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: status %d, want 429", rateLimitRequests+1, rec.Code)
			}
			seconds, err := strconv.Atoi(rec.Header().Get("Retry-After"))
			if err != nil || seconds < 1 {
				t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
			}
			var parsed struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				t.Fatal(err)
			}
			if parsed.Error.Code != "rate_limited" {
				t.Fatalf("code %q", parsed.Error.Code)
			}
		})
	}
}

func TestRateLimitPublicReadsDoNotConsumeWriteBudget(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	person := domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}
	router := newTestRouter(t, &fakePeople{person: person}, limiter)
	path := "/api/people/" + person.ID.String()
	for i := 0; i < rateLimitRequests; i++ {
		req := sessionRequest(http.MethodGet, path)
		req.RemoteAddr = "203.0.113.20:1"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("read %d: status %d", i+1, rec.Code)
		}
	}
	req := sessionRequest(http.MethodPost, path)
	req.RemoteAddr = "203.0.113.20:1"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("write limited after reads")
	}
}
