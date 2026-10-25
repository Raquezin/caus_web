package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointCompetitionGetIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	insertCompetitionRow := func(name string) uuid.UUID {
		var competitionID uuid.UUID
		if err := tx.QueryRow(ctx,
			"INSERT INTO competitions (name, description, date, location, team_size) "+
				"VALUES ($1, 'Description', '2026-05-02 18:00:00', 'Hall', 3) RETURNING id",
			name).Scan(&competitionID); err != nil {
			t.Fatal(err)
		}
		return competitionID
	}
	competitionID := insertCompetitionRow("Spring")
	ordinal := 0
	insertTeamRow := func(name *string, position *int, photoURL *string) (uuid.UUID, uuid.UUID) {
		ordinal++
		var teamID, memberID uuid.UUID
		if err := tx.QueryRow(ctx,
			"INSERT INTO teams (competition_id, name, position, photo_url) VALUES ($1, $2, $3, $4) RETURNING id",
			competitionID, name, position, photoURL).Scan(&teamID); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx,
			"INSERT INTO people (first_name, last_name, email, google_sub) VALUES ($1, 'Person', $2, $3) RETURNING id",
			fmt.Sprintf("Member%d", ordinal), fmt.Sprintf("member%d@secret.test", ordinal),
			fmt.Sprintf("sub-secret-%d", ordinal)).Scan(&memberID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			teamID, competitionID, memberID); err != nil {
			t.Fatal(err)
		}
		return teamID, memberID
	}

	alphaName, betaName, gammaName := "Alpha", "Beta", "Gamma"
	positionOne, positionTwo := 1, 2
	betaPhoto := "https://photos.test/beta"
	alphaID, alphaMember := insertTeamRow(&alphaName, nil, nil)
	betaID, betaMember := insertTeamRow(&betaName, &positionTwo, &betaPhoto)
	gammaID, gammaMember := insertTeamRow(&gammaName, &positionOne, nil)
	unnamedID, unnamedMember := insertTeamRow(nil, &positionTwo, nil)

	detail, err := repo.GetCompetition(ctx, competitionID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Competition.ID != competitionID || detail.Competition.Name != "Spring" ||
		detail.Competition.Location != "Hall" || detail.Competition.TeamSize != 3 {
		t.Fatalf("competition %+v", detail.Competition)
	}
	positionTwoOrder := []uuid.UUID{betaID, unnamedID}
	sort.Slice(positionTwoOrder, func(i, j int) bool {
		return positionTwoOrder[i].String() < positionTwoOrder[j].String()
	})
	wantOrder := []uuid.UUID{gammaID, positionTwoOrder[0], positionTwoOrder[1], alphaID}
	if len(detail.Teams) != len(wantOrder) {
		t.Fatalf("teams %+v", detail.Teams)
	}
	for index, want := range wantOrder {
		if detail.Teams[index].ID != want {
			t.Fatalf("team %d is %v, want %v (order %+v)", index, detail.Teams[index].ID, want, detail.Teams)
		}
	}
	teams := map[uuid.UUID]domain.Team{}
	for _, team := range detail.Teams {
		teams[team.ID] = team
	}
	if team := teams[gammaID]; team.Name == nil || *team.Name != gammaName || team.Position == nil || *team.Position != 1 {
		t.Fatalf("gamma %+v", team)
	}
	if team := teams[alphaID]; team.Name == nil || *team.Name != alphaName || team.Position != nil {
		t.Fatalf("alpha %+v", team)
	}
	if team := teams[unnamedID]; team.Name != nil || team.Position == nil || *team.Position != 2 {
		t.Fatalf("unnamed %+v", team)
	}
	if team := teams[betaID]; team.PhotoURL == nil || *team.PhotoURL != betaPhoto {
		t.Fatalf("beta %+v", team)
	}
	for teamID, memberID := range map[uuid.UUID]uuid.UUID{
		alphaID: alphaMember, betaID: betaMember, gammaID: gammaMember, unnamedID: unnamedMember,
	} {
		team := teams[teamID]
		if len(team.Members) != 1 || team.Members[0].ID != memberID || team.Members[0].LastName != "Person" {
			t.Fatalf("members of %v: %+v", teamID, team.Members)
		}
		if team.CompetitionID != competitionID {
			t.Fatalf("competition id %v", team.CompetitionID)
		}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret.test", "sub-secret", "google_sub"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("detail exposes %q: %s", forbidden, encoded)
		}
	}

	emptyID := insertCompetitionRow("Autumn")
	empty, err := repo.GetCompetition(ctx, emptyID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Teams == nil || len(empty.Teams) != 0 {
		t.Fatalf("empty teams %+v", empty.Teams)
	}

	if _, err := repo.GetCompetition(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing competition: %v", err)
	}
}
