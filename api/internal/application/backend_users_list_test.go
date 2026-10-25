package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type usersListRepoStub struct {
	BackendRepository
	calls  int
	query  string
	page   domain.Page
	result domain.UserPage
	err    error
}

func (s *usersListRepoStub) ListUsers(_ context.Context, query string, page domain.Page) (domain.UserPage, error) {
	s.calls++
	s.query, s.page = query, page
	return s.result, s.err
}

func usersListSampleUsers(count int) []domain.User {
	email := "ada@example.com"
	users := make([]domain.User, 0, count)
	for index := 0; index < count; index++ {
		users = append(users, domain.User{
			ID:        uuid.New(),
			FirstName: "Ada",
			LastName:  "Lovelace",
			Email:     &email,
			Role:      "admin",
		})
	}
	return users
}

func TestEndpointUsersListApplicationPagination(t *testing.T) {
	users := usersListSampleUsers(3)
	cases := []struct {
		name     string
		rows     int
		wantLen  int
		wantMore bool
	}{
		{"extra row", 3, 2, true},
		{"exact limit", 2, 2, false},
		{"fewer rows", 1, 1, false},
		{"no rows", 0, 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stub := &usersListRepoStub{result: domain.UserPage{Users: users[:testCase.rows]}}
			result, err := NewBackend(stub).ListUsers(context.Background(), "  Ada  ", domain.Page{Limit: 2, Offset: 4})
			if err != nil {
				t.Fatalf("list users: %v", err)
			}
			if stub.calls != 1 {
				t.Fatalf("repository calls %d", stub.calls)
			}
			if stub.query != "Ada" {
				t.Fatalf("repository query %q", stub.query)
			}
			if stub.page != (domain.Page{Limit: 3, Offset: 4}) {
				t.Fatalf("repository page %+v", stub.page)
			}
			if len(result.Users) != testCase.wantLen || result.HasMore != testCase.wantMore {
				t.Fatalf("result %d rows has_more=%v", len(result.Users), result.HasMore)
			}
		})
	}
}

func TestEndpointUsersListApplicationEmptyQuery(t *testing.T) {
	stub := &usersListRepoStub{result: domain.UserPage{Users: usersListSampleUsers(1)}}
	if _, err := NewBackend(stub).ListUsers(context.Background(), "", domain.Page{Limit: 20}); err != nil {
		t.Fatalf("empty query: %v", err)
	}
	if stub.query != "" || stub.page.Limit != 21 {
		t.Fatalf("repository query %q page %+v", stub.query, stub.page)
	}
}

func TestEndpointUsersListApplicationRejectsMalformedQuery(t *testing.T) {
	queries := map[string]string{
		"invalid utf-8":     "\xffada",
		"nul byte":          "ada\x00lovelace",
		"control character": "ada\x01",
		"oversized bytes":   strings.Repeat("é", 101),
		"over the limit":    strings.Repeat("a", domain.MaxUserSearchLength+1),
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			stub := &usersListRepoStub{result: domain.UserPage{Users: []domain.User{}}}
			_, err := NewBackend(stub).ListUsers(context.Background(), query, domain.Page{Limit: 20})
			fields := validationFields(t, err)
			if _, ok := fields["q"]; !ok {
				t.Fatalf("validation fields %v", fields)
			}
			if stub.calls != 0 {
				t.Fatalf("malformed query reached the repository %d times", stub.calls)
			}
		})
	}
}

func TestEndpointUsersListApplicationAcceptsMaximumQuery(t *testing.T) {
	query := strings.Repeat("a", domain.MaxUserSearchLength)
	stub := &usersListRepoStub{result: domain.UserPage{Users: []domain.User{}}}
	if _, err := NewBackend(stub).ListUsers(context.Background(), query, domain.Page{Limit: 20}); err != nil {
		t.Fatalf("maximum query: %v", err)
	}
	if stub.query != query {
		t.Fatalf("repository query %d bytes", len(stub.query))
	}
}

func TestEndpointUsersListApplicationRejectsInvalidPage(t *testing.T) {
	pages := []domain.Page{
		{Limit: 0},
		{Limit: domain.MaxPageLimit + 1},
		{Limit: 20, Offset: -1},
	}
	for _, page := range pages {
		stub := &usersListRepoStub{result: domain.UserPage{Users: []domain.User{}}}
		if _, err := NewBackend(stub).ListUsers(context.Background(), "Ada", page); !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("page %+v error %v", page, err)
		}
		if stub.calls != 0 {
			t.Fatalf("invalid page reached the repository %d times", stub.calls)
		}
	}
}

func TestEndpointUsersListApplicationPropagatesRepositoryError(t *testing.T) {
	failure := errors.New("query failed")
	stub := &usersListRepoStub{err: failure}
	result, err := NewBackend(stub).ListUsers(context.Background(), "Ada", domain.Page{Limit: 20})
	if !errors.Is(err, failure) {
		t.Fatalf("error %v", err)
	}
	if len(result.Users) != 0 || result.HasMore {
		t.Fatalf("result %+v", result)
	}
}
