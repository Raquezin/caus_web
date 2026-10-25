package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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

type stubEndpointExportService struct {
	BackendService
	export   domain.AccountExport
	err      error
	calls    int
	gotID    uuid.UUID
	deadline bool
}

func (s *stubEndpointExportService) ExportAccount(ctx context.Context, id uuid.UUID) (domain.AccountExport, error) {
	s.calls++
	s.gotID = id
	_, s.deadline = ctx.Deadline()
	return s.export, s.err
}

func endpointExportAccount() domain.Account {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return domain.Account{
		ID:        uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21"),
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Role:      "member",
		CreatedAt: created,
		UpdatedAt: created.Add(48 * time.Hour),
	}
}

func endpointExportData() domain.AccountExport {
	account := endpointExportAccount()
	speaker := domain.Person{ID: uuid.MustParse("8c1d2e3f-4a5b-6c7d-8e9f-0a1b2c3d4e5f"), FirstName: "Grace", LastName: "Hopper"}
	talk := domain.Talk{
		ID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Title:       "Draft Talk",
		Description: "Description",
		Date:        time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		Duration:    3600,
		Location:    "Room",
		Language:    "en",
		Published:   false,
		Speakers:    []domain.Person{{ID: account.ID, FirstName: account.FirstName, LastName: account.LastName}, speaker},
		CreatedAt:   account.CreatedAt,
		UpdatedAt:   account.UpdatedAt,
	}
	team := domain.Team{
		ID:            uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		CompetitionID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Members:       []domain.Person{{ID: account.ID, FirstName: account.FirstName, LastName: account.LastName}, speaker},
		CreatedAt:     account.CreatedAt,
		UpdatedAt:     account.UpdatedAt,
	}
	return domain.AccountExport{
		Account:   account,
		GoogleSub: "subject-ada",
		Sessions: []domain.SessionMetadata{{
			CreatedAt: account.CreatedAt,
			ExpiresAt: account.CreatedAt.Add(24 * time.Hour),
		}},
		Talks:        []domain.Talk{talk},
		Teams:        []domain.Team{team},
		Competitions: []domain.Competition{{ID: team.CompetitionID, Name: "Spring"}},
	}
}

func newEndpointExportServer(t *testing.T, auth *Auth, backend *Backend) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(auth.requireUser(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.exportAccount),
	})))
	t.Cleanup(server.Close)
	return server
}

func endpointExportDo(t *testing.T, server *httptest.Server, target string, cookies []*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

func endpointExportCookie() *http.Cookie {
	return &http.Cookie{Name: "__Host-caus_session", Value: "session-token"}
}

func TestEndpointExportSelfOnlyAttachment(t *testing.T) {
	account := endpointExportAccount()
	service := &stubEndpointExportService{export: endpointExportData()}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	server := newEndpointExportServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	otherID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	target := "/api/me/export?id=" + otherID.String() + "&email=grace@example.com"
	response, body := endpointExportDo(t, server, target, []*http.Cookie{endpointExportCookie()})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	if disposition := response.Header.Get("Content-Disposition"); disposition != `attachment; filename="caus-account.json"` {
		t.Fatalf("Content-Disposition %q", disposition)
	}
	if service.calls != 1 || service.gotID != account.ID || !service.deadline {
		t.Fatalf("service calls %d id %v deadline %v", service.calls, service.gotID, service.deadline)
	}
	var payload struct {
		Account struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"account"`
		GoogleSub string `json:"google_sub"`
		Sessions  []struct {
			CreatedAt time.Time `json:"created_at"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"sessions"`
		Talks []struct {
			ID        string `json:"id"`
			Published bool   `json:"published"`
			Speakers  []struct {
				FirstName string `json:"first_name"`
			} `json:"speakers"`
		} `json:"talks"`
		Teams []struct {
			ID      string `json:"id"`
			Members []struct {
				FirstName string `json:"first_name"`
			} `json:"members"`
		} `json:"teams"`
		Competitions []struct {
			ID string `json:"id"`
		} `json:"competitions"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode export %q: %v", body, err)
	}
	if payload.Account.ID != account.ID.String() || payload.Account.Email != "ada@example.com" ||
		payload.GoogleSub != "subject-ada" {
		t.Fatalf("account payload %+v", payload.Account)
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].ExpiresAt.IsZero() ||
		!payload.Sessions[0].ExpiresAt.After(payload.Sessions[0].CreatedAt) {
		t.Fatalf("sessions %+v", payload.Sessions)
	}
	if len(payload.Talks) != 1 || payload.Talks[0].Published || len(payload.Talks[0].Speakers) != 2 ||
		payload.Talks[0].Speakers[1].FirstName != "Grace" {
		t.Fatalf("talks %+v", payload.Talks)
	}
	if len(payload.Teams) != 1 || len(payload.Teams[0].Members) != 2 || payload.Teams[0].Members[1].FirstName != "Grace" {
		t.Fatalf("teams %+v", payload.Teams)
	}
	if len(payload.Competitions) != 1 {
		t.Fatalf("competitions %+v", payload.Competitions)
	}
	hash := sha256.Sum256([]byte("session-token"))
	for _, secret := range []string{
		"session-token", "grace@example.com", `"token"`, `"hash"`,
		hex.EncodeToString(hash[:]), base64.StdEncoding.EncodeToString(hash[:]),
	} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatalf("export leaked %q: %s", secret, body)
		}
	}
}

func TestEndpointExportEmptyCollections(t *testing.T) {
	account := endpointExportAccount()
	service := &stubEndpointExportService{export: domain.AccountExport{Account: account, GoogleSub: "subject-ada"}}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	server := newEndpointExportServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	response, body := endpointExportDo(t, server, "/api/me/export", []*http.Cookie{endpointExportCookie()})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	for _, field := range []string{`"sessions":[]`, `"talks":[]`, `"teams":[]`, `"competitions":[]`} {
		if !bytes.Contains(body, []byte(field)) {
			t.Fatalf("export missing %s: %s", field, body)
		}
	}
}

func TestEndpointExportNotFound(t *testing.T) {
	account := endpointExportAccount()
	service := &stubEndpointExportService{err: domain.ErrNotFound}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	server := newEndpointExportServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	response, body := endpointExportDo(t, server, "/api/me/export", []*http.Cookie{endpointExportCookie()})
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"not_found"`)) || bytes.Contains(body, []byte("ada@example.com")) {
		t.Fatalf("not found body %s", body)
	}
}

func TestEndpointExportBackendFailureSanitized(t *testing.T) {
	account := endpointExportAccount()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &stubEndpointExportService{err: errors.New("column sessions.token_hash leaked by query")}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	server := newEndpointExportServer(t, auth, NewBackend(service, auth, logger))
	response, body := endpointExportDo(t, server, "/api/me/export", []*http.Cookie{endpointExportCookie()})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"internal_error"`)) || bytes.Contains(body, []byte("token_hash")) {
		t.Fatalf("failure body %s", body)
	}
	if bytes.Contains(logs.Bytes(), []byte("token_hash")) || !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("failure logs %s", logs.String())
	}
}

func TestEndpointExportRequiresUser(t *testing.T) {
	backend := NewBackend(nil, nil, nil)
	recorder := httptest.NewRecorder()
	backend.exportAccount(recorder, httptest.NewRequest(http.MethodGet, "/api/me/export", nil))
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")

	account := endpointExportAccount()
	service := &stubEndpointExportService{export: endpointExportData()}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	server := newEndpointExportServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	response, body := endpointExportDo(t, server, "/api/me/export", nil)
	if response.StatusCode != http.StatusUnauthorized || service.calls != 0 {
		t.Fatalf("missing cookie status %d calls %d (%s)", response.StatusCode, service.calls, body)
	}

	expiredAuth := newStubBackendAuth(t, &stubBackendAuth{err: domain.ErrUnauthenticated}, "https://example.test")
	expiredServer := newEndpointExportServer(t, expiredAuth, NewBackend(service, expiredAuth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	response, body = endpointExportDo(t, expiredServer, "/api/me/export", []*http.Cookie{endpointExportCookie()})
	if response.StatusCode != http.StatusUnauthorized || service.calls != 0 {
		t.Fatalf("expired session status %d calls %d (%s)", response.StatusCode, service.calls, body)
	}

	cookies := []*http.Cookie{endpointExportCookie(), {Name: "__Host-caus_session", Value: "other-token"}}
	response, body = endpointExportDo(t, server, "/api/me/export", cookies)
	if response.StatusCode != http.StatusUnauthorized || service.calls != 0 {
		t.Fatalf("duplicate cookies status %d calls %d (%s)", response.StatusCode, service.calls, body)
	}
	if strings.Contains(string(body), "ada@example.com") {
		t.Fatalf("unauthenticated body leaked account data: %s", body)
	}
}
