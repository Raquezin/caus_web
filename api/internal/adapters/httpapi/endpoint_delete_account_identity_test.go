package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEndpointDeleteAccountIgnoresRequestAccountID(t *testing.T) {
	account := endpointDeleteAccountAccount()
	foreign := uuid.New()
	if foreign == account.ID {
		t.Fatal("foreign id must differ from the session account id")
	}
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: account}, deleteService, io.Discard)
	body, err := json.Marshal(map[string]string{
		"id":         foreign.String(),
		"person_id":  foreign.String(),
		"account_id": foreign.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodDelete,
		endpointDeleteAccountPath+"?id="+foreign.String()+"&account_id="+foreign.String(),
		strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://example.test")
	request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	request.SetPathValue("id", foreign.String())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("delete returned a body: %s", recorder.Body.String())
	}
	ids := deleteService.endpointDeleteAccountIDs()
	if len(ids) != 1 {
		t.Fatalf("DeleteAccount ids %v, want exactly one", ids)
	}
	if ids[0] != account.ID {
		t.Fatalf("DeleteAccount id %v, want session subject %v", ids[0], account.ID)
	}
	for _, id := range ids {
		if id == foreign {
			t.Fatalf("DeleteAccount used request supplied id %v", foreign)
		}
	}
}
