package httpapi

import (
	"testing"

	"caus-platform/api/internal/adapters/postgres"
	"caus-platform/api/internal/application"
)

func TestBackendCompositionContracts(t *testing.T) {
	var repository application.BackendRepository = postgres.NewBackendRepository(nil)
	if repository == nil {
		t.Fatal("postgres backend repository does not satisfy application.BackendRepository")
	}
	var service BackendService = application.NewBackend(nil)
	if service == nil {
		t.Fatal("application backend does not satisfy httpapi.BackendService")
	}
}
