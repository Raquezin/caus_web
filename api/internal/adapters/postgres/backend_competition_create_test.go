package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

var errCompetitionCreateRead = errors.New("injected competition response read failure")

type competitionCreateFailingReadDB struct {
	BackendDB
}

func (d competitionCreateFailingReadDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.BackendDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return competitionCreateFailingReadTx{Tx: tx}, nil
}

func (d competitionCreateFailingReadDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT") {
		return competitionCreateFailingRow{}
	}
	return d.BackendDB.QueryRow(ctx, sql, args...)
}

type competitionCreateFailingReadTx struct {
	pgx.Tx
}

func (t competitionCreateFailingReadTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT") {
		return competitionCreateFailingRow{}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

type competitionCreateFailingRow struct{}

func (competitionCreateFailingRow) Scan(...any) error {
	return errCompetitionCreateRead
}

func TestEndpointCompetitionCreateRepositoryIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	input := domain.CompetitionInput{
		Name:        "Spring CTF",
		Description: "Capture the flag",
		Location:    "Madrid",
		Date:        time.Date(2026, 5, 2, 18, 30, 0, 0, time.UTC),
		TeamSize:    4,
	}
	created, err := repo.CreateCompetition(ctx, input)
	if err != nil {
		t.Fatalf("create competition: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("create competition returned no id")
	}
	if created.Name != input.Name || created.Description != input.Description ||
		created.Location != input.Location || created.TeamSize != input.TeamSize {
		t.Fatalf("created competition %+v, want %+v", created, input)
	}
	if !created.Date.Equal(input.Date) || created.Date.Location() != time.UTC {
		t.Fatalf("date %v, want local timestamp %v", created.Date, input.Date)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("audit timestamps missing: %+v", created)
	}

	var name, description, location string
	var date time.Time
	var teamSize int
	if err := tx.QueryRow(ctx,
		"SELECT name, description, date, location, team_size FROM competitions WHERE id = $1", created.ID).
		Scan(&name, &description, &date, &location, &teamSize); err != nil {
		t.Fatalf("read stored competition: %v", err)
	}
	if name != input.Name || description != input.Description || location != input.Location ||
		teamSize != input.TeamSize || !naiveTimestamp(date).Equal(input.Date) {
		t.Fatalf("stored row %q %q %v %q %d", name, description, date, location, teamSize)
	}

	maximum := input
	maximum.Name = "Autumn CTF"
	maximum.TeamSize = 32767
	createdMaximum, err := repo.CreateCompetition(ctx, maximum)
	if err != nil {
		t.Fatalf("create maximum team size: %v", err)
	}
	if createdMaximum.TeamSize != 32767 {
		t.Fatalf("maximum team size %d", createdMaximum.TeamSize)
	}

	if _, err := repo.CreateCompetition(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate competition name and date: %v", err)
	}

	invalid := input
	invalid.Name = "   "
	if _, err := repo.CreateCompetition(ctx, invalid); err == nil {
		t.Fatal("blank competition name accepted")
	} else {
		var validation domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("blank competition name error %v", err)
		}
	}

	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM competitions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("competitions row count %d, want 2 after rejected writes", count)
	}
}

func TestEndpointCompetitionCreateReadFailureRollsBack(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	failing := NewBackendRepository(competitionCreateFailingReadDB{BackendDB: repo.db})
	input := domain.CompetitionInput{
		Name:        "Rollback CTF",
		Description: "Response read fails",
		Location:    "Madrid",
		Date:        time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
		TeamSize:    3,
	}
	created, err := failing.CreateCompetition(ctx, input)
	if !errors.Is(err, errCompetitionCreateRead) {
		t.Fatalf("create competition with failing response read: %v", err)
	}
	if created.ID != uuid.Nil {
		t.Fatalf("failed create returned competition %+v", created)
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM competitions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("competitions row count %d, want 0 after response read failure", count)
	}
}
