package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"caus-platform/api/internal/domain"
)

func TestTranslateBackendCheckViolationNamesField(t *testing.T) {
	err := translateBackendError(&pgconn.PgError{Code: "23514", ConstraintName: "talks_poster_url_valid"})
	violation, ok := err.(domain.ValidationError)
	if !ok || violation.Fields["poster_url"] == "" {
		t.Fatalf("check violation %v, want poster_url field error", err)
	}
}

func TestTranslateBackendUnknownCheckViolationFallsBackToData(t *testing.T) {
	err := translateBackendError(&pgconn.PgError{Code: "23514", ConstraintName: "unknown_constraint"})
	violation, ok := err.(domain.ValidationError)
	if !ok || violation.Fields["data"] == "" {
		t.Fatalf("unknown check violation %v, want data field error", err)
	}
}
