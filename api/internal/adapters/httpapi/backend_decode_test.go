package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type namedBody struct {
	Name string `json:"name"`
}

func decodeInto(t *testing.T, contentType, body string, target any) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/talks", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	return decodeBody(recorder, request, target), recorder
}

func TestDecodeBodyAcceptsJSON(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8"} {
		var body namedBody
		ok, recorder := decodeInto(t, contentType, `{"name":"Ada"}`, &body)
		if !ok || recorder.Code != http.StatusOK && recorder.Code != http.StatusNoContent {
			t.Fatalf("%s rejected: %d", contentType, recorder.Code)
		}
		if body.Name != "Ada" {
			t.Fatalf("body %+v", body)
		}
	}
}

func TestDecodeBodyRejectsContentType(t *testing.T) {
	for _, contentType := range []string{"", "text/plain", "application/xml", "application/jsonx"} {
		_, recorder := decodeInto(t, contentType, `{"name":"Ada"}`, &namedBody{})
		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("content type %q: status %d, want 415", contentType, recorder.Code)
		}
	}
}

func TestDecodeBodyRejectsMalformedPayloads(t *testing.T) {
	cases := map[string]string{
		"empty":          ``,
		"blank":          "   \n\t ",
		"null":           `null`,
		"unknown field":  `{"name":"Ada","role":"admin"}`,
		"truncated":      `{"name":`,
		"trailing value": `{"name":"Ada"}{"name":"Grace"}`,
		"array":          `[{"name":"Ada"}]`,
		"string":         `"Ada"`,
	}
	for name, body := range cases {
		_, recorder := decodeInto(t, "application/json", body, &namedBody{})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", name, recorder.Code)
		}
		var response struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Error.Code != "invalid_body" {
			t.Fatalf("%s: response %s", name, recorder.Body.String())
		}
	}
}

func TestDecodeBodyRejectsOversizedPayload(t *testing.T) {
	body := `{"name":"` + strings.Repeat("a", maxJSONBody) + `"}`
	_, recorder := decodeInto(t, "application/json", body, &namedBody{})
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", recorder.Code)
	}
}

func TestRequestUUID(t *testing.T) {
	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/api/talks/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()
	got, ok := requestUUID(recorder, request)
	if !ok || got != id {
		t.Fatalf("request uuid %v %v", got, ok)
	}
	for _, value := range []string{"", "not-a-uuid", uuid.Nil.String(), id.String() + "x"} {
		request := httptest.NewRequest(http.MethodGet, "/api/talks/x", nil)
		request.SetPathValue("id", value)
		recorder := httptest.NewRecorder()
		if _, ok := requestUUID(recorder, request); ok {
			t.Fatalf("value %q accepted", value)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("value %q: status %d", value, recorder.Code)
		}
	}
}
