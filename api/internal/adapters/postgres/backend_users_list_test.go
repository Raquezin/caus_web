package postgres

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertUsersListPerson(t *testing.T, tx pgx.Tx, first, last string, email *string, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name, email, role) VALUES ($1, $2, $3, $4) RETURNING id",
		first, last, email, role).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func usersListSearch(t *testing.T, repo *BackendRepository, query string, page domain.Page) []domain.User {
	t.Helper()
	result, err := repo.ListUsers(context.Background(), query, page)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	if result.HasMore {
		t.Fatalf("repository reported has_more for %q", query)
	}
	return result.Users
}

func usersListIDs(users []domain.User) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	return ids
}

func usersListLastNames(users []domain.User) []string {
	names := make([]string, 0, len(users))
	for _, user := range users {
		names = append(names, user.LastName)
	}
	return names
}

func TestEndpointUsersListPostgresSearch(t *testing.T) {
	repo, tx := backendFixture(t)
	adaEmail := "ada@example.com"
	graceEmail := "grace@example.com"
	zoeEmail := "zoe@example.com"
	ada := insertUsersListPerson(t, tx, "Ada", "Lovelace", &adaEmail, "admin")
	grace := insertUsersListPerson(t, tx, "Grace", "Hopper", &graceEmail, "member")
	alan := insertUsersListPerson(t, tx, "Alan", "Turing", nil, "member")
	zoe := insertUsersListPerson(t, tx, "Zoë", "Ñuñez", &zoeEmail, "member")
	percent := insertUsersListPerson(t, tx, "100%", "_done", nil, "member")

	all := usersListSearch(t, repo, "", domain.Page{Limit: 100})
	gotIDs := usersListIDs(all)
	wantIDs := []uuid.UUID{ada, grace, alan, zoe, percent}
	sort.Slice(wantIDs, func(i, j int) bool { return wantIDs[i].String() < wantIDs[j].String() })
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("listed %d users, want %d", len(gotIDs), len(wantIDs))
	}
	for index := range wantIDs {
		if gotIDs[index] != wantIDs[index] {
			t.Fatalf("user order %v, want %v", gotIDs, wantIDs)
		}
	}

	cases := []struct {
		query string
		want  []uuid.UUID
	}{
		{"ada", []uuid.UUID{ada}},
		{"ADA", []uuid.UUID{ada}},
		{"HOPPER", []uuid.UUID{grace}},
		{"ada love", []uuid.UUID{ada}},
		{"LoveLace", []uuid.UUID{ada}},
		{"@example.com", []uuid.UUID{ada, grace, zoe}},
		{"zoe@", []uuid.UUID{zoe}},
		{"turing", []uuid.UUID{alan}},
		{"%", []uuid.UUID{percent}},
		{"_", []uuid.UUID{percent}},
		{"' OR 1=1 --", nil},
		{"'; DROP TABLE people; --", nil},
		{"missing", nil},
	}
	for _, testCase := range cases {
		found := usersListSearch(t, repo, testCase.query, domain.Page{Limit: 100})
		got := usersListIDs(found)
		want := append([]uuid.UUID(nil), testCase.want...)
		sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })
		if len(got) != len(want) {
			t.Fatalf("search %q returned %d users, want %d", testCase.query, len(got), len(want))
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("search %q returned %v, want %v", testCase.query, got, want)
			}
		}
	}
}

func TestEndpointUsersListPostgresNullableEmailAndPaging(t *testing.T) {
	repo, tx := backendFixture(t)
	adaEmail := "ada@example.com"
	insertUsersListPerson(t, tx, "Ada", "Lovelace", &adaEmail, "admin")
	insertUsersListPerson(t, tx, "Grace", "Hopper", nil, "member")
	insertUsersListPerson(t, tx, "Alan", "Turing", nil, "member")

	all := usersListSearch(t, repo, "", domain.Page{Limit: 100})
	sorted := usersListIDs(all)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].String() < sorted[j].String() })
	if len(sorted) != 3 {
		t.Fatalf("listed %d users", len(sorted))
	}

	for _, query := range []string{"hopper", "turing"} {
		found := usersListSearch(t, repo, query, domain.Page{Limit: 100})
		if len(found) != 1 {
			t.Fatalf("search %q returned %d users", query, len(found))
		}
		if found[0].Email != nil {
			t.Fatalf("search %q returned email %v, want null", query, *found[0].Email)
		}
		if found[0].Role != "member" {
			t.Fatalf("search %q returned role %q", query, found[0].Role)
		}
	}

	firstPage := usersListSearch(t, repo, "", domain.Page{Limit: 2, Offset: 0})
	if len(firstPage) != 2 || usersListIDs(firstPage)[0] != sorted[0] || usersListIDs(firstPage)[1] != sorted[1] {
		t.Fatalf("first page %v, want %v", usersListIDs(firstPage), sorted[:2])
	}
	secondPage := usersListSearch(t, repo, "", domain.Page{Limit: 2, Offset: 2})
	if len(secondPage) != 1 || usersListIDs(secondPage)[0] != sorted[2] {
		t.Fatalf("second page %v, want %v", usersListIDs(secondPage), sorted[2:])
	}
	beyond := usersListSearch(t, repo, "", domain.Page{Limit: 2, Offset: 10})
	if len(beyond) != 0 {
		t.Fatalf("offset beyond rows returned %d users", len(beyond))
	}
	if names := usersListLastNames(usersListSearch(t, repo, "", domain.Page{Limit: 0})); len(names) != 0 {
		t.Fatalf("zero limit returned %v", names)
	}
}
