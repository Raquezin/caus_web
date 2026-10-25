package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestLookupContextHasDeadline(t *testing.T) {
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()
	resp := doAdmin(t, srv, http.MethodGet, "/api/people/"+people.person.ID.String())
	resp.Body.Close()
	deadline, ok := people.gotCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > lookupTimeout {
		t.Fatalf("expected bounded context, deadline %v", deadline)
	}
}

func TestContextPropagatedWithCancellation(t *testing.T) {
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Role: "admin"},
	}, "https://people.test")
	router := NewRouter(people, func(context.Context) error { return nil }, log, auth)

	type ctxKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "marker"))
	req := sessionRequest(http.MethodGet, "/api/people/"+people.person.ID.String()).WithContext(ctx)
	rec := httptest.NewRecorder()
	cancel()
	router.ServeHTTP(rec, req)

	if !people.called {
		t.Fatal("service not called")
	}
	if people.gotCtx.Value(ctxKey{}) != "marker" {
		t.Fatal("context value not propagated")
	}
	if !errors.Is(people.gotCtx.Err(), context.Canceled) {
		t.Fatalf("derived context err %v, want canceled", people.gotCtx.Err())
	}
	if _, ok := people.gotCtx.Deadline(); !ok {
		t.Fatal("derived context missing deadline")
	}
}

func TestListContextPropagatedWithCancellation(t *testing.T) {
	people := &fakePeople{page: domain.PeoplePage{People: []domain.Person{}}}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Role: "admin"},
	}, "https://people.test")
	router := NewRouter(people, func(context.Context) error { return nil }, log, auth)

	type ctxKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "marker"))
	req := sessionRequest(http.MethodGet, "/api/people").WithContext(ctx)
	rec := httptest.NewRecorder()
	cancel()
	router.ServeHTTP(rec, req)

	if !people.called {
		t.Fatal("service not called")
	}
	if people.gotCtx.Value(ctxKey{}) != "marker" {
		t.Fatal("context value not propagated")
	}
	if !errors.Is(people.gotCtx.Err(), context.Canceled) {
		t.Fatalf("derived context err %v, want canceled", people.gotCtx.Err())
	}
	if _, ok := people.gotCtx.Deadline(); !ok {
		t.Fatal("derived context missing deadline")
	}
}
