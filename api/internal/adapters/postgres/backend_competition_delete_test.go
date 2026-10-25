package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func endpointCompetitionDeleteCount(t *testing.T, tx pgx.Tx, query string, args ...any) int {
	t.Helper()
	var count int
	if err := tx.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

type EndpointCompetitionDeletePerson struct {
	id    uuid.UUID
	first string
	last  string
}

func TestEndpointCompetitionDeleteCascadeIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	competitionID := insertCompetition(t, tx, "Spring", 2)
	firstTeamID := insertTeam(t, tx, competitionID, "Team A")
	secondTeamID := insertTeam(t, tx, competitionID, "Team B")
	members := []EndpointCompetitionDeletePerson{
		{insertPersonRow(t, tx, "Grace", "Hopper"), "Grace", "Hopper"},
		{insertPersonRow(t, tx, "Alan", "Turing"), "Alan", "Turing"},
		{insertPersonRow(t, tx, "Katherine", "Johnson"), "Katherine", "Johnson"},
	}
	for _, member := range members[:2] {
		if _, err := tx.Exec(ctx, "INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			firstTeamID, competitionID, member.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, "INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		secondTeamID, competitionID, members[2].id); err != nil {
		t.Fatal(err)
	}
	talkID := insertTalkRow(t, tx, true)
	if _, err := tx.Exec(ctx, "INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)",
		talkID, members[0].id); err != nil {
		t.Fatal(err)
	}
	talkBefore, err := readTalk(ctx, tx, talkID, true)
	if err != nil {
		t.Fatalf("read talk before delete: %v", err)
	}
	otherPerson := EndpointCompetitionDeletePerson{insertPersonRow(t, tx, "Dorothy", "Vaughan"), "Dorothy", "Vaughan"}
	otherCompetitionID := insertCompetition(t, tx, "Autumn", 2)
	otherTeamID := insertTeam(t, tx, otherCompetitionID, "Team C")
	if _, err := tx.Exec(ctx, "INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		otherTeamID, otherCompetitionID, otherPerson.id); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteCompetition(ctx, competitionID); err != nil {
		t.Fatalf("delete competition: %v", err)
	}

	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM competitions WHERE id = $1", competitionID); count != 0 {
		t.Fatalf("competition rows remaining: %d", count)
	}
	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM teams WHERE competition_id = $1", competitionID); count != 0 {
		t.Fatalf("team rows remaining: %d", count)
	}
	if count := endpointCompetitionDeleteCount(t, tx,
		"SELECT count(*) FROM teams WHERE id IN ($1, $2)", firstTeamID, secondTeamID); count != 0 {
		t.Fatalf("cascaded team rows remaining: %d", count)
	}
	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM team_members WHERE competition_id = $1", competitionID); count != 0 {
		t.Fatalf("member rows remaining: %d", count)
	}
	for _, member := range members {
		var first, last string
		if err := tx.QueryRow(ctx, "SELECT first_name, last_name FROM people WHERE id = $1", member.id).
			Scan(&first, &last); err != nil {
			t.Fatalf("member %s was deleted with the competition: %v", member.id, err)
		}
		if first != member.first || last != member.last {
			t.Fatalf("member %s became %q %q", member.id, first, last)
		}
	}
	if _, err := readCompetition(ctx, tx, competitionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted competition still readable: %v", err)
	}
	talkAfter, err := readTalk(ctx, tx, talkID, true)
	if err != nil {
		t.Fatalf("talk deleted with its speaker's competition: %v", err)
	}
	if talkAfter.ID != talkID || talkAfter.Title != talkBefore.Title ||
		talkAfter.Description != talkBefore.Description || talkAfter.Location != talkBefore.Location ||
		talkAfter.Language != talkBefore.Language || !talkAfter.Date.Equal(talkBefore.Date) ||
		talkAfter.Duration != talkBefore.Duration || talkAfter.Published != talkBefore.Published {
		t.Fatalf("talk changed: before %+v after %+v", talkBefore, talkAfter)
	}
	if talkBefore.StreamURL == nil || talkAfter.StreamURL == nil || *talkAfter.StreamURL != *talkBefore.StreamURL ||
		talkBefore.PosterURL == nil || talkAfter.PosterURL == nil || *talkAfter.PosterURL != *talkBefore.PosterURL {
		t.Fatalf("talk media changed: before %+v after %+v", talkBefore, talkAfter)
	}
	if len(talkAfter.Speakers) != 1 || talkAfter.Speakers[0].ID != members[0].id ||
		talkAfter.Speakers[0].FirstName != members[0].first || talkAfter.Speakers[0].LastName != members[0].last {
		t.Fatalf("talk speaker association changed: %+v", talkAfter.Speakers)
	}
	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM talk_speakers WHERE talk_id = $1",
		talkID); count != 1 {
		t.Fatalf("talk speaker rows changed: %d", count)
	}

	otherCompetition, err := readCompetition(ctx, tx, otherCompetitionID)
	if err != nil {
		t.Fatalf("other competition removed: %v", err)
	}
	if otherCompetition.Name != "Autumn" {
		t.Fatalf("other competition %+v", otherCompetition)
	}
	otherTeam, err := readTeam(ctx, tx, otherTeamID)
	if err != nil {
		t.Fatalf("other team removed: %v", err)
	}
	if otherTeam.CompetitionID != otherCompetitionID || len(otherTeam.Members) != 1 ||
		otherTeam.Members[0].ID != otherPerson.id {
		t.Fatalf("other team changed: %+v", otherTeam)
	}
	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM team_members WHERE competition_id = $1",
		otherCompetitionID); count != 1 {
		t.Fatalf("other competition members %d", count)
	}
}

func TestEndpointCompetitionDeleteMissingIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	competitionID := insertCompetition(t, tx, "Spring", 2)
	teamID := insertTeam(t, tx, competitionID, "Team A")
	memberID := insertPersonRow(t, tx, "Grace", "Hopper")
	if _, err := tx.Exec(ctx, "INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		teamID, competitionID, memberID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteCompetition(ctx, competitionID); err != nil {
		t.Fatalf("delete competition: %v", err)
	}
	if err := repo.DeleteCompetition(ctx, competitionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("repeat delete: %v, want not found", err)
	}
	if err := repo.DeleteCompetition(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown delete: %v, want not found", err)
	}
	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM people WHERE id = $1", memberID); count != 1 {
		t.Fatalf("member person removed by a failed delete: %d", count)
	}
	if count := endpointCompetitionDeleteCount(t, tx, "SELECT count(*) FROM competitions"); count != 0 {
		t.Fatalf("competition rows remaining: %d", count)
	}
}
