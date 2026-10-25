package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func insertPersonRow(t *testing.T, tx pgx.Tx, first, last string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name) VALUES ($1, $2) RETURNING id", first, last).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertTalkRow(t *testing.T, tx pgx.Tx, published bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO talks (title, description, date, duration, location, language, stream_url, poster_url, published) "+
			"VALUES ($1, 'Description', '2026-03-14 09:30:00', interval '1 hour 30 minutes', 'Room 1', 'en', "+
			"'https://example.com/stream', 'https://example.com/poster', $2) RETURNING id",
		"Talk "+uuid.NewString(), published).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReadTalkIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	speakerID := insertPersonRow(t, tx, "Ada", "Lovelace")
	talkID := insertTalkRow(t, tx, true)
	if _, err := tx.Exec(ctx, "INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, speakerID); err != nil {
		t.Fatal(err)
	}
	talk, err := readTalk(ctx, tx, talkID, false)
	if err != nil {
		t.Fatal(err)
	}
	if talk.ID != talkID || talk.Title == "" || talk.Description != "Description" || talk.Location != "Room 1" ||
		talk.Language != "en" || talk.Duration != 5400 || !talk.Published || talk.StreamURL == nil || talk.PosterURL == nil {
		t.Fatalf("unexpected talk %+v", talk)
	}
	wantDate := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	if !talk.Date.Equal(wantDate) || talk.Date.Location() != time.UTC {
		t.Fatalf("date %v, want %v", talk.Date, wantDate)
	}
	if len(talk.Speakers) != 1 || talk.Speakers[0].ID != speakerID || talk.Speakers[0].FirstName != "Ada" {
		t.Fatalf("speakers %+v", talk.Speakers)
	}
	hiddenID := insertTalkRow(t, tx, false)
	if _, err := readTalk(ctx, tx, hiddenID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unpublished talk read as public: %v", err)
	}
	if _, err := readTalk(ctx, tx, hiddenID, true); err != nil {
		t.Fatalf("admin talk read: %v", err)
	}
	if _, err := readTalk(ctx, tx, uuid.New(), true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing talk: %v", err)
	}
}

func TestReadCompetitionAndTeamIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	var competitionID uuid.UUID
	err := tx.QueryRow(ctx,
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ('Spring', 'Description', '2026-05-02 18:00:00', 'Hall', 2) RETURNING id").Scan(&competitionID)
	if err != nil {
		t.Fatal(err)
	}
	competition, err := readCompetition(ctx, tx, competitionID)
	if err != nil {
		t.Fatal(err)
	}
	if competition.ID != competitionID || competition.Name != "Spring" || competition.Location != "Hall" || competition.TeamSize != 2 {
		t.Fatalf("unexpected competition %+v", competition)
	}
	if !competition.Date.Equal(time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("competition date %v", competition.Date)
	}
	var teamID uuid.UUID
	err = tx.QueryRow(ctx,
		"INSERT INTO teams (competition_id, name, position, photo_url) VALUES ($1, 'Team A', 3, 'https://example.com/photo') RETURNING id",
		competitionID).Scan(&teamID)
	if err != nil {
		t.Fatal(err)
	}
	members := []uuid.UUID{insertPersonRow(t, tx, "Grace", "Hopper"), insertPersonRow(t, tx, "Alan", "Turing")}
	for _, member := range members {
		if _, err := tx.Exec(ctx, "INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			teamID, competitionID, member); err != nil {
			t.Fatal(err)
		}
	}
	team, err := readTeam(ctx, tx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != teamID || team.CompetitionID != competitionID || team.Name == nil || *team.Name != "Team A" ||
		team.Position == nil || *team.Position != 3 || team.PhotoURL == nil || len(team.Members) != 2 {
		t.Fatalf("unexpected team %+v", team)
	}
	if team.Members[0].LastName != "Hopper" || team.Members[1].LastName != "Turing" {
		t.Fatalf("member ordering %+v", team.Members)
	}
	if _, err := readCompetition(ctx, tx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing competition: %v", err)
	}
	if _, err := readTeam(ctx, tx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing team: %v", err)
	}
}

func TestReadUserAndQueryIDsIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	withEmail := insertPersonRow(t, tx, "Ada", "Lovelace")
	if _, err := tx.Exec(ctx, "UPDATE people SET email = 'ada@example.com', role = 'admin' WHERE id = $1", withEmail); err != nil {
		t.Fatal(err)
	}
	withoutEmail := insertPersonRow(t, tx, "Alan", "Turing")
	user, err := readUser(ctx, tx, withEmail)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email == nil || *user.Email != "ada@example.com" || user.Role != "admin" {
		t.Fatalf("unexpected user %+v", user)
	}
	user, err = readUser(ctx, tx, withoutEmail)
	if err != nil || user.Email != nil {
		t.Fatalf("nullable email: %v %+v", err, user)
	}
	if _, err := readUser(ctx, tx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	if _, err := queryIDs(ctx, tx, "SELECT id FROM talks"); err != nil {
		t.Fatal(err)
	}
	ids, err := queryIDs(ctx, tx, "SELECT id FROM people WHERE id = ANY($1::uuid[])", []pgtype.UUID{pgUUID(withEmail)})
	if err != nil || len(ids) != 1 || ids[0] != withEmail {
		t.Fatalf("query ids: %v %v", ids, err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("rows left open after queryIDs: %v", err)
	}
}

func TestTranslateBackendError(t *testing.T) {
	for _, code := range []string{"23503", "23514"} {
		err := translateBackendError(&pgconn.PgError{Code: code})
		var validation domain.ValidationError
		if !errors.As(err, &validation) || len(validation.Fields) != 1 {
			t.Fatalf("%s translated to %v", code, err)
		}
	}
	if err := translateBackendError(&pgconn.PgError{Code: "23505"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unique violation translated to %v", err)
	}
	if err := translateBackendError(pgx.ErrNoRows); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no rows translated to %v", err)
	}
	original := errors.New("boom")
	if err := translateBackendError(original); err != original {
		t.Fatalf("unknown error translated to %v", err)
	}
	if err := translateBackendError(nil); err != nil {
		t.Fatalf("nil error translated to %v", err)
	}
}
