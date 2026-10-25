package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestBackendIntegrationAccountExportDelete(t *testing.T) {
	f := newIntegrationFixture(t)
	subject := f.insertPerson("Gdpr", "Subject", "gdpr@example.test", "sub-gdpr", "admin")
	f.exec("INSERT INTO admin_email_bindings (email_hash) VALUES (sha256(convert_to($1, 'UTF8')))",
		"gdpr@example.test")
	first := f.createSession(subject)
	second := f.createSession(subject)
	var talkID uuid.UUID
	f.withTx(func(tx pgx.Tx) {
		if err := tx.QueryRow(context.Background(),
			"INSERT INTO talks (title, description, date, duration, location, published) "+
				"VALUES ('Draft history', 'desc', '2026-08-01 18:00', interval '1 hour', 'Madrid', false) RETURNING id").
			Scan(&talkID); err != nil {
			f.t.Fatal(err)
		}
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, subject); err != nil {
			f.t.Fatal(err)
		}
	})
	competitionID := f.createCompetitionRow("GDPR Cup", 3)
	teamID := f.createTeamRow(competitionID, "GDPR Team", []uuid.UUID{subject})

	status, header, body := f.do(integrationRequest{method: http.MethodGet, path: "/api/me/export", token: first})
	if status != http.StatusOK {
		t.Fatalf("export: status %d body %s", status, body)
	}
	if disposition := header.Get("Content-Disposition"); !strings.Contains(disposition, "caus-account.json") {
		t.Fatalf("export disposition %q", disposition)
	}
	export := integrationJSON(t, body)
	account := export["account"].(map[string]any)
	if account["id"] != subject.String() || export["google_sub"] != "sub-gdpr" {
		t.Fatalf("export identity %v %v", account, export["google_sub"])
	}
	if sessions, ok := export["sessions"].([]any); !ok || len(sessions) != 2 {
		t.Fatalf("export sessions %v", export["sessions"])
	}
	for _, entry := range export["sessions"].([]any) {
		session := entry.(map[string]any)
		if session["created_at"] == "" || session["expires_at"] == "" || len(session) != 2 {
			t.Fatalf("session metadata %v", session)
		}
	}
	if talks, ok := export["talks"].([]any); !ok || len(talks) != 1 ||
		talks[0].(map[string]any)["id"] != talkID.String() {
		t.Fatalf("export talks %v", export["talks"])
	}
	if teams, ok := export["teams"].([]any); !ok || len(teams) != 1 ||
		teams[0].(map[string]any)["id"] != teamID.String() {
		t.Fatalf("export teams %v", export["teams"])
	}
	if competitions, ok := export["competitions"].([]any); !ok || len(competitions) != 1 ||
		competitions[0].(map[string]any)["id"] != competitionID.String() {
		t.Fatalf("export competitions %v", export["competitions"])
	}
	if _, ok := export["admin_bindings"]; ok {
		t.Fatalf("export leaks admin bindings: %v", export["admin_bindings"])
	}
	text := string(body)
	for _, secret := range []string{first, second, "token_hash", "member@example.test", "admin@example.test", "sub-member"} {
		if strings.Contains(text, secret) {
			t.Fatalf("export leaked %q", secret)
		}
	}

	status, _, body = f.do(integrationRequest{
		method: http.MethodGet, path: "/api/me/export?person_id=" + f.memberID.String() + "&id=" + f.memberID.String(), token: first,
	})
	if status != http.StatusOK {
		t.Fatalf("query export: status %d body %s", status, body)
	}
	queried := integrationJSON(t, body)["account"].(map[string]any)
	if queried["id"] != subject.String() {
		t.Fatalf("query export leaked another subject %v", queried)
	}

	status, header, body = f.do(integrationRequest{
		method: http.MethodDelete, path: "/api/me", token: first, origins: []string{f.origin},
	})
	if status != http.StatusNoContent {
		t.Fatalf("delete account: status %d body %s", status, body)
	}
	if cookie := header.Get("Set-Cookie"); !strings.Contains(cookie, "caus_session=") ||
		(!strings.Contains(cookie, "Max-Age=0") && !strings.Contains(cookie, "1970")) {
		t.Fatalf("delete account cookie %q", cookie)
	}
	var email, googleSub *string
	var role, firstName string
	if err := f.pool.QueryRow(context.Background(),
		"SELECT first_name, email, google_sub, role::text FROM people WHERE id = $1", subject).
		Scan(&firstName, &email, &googleSub, &role); err != nil {
		t.Fatal(err)
	}
	if email != nil || googleSub != nil || role != "member" || firstName != "Gdpr" {
		t.Fatalf("deleted person email=%v sub=%v role=%q name=%q", email, googleSub, role, firstName)
	}
	if sessions := f.count("SELECT count(*) FROM sessions WHERE person_id = $1", subject); sessions != 0 {
		t.Fatalf("sessions retained: %d", sessions)
	}
	if markers := f.count("SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to('gdpr@example.test', 'UTF8'))"); markers != 1 {
		t.Fatalf("consumed marker removed by deletion: %d", markers)
	}
	if talks := f.count("SELECT count(*) FROM talks WHERE id = $1", talkID); talks != 1 {
		t.Fatal("talk history removed")
	}
	if links := f.count("SELECT count(*) FROM talk_speakers WHERE talk_id = $1", talkID); links != 1 {
		t.Fatal("talk speaker history removed")
	}
	if joins := f.count("SELECT count(*) FROM team_members WHERE team_id = $1", teamID); joins != 1 {
		t.Fatal("team membership removed")
	}
	for _, token := range []string{first, second} {
		status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: token})
		integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
	}
	status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: f.adminToken})
	if status != http.StatusOK || integrationJSON(t, body)["id"] != f.adminID.String() {
		t.Fatalf("unrelated account affected: status %d body %s", status, body)
	}
}

func TestBackendIntegrationLogoutRevokesSession(t *testing.T) {
	f := newIntegrationFixture(t)
	secondary := f.createSession(f.memberID)
	status, header, body := f.do(integrationRequest{
		method: http.MethodPost, path: "/api/auth/logout", token: secondary, origins: []string{f.origin},
	})
	if status != http.StatusNoContent {
		t.Fatalf("logout: status %d body %s", status, body)
	}
	if cookie := header.Get("Set-Cookie"); !strings.Contains(cookie, "caus_session=") ||
		(!strings.Contains(cookie, "Max-Age=0") && !strings.Contains(cookie, "1970")) {
		t.Fatalf("logout cookie %q", cookie)
	}
	status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: secondary})
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
	if sessions := f.count("SELECT count(*) FROM sessions WHERE person_id = $1", f.memberID); sessions != 1 {
		t.Fatalf("logout left %d sessions, want 1", sessions)
	}
	status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: f.memberToken})
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "member" {
		t.Fatalf("primary member session revoked: status %d body %s", status, body)
	}
	status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: f.adminToken})
	if status != http.StatusOK {
		t.Fatalf("admin session affected: status %d body %s", status, body)
	}
}
