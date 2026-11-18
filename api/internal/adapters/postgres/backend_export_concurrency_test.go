package postgres

import (
	"context"
	"testing"
	"time"
)

func TestEndpointExportIgnoresConcurrentProfileWriteIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	own := insertExportPerson(t, tx, "Ada", "Lovelace", "owner@example.com", "owner-sub", "member")
	pool := endpointExportDatabase(t, tx)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(context.Background()) }()
	if _, err := writer.Exec(ctx, "UPDATE people SET first_name = 'Changed' WHERE id = $1", own); err != nil {
		t.Fatal(err)
	}

	results := make(chan endpointExportResult, 1)
	go func() {
		export, err := NewBackendRepository(pool).ExportAccount(ctx, own)
		results <- endpointExportResult{export: export, err: err}
	}()
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("export with a paused profile write: %v", result.err)
		}
		if result.export.Account.FirstName != "Ada" || result.export.Account.ID != own {
			t.Fatalf("export read the uncommitted profile %+v", result.export.Account)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("export blocked on the uncommitted profile write")
	}

	if err := writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	later, err := NewBackendRepository(pool).ExportAccount(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if later.Account.FirstName != "Changed" {
		t.Fatalf("export after the commit has first name %q, want Changed", later.Account.FirstName)
	}
}
