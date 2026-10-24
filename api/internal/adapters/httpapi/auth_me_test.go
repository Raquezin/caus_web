package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

type meServiceStub struct {
	AuthService
	currentUser func(context.Context, string) (domain.Account, error)
}

func (s meServiceStub) CurrentUser(ctx context.Context, token string) (domain.Account, error) {
	return s.currentUser(ctx, token)
}

func assertMeHeaders(t *testing.T, response *http.Response) {
	t.Helper()
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Content-Type") != "application/json" {
		t.Fatal("current user response must be non-cacheable JSON")
	}
	if len(response.Header.Values("Set-Cookie")) != 0 {
		t.Fatal("current user request mutated cookies")
	}
}

func assertMeError(t *testing.T, response *http.Response, status int, code, message string) {
	t.Helper()
	assertMeHeaders(t, response)
	gotCode, gotMessage := errorBody(t, response)
	if response.StatusCode != status || gotCode != code || gotMessage != message {
		t.Fatalf("status %d code %q message %q; wanted %d %q %q", response.StatusCode, gotCode, gotMessage, status, code, message)
	}
}

func assertMeAccount(t *testing.T, response *http.Response, account domain.Account) {
	t.Helper()
	defer response.Body.Close()
	assertMeHeaders(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("current user status %d", response.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"id": account.ID.String(), "first_name": account.FirstName, "last_name": account.LastName,
		"email": account.Email, "role": account.Role,
		"created_at": account.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": account.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if len(body) != len(want) {
		t.Fatalf("current user field count %d; wanted %d", len(body), len(want))
	}
	for field, value := range want {
		if body[field] != value {
			t.Fatalf("current user field %s differs from account", field)
		}
	}
}

func meRequest(t *testing.T, client *http.Client, method, target string, cookies ...string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		request.Header.Add("Cookie", cookie)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func assertMeHead(t *testing.T, response *http.Response, status int) {
	t.Helper()
	defer response.Body.Close()
	assertMeHeaders(t, response)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status || len(body) != 0 {
		t.Fatalf("HEAD status %d body length %d; wanted %d and no body", response.StatusCode, len(body), status)
	}
}

func TestMeCookieSelection(t *testing.T) {
	token := oauth2.GenerateVerifier()
	cases := []struct {
		name    string
		cookies []string
	}{
		{name: "missing"},
		{name: "unrelated", cookies: []string{"other=" + token}},
		{name: "empty", cookies: []string{"caus_session="}},
		{name: "quoted empty", cookies: []string{`caus_session=""`}},
		{name: "duplicate", cookies: []string{"caus_session=" + token + "; caus_session=" + token}},
		{name: "duplicate headers", cookies: []string{"caus_session=" + token, "caus_session=" + token}},
		{name: "empty then valid", cookies: []string{"caus_session=; caus_session=" + token}},
		{name: "valid then empty", cookies: []string{"caus_session=" + token + "; caus_session="}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := meServiceStub{currentUser: func(context.Context, string) (domain.Account, error) {
				t.Fatal("ambiguous or missing session reached application")
				return domain.Account{}, nil
			}}
			log := slog.New(slog.NewJSONHandler(io.Discard, nil))
			auth := NewAuth(nil, service, AuthConfig{}, log)
			request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			for _, cookie := range tc.cookies {
				request.Header.Add("Cookie", cookie)
			}
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, log, auth).ServeHTTP(recorder, request)
			assertMeError(t, recorder.Result(), http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		})
	}
}

func TestMeResponseUTC(t *testing.T) {
	account := domain.Account{
		ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Role: "member",
		CreatedAt: time.Date(2026, 10, 24, 12, 30, 15, 123456789, time.FixedZone("offset", 2*60*60)),
		UpdatedAt: time.Date(2026, 10, 24, 13, 30, 15, 0, time.FixedZone("offset", -5*60*60)),
	}
	token := oauth2.GenerateVerifier()
	calls := 0
	service := meServiceStub{currentUser: func(_ context.Context, got string) (domain.Account, error) {
		calls++
		if got != token {
			t.Fatal("raw session cookie was changed before application lookup")
		}
		return account, nil
	}}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(nil, service, AuthConfig{}, log)
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.Header.Set("Cookie", "other=one; caus_session="+token+"; other=two")
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, log, auth).ServeHTTP(recorder, request)
	assertMeAccount(t, recorder.Result(), account)
	if calls != 1 {
		t.Fatalf("current user application calls %d", calls)
	}
}

func TestMeLookupContext(t *testing.T) {
	for _, name := range []string{"bounded", "parent deadline", "parent canceled"} {
		t.Run(name, func(t *testing.T) {
			key := struct{}{}
			parent := context.WithValue(context.Background(), key, "request value")
			parent, cancel := context.WithCancel(parent)
			defer cancel()
			if name == "parent deadline" {
				var deadlineCancel context.CancelFunc
				parent, deadlineCancel = context.WithTimeout(parent, time.Second)
				defer deadlineCancel()
			}
			if name == "parent canceled" {
				cancel()
			}
			var lookup context.Context
			start := time.Now()
			service := meServiceStub{currentUser: func(ctx context.Context, _ string) (domain.Account, error) {
				lookup = ctx
				deadline, ok := ctx.Deadline()
				if !ok || deadline.After(time.Now().Add(lookupTimeout)) || ctx.Value(key) != "request value" {
					t.Fatal("lookup lost bounded deadline or parent values")
				}
				if parentDeadline, ok := parent.Deadline(); ok && deadline != parentDeadline {
					t.Fatal("lookup extended parent deadline")
				}
				if name == "bounded" && deadline.Before(start.Add(lookupTimeout-time.Second)) {
					t.Fatal("lookup deadline shorter than configured timeout")
				}
				if name == "parent canceled" && !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("lookup lost parent cancellation")
				}
				return domain.Account{}, domain.ErrUnauthenticated
			}}
			log := slog.New(slog.NewJSONHandler(io.Discard, nil))
			auth := NewAuth(nil, service, AuthConfig{}, log)
			request := httptest.NewRequest(http.MethodGet, "/api/me", nil).WithContext(parent)
			request.AddCookie(&http.Cookie{Name: "caus_session", Value: oauth2.GenerateVerifier()})
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, log, auth).ServeHTTP(recorder, request)
			assertMeError(t, recorder.Result(), http.StatusUnauthorized, "unauthenticated", "unauthenticated")
			if lookup == nil || !errors.Is(lookup.Err(), context.Canceled) {
				t.Fatal("lookup context not canceled after response")
			}
		})
	}
}

func TestMeSanitizedErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		text   string
	}{
		{name: "unauthenticated", err: fmt.Errorf("private-error: %w", domain.ErrUnauthenticated), status: 401, code: "unauthenticated", text: "unauthenticated"},
		{name: "backend", err: errors.New("private-error ada@example.com database-secret"), status: 500, code: "internal_error", text: "internal error"},
		{name: "deadline", err: fmt.Errorf("private-error: %w", context.DeadlineExceeded), status: 500, code: "internal_error", text: "internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := oauth2.GenerateVerifier()
			service := meServiceStub{currentUser: func(context.Context, string) (domain.Account, error) {
				return domain.Account{}, tc.err
			}}
			logs := &authLogSink{}
			log := slog.New(slog.NewJSONHandler(logs, nil))
			auth := NewAuth(nil, service, AuthConfig{}, log)
			request := httptest.NewRequest(http.MethodGet, "/api/me?private-query=ada@example.com", nil)
			request.AddCookie(&http.Cookie{Name: "caus_session", Value: token})
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, log, auth).ServeHTTP(recorder, request)
			assertMeError(t, recorder.Result(), tc.status, tc.code, tc.text)
			for _, secret := range []string{token, "private-error", "ada@example.com", "database-secret", "private-query"} {
				if strings.Contains(logs.String(), secret) || strings.Contains(recorder.Body.String(), secret) {
					t.Fatal("current user error leaked private data")
				}
			}
			decoder := json.NewDecoder(strings.NewReader(logs.String()))
			errorLogs := 0
			for decoder.More() {
				var entry map[string]any
				if err := decoder.Decode(&entry); err != nil {
					t.Fatal(err)
				}
				if entry["path"] != "/api/me" {
					t.Fatal("logging did not use route pattern")
				}
				if entry["level"] == "ERROR" {
					errorLogs++
					if len(entry) != 5 || entry["error_type"] != fmt.Sprintf("%T", tc.err) {
						t.Fatal("backend error logging must contain only error type and route pattern")
					}
				}
			}
			wantErrors := 0
			if tc.status == http.StatusInternalServerError {
				wantErrors = 1
			}
			if errorLogs != wantErrors {
				t.Fatalf("error log count %d; wanted %d", errorLogs, wantErrors)
			}
		})
	}
}

func TestMeUnregisteredWithoutService(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cases := [][]*Auth{nil, {nil}, {NewAuth(nil, nil, AuthConfig{}, log)}}
	for _, auth := range cases {
		recorder := httptest.NewRecorder()
		NewRouter(nil, nil, log, auth...).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/me", nil))
		assertMeError(t, recorder.Result(), http.StatusNotFound, "not_found", "resource not found")
	}
}
