package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestBackendIntegrationTalkInProgressIsUpcoming(t *testing.T) {
	f := newIntegrationFixture(t)
	var id uuid.UUID
	f.withTx(func(tx pgx.Tx) {
		if err := tx.QueryRow(context.Background(),
			"INSERT INTO talks (title, description, date, duration, location, published) "+
				"VALUES ('Live now', 'description', date_trunc('second', (now() AT TIME ZONE 'Europe/Madrid') - interval '10 minutes'), "+
				"interval '1 hour', 'Sevilla', true) RETURNING id").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", id, f.memberID); err != nil {
			t.Fatal(err)
		}
	})
	status, _, body := f.public(http.MethodGet, "/api/talks?period=upcoming")
	if status != http.StatusOK {
		t.Fatalf("upcoming: status %d body %s", status, body)
	}
	upcoming := strings.Contains(string(body), id.String())
	status, _, body = f.public(http.MethodGet, "/api/talks?period=past")
	if status != http.StatusOK {
		t.Fatalf("past: status %d body %s", status, body)
	}
	past := strings.Contains(string(body), id.String())
	if !upcoming || past {
		t.Errorf("talk that started 10 minutes ago and lasts 1 hour: listed as upcoming=%v past=%v", upcoming, past)
	}
}
