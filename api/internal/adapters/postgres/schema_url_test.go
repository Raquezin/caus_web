package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestURLValidationIntegration(t *testing.T) {
	tx := schemaFixture(t)
	ctx := context.Background()
	valid := []string{
		"https://example.com", "https://example.com?download=1", "https://example.com#section",
		"https://example.com?", "https://example.com#", "https://example.com/path?x=1#part",
		"https://localhost:8080", "http://127.0.0.1:1", "https://[::1]:65535/path",
		"https://[::ffff:192.0.2.1]/", "HTTPS://EXAMPLE.COM/Path",
		"https://xn--nxasmq6b.example/", "https://example.com./", "https://example.com/path/100%25",
		"https://example.com:080/",
	}
	for _, value := range valid {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT is_valid_url($1)", value).Scan(&ok); err != nil {
			t.Fatalf("is_valid_url(%q): %v", value, err)
		}
		if !ok {
			t.Errorf("is_valid_url(%q) = false, want true", value)
		}
	}
	longLabel := strings.Repeat("a", 64) + ".example"
	longName := strings.Repeat("a.", 126) + "aa.example"
	invalid := []string{
		"https://example.com:65536", "https://-bad.example/", "https://bad-.example/",
		"https://example..com/", "https://", "https://exa mple.com/", "https://example.com/a b",
		"https://example.com/\n", "https://example.com/a\\b", "https://user@example.com/",
		"ftp://example.com/", "javascript:alert(1)", "https://example.com:", "https://example.com:abc",
		"https://example.com:-1", "https://example.com:0", "https://example.com:99999999999999999999",
		"https://" + longLabel + "/", "https://" + longName + "/",
		"https://256.1.1.1/", "https://1.2.3.4.5/", "https://[127.0.0.1]/", "https://[::1",
		"https://[xyz]/", "https://::1/", "https://example.com/%ZZ", "https://example.com/%",
	}
	for _, value := range invalid {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT is_valid_url($1)", value).Scan(&ok); err != nil {
			t.Fatalf("is_valid_url(%q): %v", value, err)
		}
		if ok {
			t.Errorf("is_valid_url(%q) = true, want false", value)
		}
	}
	var nullResult *bool
	if err := tx.QueryRow(ctx, "SELECT is_valid_url(NULL)").Scan(&nullResult); err != nil {
		t.Fatal(err)
	}
	if nullResult != nil {
		t.Fatalf("is_valid_url(NULL) = %v, want NULL", *nullResult)
	}
	insert := func(query string, args ...any) error {
		if _, err := tx.Exec(ctx, "SAVEPOINT url_check"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, query, args...)
		if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT url_check"); rbErr != nil {
			t.Fatal(rbErr)
		}
		return err
	}
	goodTalk := "INSERT INTO talks (title,description,date,duration,location,%s) VALUES ($1,'desc',now(),interval '1 hour','loc',$2)"
	for _, column := range []string{"stream_url", "poster_url"} {
		if err := insert(fmt.Sprintf(goodTalk, column), "talk-"+column, "https://example.com/path?x=1#part"); err != nil {
			t.Errorf("talks.%s rejected valid URL: %v", column, err)
		}
		if err := insert(fmt.Sprintf(goodTalk, column), "talk-"+column+"-bad", "javascript:alert(1)"); err == nil {
			t.Errorf("talks.%s accepted javascript URL", column)
		} else {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("talks.%s bad URL error %v, want SQLSTATE 23514", column, err)
			}
		}
	}
	competitionID := insertCompetition(t, tx, "url-comp", 3)
	goodTeam := "INSERT INTO teams (competition_id,name,photo_url) VALUES ($1,$2,$3)"
	if err := insert(goodTeam, competitionID, "url-team", "https://example.com/path?x=1#part"); err != nil {
		t.Errorf("teams.photo_url rejected valid URL: %v", err)
	}
	if err := insert(goodTeam, competitionID, "url-team-bad", "https://example.com:99999"); err == nil {
		t.Error("teams.photo_url accepted bad port URL")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("teams.photo_url bad URL error %v, want SQLSTATE 23514", pgErr)
		}
	}
}
