package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertExportPerson(t *testing.T, tx pgx.Tx, first, last, email, subject, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name, email, google_sub, role) "+
			"VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5::person_role) RETURNING id",
		first, last, email, subject, role).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertExportSession(t *testing.T, tx pgx.Tx, personID uuid.UUID, created time.Time, token string) {
	t.Helper()
	hash := sha256.Sum256([]byte(token))
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO sessions (token_hash, person_id, created_at, expires_at) VALUES ($1, $2, $3, $4)",
		hash[:], personID, created, created.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func insertExportTalk(t *testing.T, tx pgx.Tx, title, date string, published bool, speakers ...uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO talks (title, description, date, duration, location, language, published) "+
			"VALUES ($1, 'Description', $2::timestamp, interval '1 hour', 'Room', 'en', $3) RETURNING id",
		title, date, published).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	for _, speaker := range speakers {
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", id, speaker); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func insertExportCompetition(t *testing.T, tx pgx.Tx, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'Description', '2026-05-02 18:00:00', 'Hall', 3) RETURNING id", name).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertExportTeam(t *testing.T, tx pgx.Tx, competitionID uuid.UUID, name string, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO teams (competition_id, name) VALUES ($1, $2) RETURNING id", competitionID, name).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			id, competitionID, member); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestEndpointExportAccountIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	own := insertExportPerson(t, tx, "Ada", "Lovelace", "owner@example.com", "owner-sub", "admin")
	other := insertExportPerson(t, tx, "Grace", "Hopper", "other@example.com", "other-sub", "member")
	empty := insertExportPerson(t, tx, "Empty", "Account", "", "", "member")

	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	insertExportSession(t, tx, own, created, "owner-new")
	insertExportSession(t, tx, own, created.Add(-time.Hour), "owner-old")
	insertExportSession(t, tx, other, created, "other-session")

	publishedTalk := insertExportTalk(t, tx, "Published", "2026-04-01 10:00:00", true, own)
	unpublishedTalk := insertExportTalk(t, tx, "Draft", "2026-04-02 10:00:00", false, own, other)
	otherTalk := insertExportTalk(t, tx, "Other", "2026-04-03 10:00:00", true, other)

	spring := insertExportCompetition(t, tx, "Spring")
	autumn := insertExportCompetition(t, tx, "Autumn")
	team := insertExportTeam(t, tx, spring, "Spring Team", own, other)
	insertExportTeam(t, tx, autumn, "Autumn Team", other)

	repo := NewBackendRepository(endpointExportDatabase(t, tx))

	export, err := repo.ExportAccount(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if export.Account.ID != own || export.Account.FirstName != "Ada" || export.Account.LastName != "Lovelace" ||
		export.Account.Email != "owner@example.com" || export.Account.Role != "admin" || export.GoogleSub != "owner-sub" {
		t.Fatalf("account %+v subject %q", export.Account, export.GoogleSub)
	}
	if len(export.Sessions) != 2 || !export.Sessions[0].CreatedAt.Equal(created) ||
		!export.Sessions[1].CreatedAt.Equal(created.Add(-time.Hour)) ||
		!export.Sessions[0].ExpiresAt.Equal(created.Add(2*time.Hour)) {
		t.Fatalf("sessions %+v", export.Sessions)
	}
	if len(export.Talks) != 2 {
		t.Fatalf("talks %+v", export.Talks)
	}
	talks := map[uuid.UUID]domain.Talk{}
	for _, talk := range export.Talks {
		talks[talk.ID] = talk
	}
	if _, ok := talks[publishedTalk]; !ok {
		t.Fatal("published own talk missing")
	}
	draft, ok := talks[unpublishedTalk]
	if !ok || draft.Published || len(draft.Speakers) != 2 {
		t.Fatalf("unpublished own talk %+v", draft)
	}
	if _, ok := talks[otherTalk]; ok {
		t.Fatal("talk without own participation exported")
	}
	if len(export.Teams) != 1 || export.Teams[0].ID != team || len(export.Teams[0].Members) != 2 {
		t.Fatalf("teams %+v", export.Teams)
	}
	if len(export.Competitions) != 1 || export.Competitions[0].ID != spring {
		t.Fatalf("competitions %+v", export.Competitions)
	}
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{"other@example.com", "other-sub", "foreign-sub"}
	for _, token := range []string{"owner-new", "owner-old", "other-session"} {
		hash := sha256.Sum256([]byte(token))
		secrets = append(secrets, token, hex.EncodeToString(hash[:]), base64.StdEncoding.EncodeToString(hash[:]))
	}
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("export leaked %q: %s", secret, encoded)
		}
	}

	emptyExport, err := repo.ExportAccount(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	if emptyExport.Account.ID != empty || emptyExport.GoogleSub != "" {
		t.Fatalf("empty account %+v subject %q", emptyExport.Account, emptyExport.GoogleSub)
	}
	for name, size := range map[string]int{
		"sessions": len(emptyExport.Sessions), "talks": len(emptyExport.Talks), "teams": len(emptyExport.Teams),
		"competitions": len(emptyExport.Competitions),
	} {
		if size != 0 {
			t.Fatalf("empty export %s contains %d rows", name, size)
		}
	}
	emptyEncoded, err := json.Marshal(emptyExport)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"Sessions":[]`, `"Talks":[]`, `"Teams":[]`, `"Competitions":[]`} {
		if !strings.Contains(string(emptyEncoded), field) {
			t.Fatalf("empty export missing %s: %s", field, emptyEncoded)
		}
	}

	if _, err := repo.ExportAccount(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing account error %v, want not found", err)
	}
}
