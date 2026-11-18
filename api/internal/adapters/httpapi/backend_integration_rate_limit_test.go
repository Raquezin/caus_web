package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestBackendIntegrationPublicReadsAreNotRateLimited(t *testing.T) {
	f := newIntegrationFixture(t)
	ids := make([]uuid.UUID, 0, 61)
	for i := 0; i < 61; i++ {
		ids = append(ids, f.createCompetitionRow(fmt.Sprintf("Edition %d", i), 3))
	}
	status, _, body := f.public(http.MethodGet, "/api/talks?period=all&limit=100&offset=0")
	if status != http.StatusOK {
		t.Fatalf("talks page: status %d body %s", status, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/competitions?limit=100&offset=0")
	if status != http.StatusOK {
		t.Fatalf("competitions page: status %d body %s", status, body)
	}
	for i, id := range ids {
		status, header, body := f.public(http.MethodGet, "/api/competitions/"+id.String())
		if status != http.StatusOK {
			t.Fatalf("competition detail %d of %d from loopback without X-Forwarded-For: status %d Retry-After %q body %s",
				i+1, len(ids), status, header.Get("Retry-After"), body)
		}
	}
}
