package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type integrationRequest struct {
	method  string
	path    string
	body    any
	raw     []byte
	token   string
	origins []string
	headers map[string]string
	ctype   string
}

func (f *integrationFixture) do(request integrationRequest) (int, http.Header, []byte) {
	f.t.Helper()
	var reader io.Reader
	if request.raw != nil {
		reader = bytes.NewReader(request.raw)
	} else if request.body != nil {
		data, err := json.Marshal(request.body)
		if err != nil {
			f.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	httpRequest, err := http.NewRequest(request.method, f.server.URL+request.path, reader)
	if err != nil {
		f.t.Fatal(err)
	}
	if request.token != "" {
		httpRequest.AddCookie(&http.Cookie{Name: "caus_session", Value: request.token})
	}
	for _, origin := range request.origins {
		httpRequest.Header.Add("Origin", origin)
	}
	if request.ctype != "" {
		httpRequest.Header.Set("Content-Type", request.ctype)
	} else if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	for name, value := range request.headers {
		httpRequest.Header.Set(name, value)
	}
	response, err := f.client.Do(httpRequest)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return response.StatusCode, response.Header, data
}

func (f *integrationFixture) public(method, path string) (int, http.Header, []byte) {
	f.t.Helper()
	return f.do(integrationRequest{method: method, path: path})
}

func (f *integrationFixture) admin(method, path string, body any) (int, http.Header, []byte) {
	f.t.Helper()
	return f.do(integrationRequest{method: method, path: path, body: body, token: f.adminToken, origins: []string{f.origin}})
}

func (f *integrationFixture) memberRequest(method, path string, body any) (int, http.Header, []byte) {
	f.t.Helper()
	return f.do(integrationRequest{method: method, path: path, body: body, token: f.memberToken, origins: []string{f.origin}})
}

func integrationJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("invalid JSON body %q: %v", body, err)
	}
	return decoded
}

func integrationData(t *testing.T, body []byte) []any {
	t.Helper()
	data, ok := integrationJSON(t, body)["data"].([]any)
	if !ok {
		t.Fatalf("body %q has no data array", body)
	}
	return data
}

func integrationErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	errorObject, ok := integrationJSON(t, body)["error"].(map[string]any)
	if !ok {
		t.Fatalf("body %q has no error object", body)
	}
	code, _ := errorObject["code"].(string)
	return code
}

func integrationExpect(t *testing.T, status int, body []byte, want int, code string) {
	t.Helper()
	if status != want {
		t.Fatalf("status %d, want %d (body %s)", status, want, body)
	}
	if code != "" {
		if got := integrationErrorCode(t, body); got != code {
			t.Fatalf("error code %q, want %q (body %s)", got, code, body)
		}
	}
}

func (f *integrationFixture) createPublishedTalk(title string, published bool) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	f.withTx(func(tx pgx.Tx) {
		if err := tx.QueryRow(context.Background(),
			"INSERT INTO talks (title, description, date, duration, location, published) "+
				"VALUES ($1, 'description', '2026-05-01 18:00', interval '1 hour', 'Madrid', $2) RETURNING id",
			title, published).Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", id, f.memberID); err != nil {
			f.t.Fatal(err)
		}
	})
	return id
}

func (f *integrationFixture) createCompetitionRow(name string, teamSize int) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'description', '2026-06-01 10:00', 'Madrid', $2) RETURNING id",
		name, teamSize).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *integrationFixture) createTeamRow(competitionID uuid.UUID, name string, memberIDs []uuid.UUID) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	f.withTx(func(tx pgx.Tx) {
		if err := tx.QueryRow(context.Background(),
			"INSERT INTO teams (competition_id, name) VALUES ($1, $2) RETURNING id",
			competitionID, name).Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		for _, memberID := range memberIDs {
			if _, err := tx.Exec(context.Background(),
				"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
				id, competitionID, memberID); err != nil {
				f.t.Fatal(err)
			}
		}
	})
	return id
}
