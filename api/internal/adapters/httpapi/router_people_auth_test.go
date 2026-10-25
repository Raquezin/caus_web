package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestPeopleEndpointsRequireSession(t *testing.T) {
	id := uuid.New()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/api/people", "/api/people/" + id.String()} {
			people := &fakePeople{person: domain.Person{ID: id}}
			srv, _ := newAdminTestServer(t, people, nil)
			resp := do(t, srv, method, path)
			resp.Body.Close()
			srv.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s %s: status %d", method, path, resp.StatusCode)
			}
			if people.called {
				t.Fatalf("%s %s: service called", method, path)
			}
		}
	}
}

func TestPeopleEndpointsMissingAuthRejects(t *testing.T) {
	id := uuid.New()
	for _, path := range []string{"/api/people", "/api/people/" + id.String()} {
		people := &fakePeople{person: domain.Person{ID: id}}
		srv, _ := newTestServer(people, nil)
		resp := do(t, srv, http.MethodGet, path)
		resp.Body.Close()
		srv.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		if people.called {
			t.Fatalf("%s: service called", path)
		}
	}
}

func TestPeopleEndpointsForbidMembers(t *testing.T) {
	id := uuid.New()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/api/people", "/api/people/" + id.String()} {
			people := &fakePeople{person: domain.Person{ID: id}}
			srv, _ := newRoleTestServer(t, people, nil, "member")
			resp := doAdmin(t, srv, method, path)
			resp.Body.Close()
			srv.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s %s: status %d", method, path, resp.StatusCode)
			}
			if people.called {
				t.Fatalf("%s %s: service called", method, path)
			}
		}
	}
}
