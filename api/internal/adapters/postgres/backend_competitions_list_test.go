package postgres

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

type endpointCompetitionsListRow struct {
	id   uuid.UUID
	date time.Time
}

func insertEndpointCompetitionsListRow(t *testing.T, tx pgx.Tx, name, date string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'Description', $2::timestamp, 'Hall', 2) RETURNING id", name, date).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEndpointCompetitionsListPostgresStablePages(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	insertions := []struct {
		name string
		date string
	}{
		{"Alpha", "2026-01-01 10:00:00"},
		{"Bravo", "2026-05-02 18:00:00"},
		{"Charlie", "2026-05-02 18:00:00"},
		{"Delta", "2026-12-31 23:59:59"},
		{"Echo", "2025-03-03 08:00:00"},
	}
	rows := make([]endpointCompetitionsListRow, 0, len(insertions))
	for _, insertion := range insertions {
		rows = append(rows, endpointCompetitionsListRow{
			id:   insertEndpointCompetitionsListRow(t, tx, insertion.name, insertion.date),
			date: naiveTimestamp(endpointCompetitionsListParseTimestamp(t, insertion.date)),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].date.Equal(rows[j].date) {
			return rows[i].date.After(rows[j].date)
		}
		return bytes.Compare(rows[i].id[:], rows[j].id[:]) < 0
	})

	seen := make([]uuid.UUID, 0, len(rows))
	for offset := 0; offset <= len(rows); offset += 2 {
		page, err := repo.ListCompetitions(ctx, domain.Page{Limit: 2, Offset: offset})
		if err != nil {
			t.Fatalf("offset %d: %v", offset, err)
		}
		if page.HasMore {
			t.Fatalf("offset %d: repository must leave has_more false", offset)
		}
		end := offset + 2
		if end > len(rows) {
			end = len(rows)
		}
		want := rows[offset:end]
		if len(page.Competitions) != len(want) {
			t.Fatalf("offset %d: got %d competitions, want %d", offset, len(page.Competitions), len(want))
		}
		for i, competition := range page.Competitions {
			if competition.ID != want[i].id {
				t.Fatalf("offset %d position %d: id %s, want %s", offset, i, competition.ID, want[i].id)
			}
			if competition.Name == "" || competition.TeamSize != 2 || competition.Location != "Hall" {
				t.Fatalf("offset %d position %d: unexpected competition %+v", offset, i, competition)
			}
			if !competition.Date.Equal(want[i].date) || competition.Date.Location() != time.UTC {
				t.Fatalf("offset %d position %d: date %v, want %v", offset, i, competition.Date, want[i].date)
			}
			if competition.CreatedAt.IsZero() || competition.UpdatedAt.IsZero() {
				t.Fatalf("offset %d position %d: missing timestamps", offset, i)
			}
		}
		for _, competition := range page.Competitions {
			seen = append(seen, competition.ID)
		}
	}
	if len(seen) != len(rows) {
		t.Fatalf("paged %d competitions, want %d", len(seen), len(rows))
	}
	for i, id := range seen {
		if id != rows[i].id {
			t.Fatalf("position %d: id %s, want %s", i, id, rows[i].id)
		}
	}
	empty, err := repo.ListCompetitions(ctx, domain.Page{Limit: 2, Offset: len(rows) + 1})
	if err != nil {
		t.Fatalf("past-the-end page: %v", err)
	}
	if empty.Competitions == nil || len(empty.Competitions) != 0 {
		t.Fatalf("past-the-end page %+v, want empty slice", empty.Competitions)
	}
}

func TestEndpointCompetitionsListPostgresEmptyPage(t *testing.T) {
	repo, _ := backendFixture(t)
	page, err := repo.ListCompetitions(context.Background(), domain.Page{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if page.Competitions == nil || len(page.Competitions) != 0 {
		t.Fatalf("competitions %+v, want empty slice", page.Competitions)
	}
	if page.HasMore {
		t.Fatal("empty repository page reported has_more")
	}
}

func TestEndpointCompetitionsListPostgresAcceptsLookaheadLimit(t *testing.T) {
	repo, _ := backendFixture(t)
	page, err := repo.ListCompetitions(context.Background(), domain.Page{Limit: domain.MaxPageLimit + 1})
	if err != nil {
		t.Fatalf("lookahead limit %d rejected: %v", domain.MaxPageLimit+1, err)
	}
	if page.Competitions == nil || len(page.Competitions) != 0 || page.HasMore {
		t.Fatalf("empty lookahead page %+v", page)
	}
}

func TestEndpointCompetitionsListPostgresRejectsInvalidPaginationBeforeQuery(t *testing.T) {
	repo := NewBackendRepository(nil)
	for _, page := range []domain.Page{{Limit: 0}, {Limit: -1}, {Limit: 1, Offset: -1}} {
		if _, err := repo.ListCompetitions(context.Background(), page); !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("page %+v: error %v, want invalid pagination", page, err)
		}
	}
}

func TestEndpointCompetitionsListPostgresPropagatesContext(t *testing.T) {
	repo, _ := backendFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.ListCompetitions(ctx, domain.Page{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context canceled", err)
	}
}

func endpointCompetitionsListParseTimestamp(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.DateTime, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
