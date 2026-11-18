package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"caus-platform/api/internal/domain"
)

var checkViolationFields = map[string]map[string]string{
	"talks_stream_url_valid":           {"stream_url": "stream_url must be an http or https URL"},
	"talks_poster_url_valid":           {"poster_url": "poster_url must be an http or https URL"},
	"teams_photo_url_valid":            {"photo_url": "photo_url must be an http or https URL"},
	"people_admin_requires_google_sub": {"role": "role admin requires an account that can sign in"},
}

func translateBackendError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrConflict
		case "23514":
			if fields, ok := checkViolationFields[pgErr.ConstraintName]; ok {
				return domain.ValidationError{Fields: fields}
			}
			return domain.ValidationError{Fields: map[string]string{"data": "invalid value"}}
		case "23503":
			return domain.ValidationError{Fields: map[string]string{"data": "invalid value"}}
		}
	}
	return err
}
