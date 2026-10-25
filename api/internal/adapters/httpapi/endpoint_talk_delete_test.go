package httpapi

import (
	"bytes"
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

	"caus-platform/api/internal/domain"
)

type talkDeleteHTTPService struct {
	BackendService
	err    error
	gotID  uuid.UUID
	gotCtx context.Context
	calls  int
}

func (s *talkDeleteHTTPService) DeleteTalk(ctx context.Context, id uuid.UUID) error {
	s.calls++
	s.gotID = id
	s.gotCtx = ctx
	return s.err
}

func talkDeleteHTTPAuth(t *testing.T, account domain.Account, authErr error) *Auth {
	t.Helper()
	return newStubBackendAuth(t, &stubBackendAuth{account: account, err: authErr}, "https://example.test")
}

func newTalkDeleteHTTPHandler(t *testing.T, service BackendService, auth *Auth, logs *bytes.Buffer) http.Handler {
	t.Helper()
	var logger *slog.Logger
	if logs == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	} else {
		logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	backend := NewBackend(service, auth, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/talks/{id}", backend.deleteTalk)
	return auth.requireAdmin(auth.requireSameOrigin(mux))
}

func talkDeleteHTTPServe(handler http.Handler, id string, cookies []*http.Cookie, origins []string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodDelete, "/api/admin/talks/"+id, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func talkDeleteHTTPSessionCookie() *http.Cookie {
	return &http.Cookie{Name: "__Host-caus_session", Value: "session-token"}
}

func TestEndpointTalkDeleteHTTPSuccess(t *testing.T) {
	auth := talkDeleteHTTPAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
	service := &talkDeleteHTTPService{}
	id := uuid.New()
	recorder := talkDeleteHTTPServe(newTalkDeleteHTTPHandler(t, service, auth, nil), id.String(),
		[]*http.Cookie{talkDeleteHTTPSessionCookie()}, []string{"https://example.test"})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("204 response has a body: %q", recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "" {
		t.Fatalf("204 response Content-Type %q", contentType)
	}
	if service.calls != 1 || service.gotID != id {
		t.Fatalf("service id=%v calls=%d", service.gotID, service.calls)
	}
	if service.gotCtx == nil {
		t.Fatal("service received no context")
	}
	deadline, ok := service.gotCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > lookupTimeout {
		t.Fatalf("service deadline %v (%v)", deadline, ok)
	}
}

func TestEndpointTalkDeleteHTTPGuards(t *testing.T) {
	id := uuid.NewString()
	admin := domain.Account{ID: uuid.New(), Role: "admin"}
	member := domain.Account{ID: uuid.New(), Role: "member"}
	allowedOrigin := []string{"https://example.test"}
	cases := []struct {
		name    string
		account domain.Account
		authErr error
		cookies []*http.Cookie
		origins []string
		status  int
		code    string
	}{
		{"missing session", admin, nil, nil, allowedOrigin, http.StatusUnauthorized, "unauthenticated"},
		{"expired session", admin, domain.ErrUnauthenticated,
			[]*http.Cookie{talkDeleteHTTPSessionCookie()}, allowedOrigin, http.StatusUnauthorized, "unauthenticated"},
		{"member", member, nil,
			[]*http.Cookie{talkDeleteHTTPSessionCookie()}, allowedOrigin, http.StatusForbidden, "forbidden"},
		{"missing origin", admin, nil, []*http.Cookie{talkDeleteHTTPSessionCookie()}, nil, http.StatusForbidden, "origin_denied"},
		{"wrong origin", admin, nil,
			[]*http.Cookie{talkDeleteHTTPSessionCookie()}, []string{"https://evil.test"}, http.StatusForbidden, "origin_denied"},
		{"duplicate origin", admin, nil,
			[]*http.Cookie{talkDeleteHTTPSessionCookie()}, []string{"https://example.test", "https://example.test"},
			http.StatusForbidden, "origin_denied"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &talkDeleteHTTPService{}
			auth := talkDeleteHTTPAuth(t, testCase.account, testCase.authErr)
			recorder := talkDeleteHTTPServe(newTalkDeleteHTTPHandler(t, service, auth, nil), id,
				testCase.cookies, testCase.origins)
			if recorder.Code != testCase.status {
				t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
			}
			if code := errorCode(t, recorder); code != testCase.code {
				t.Fatalf("code %q", code)
			}
			if service.calls != 0 {
				t.Fatalf("guarded request reached the service %d times", service.calls)
			}
		})
	}
}

func TestEndpointTalkDeleteHTTPMalformedID(t *testing.T) {
	for _, id := range []string{"not-a-uuid", uuid.Nil.String()} {
		service := &talkDeleteHTTPService{}
		auth := talkDeleteHTTPAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
		recorder := talkDeleteHTTPServe(newTalkDeleteHTTPHandler(t, service, auth, nil), id,
			[]*http.Cookie{talkDeleteHTTPSessionCookie()}, []string{"https://example.test"})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d (%s)", id, recorder.Code, recorder.Body.String())
		}
		if code := errorCode(t, recorder); code != "invalid_id" {
			t.Fatalf("%s: code %q", id, code)
		}
		if service.calls != 0 {
			t.Fatalf("%s: service called %d times", id, service.calls)
		}
	}
}

func TestEndpointTalkDeleteHTTPNotFound(t *testing.T) {
	id := uuid.New()
	for _, err := range []error{domain.ErrNotFound, fmt.Errorf("delete: %w", domain.ErrNotFound)} {
		service := &talkDeleteHTTPService{err: err}
		auth := talkDeleteHTTPAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
		recorder := talkDeleteHTTPServe(newTalkDeleteHTTPHandler(t, service, auth, nil), id.String(),
			[]*http.Cookie{talkDeleteHTTPSessionCookie()}, []string{"https://example.test"})
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
		}
		if code := errorCode(t, recorder); code != "not_found" {
			t.Fatalf("code %q", code)
		}
		if service.calls != 1 || service.gotID != id {
			t.Fatalf("service id=%v calls=%d", service.gotID, service.calls)
		}
	}
}

func TestEndpointTalkDeleteHTTPInternalError(t *testing.T) {
	var logs bytes.Buffer
	id := uuid.New()
	service := &talkDeleteHTTPService{err: errors.New("password authentication failed for user caus")}
	auth := talkDeleteHTTPAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
	recorder := talkDeleteHTTPServe(newTalkDeleteHTTPHandler(t, service, auth, &logs), id.String(),
		[]*http.Cookie{talkDeleteHTTPSessionCookie()}, []string{"https://example.test"})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if code := errorCode(t, recorder); code != "internal_error" {
		t.Fatalf("code %q", code)
	}
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("body leaked the database error: %s", recorder.Body.String())
	}
	if strings.Contains(logs.String(), "password") || strings.Contains(logs.String(), id.String()) {
		t.Fatalf("logs leaked backend detail: %s", logs.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("log %q: %v", logs.String(), err)
	}
	if entry["error_type"] != "*errors.errorString" || entry["operation"] != "DELETE /api/admin/talks/{id}" {
		t.Fatalf("log entry %v", entry)
	}
}
