package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEndpointCompetitionCreateRejectsMalformedBodies(t *testing.T) {
	oversize := `{"name":"` + strings.Repeat("a", maxJSONBody) + `","description":"D",` +
		`"date":"2026-05-02T18:30:00","location":"H"}`
	cases := map[string]struct {
		contentType string
		body        string
		status      int
		code        string
	}{
		"malformed":          {"application/json", `{"name":`, http.StatusBadRequest, "invalid_body"},
		"unknown field":      {"application/json", `{"name":"N","published":true}`, http.StatusBadRequest, "invalid_body"},
		"null":               {"application/json", `null`, http.StatusBadRequest, "invalid_body"},
		"empty":              {"application/json", ``, http.StatusBadRequest, "invalid_body"},
		"oversize":           {"application/json", oversize, http.StatusRequestEntityTooLarge, "request_too_large"},
		"wrong content type": {"text/plain", endpointCompetitionCreateBody, http.StatusUnsupportedMediaType, "unsupported_media_type"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			service := &stubEndpointCompetitionCreateService{competition: endpointCompetitionCreateFixture()}
			handler := endpointCompetitionCreateHandler(endpointCompetitionCreateAuth(t, "admin"), service,
				endpointCompetitionCreateLogger())
			request := endpointCompetitionCreateRequest(testCase.body)
			request.Header.Set("Content-Type", testCase.contentType)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertBackendError(t, recorder, testCase.status, testCase.code)
			if service.calls != 0 {
				t.Fatalf("invalid body reached the service %d times", service.calls)
			}
		})
	}
}

func TestEndpointCompetitionCreateRequiresAdminAndOrigin(t *testing.T) {
	adminAuth := endpointCompetitionCreateAuth(t, "admin")
	memberAuth := endpointCompetitionCreateAuth(t, "member")
	cases := []struct {
		name   string
		auth   *Auth
		mutate func(*http.Request)
		status int
		code   string
	}{
		{"missing session", adminAuth, func(r *http.Request) { r.Header.Del("Cookie") },
			http.StatusUnauthorized, "unauthenticated"},
		{"member", memberAuth, func(*http.Request) {}, http.StatusForbidden, "forbidden"},
		{"missing origin", adminAuth, func(r *http.Request) { r.Header.Del("Origin") },
			http.StatusForbidden, "origin_denied"},
		{"wrong origin", adminAuth, func(r *http.Request) { r.Header.Set("Origin", "https://evil.test") },
			http.StatusForbidden, "origin_denied"},
		{"duplicate origin", adminAuth, func(r *http.Request) { r.Header.Add("Origin", "https://example.test") },
			http.StatusForbidden, "origin_denied"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &stubEndpointCompetitionCreateService{competition: endpointCompetitionCreateFixture()}
			handler := endpointCompetitionCreateHandler(testCase.auth, service, endpointCompetitionCreateLogger())
			request := endpointCompetitionCreateRequest(endpointCompetitionCreateBody)
			testCase.mutate(request)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertBackendError(t, recorder, testCase.status, testCase.code)
			if service.calls != 0 {
				t.Fatalf("denied request reached the service %d times", service.calls)
			}
		})
	}
}
