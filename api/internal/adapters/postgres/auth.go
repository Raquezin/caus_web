package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

type AuthDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type AuthRepository struct {
	db AuthDB
}

func NewAuthRepository(db AuthDB) *AuthRepository {
	return &AuthRepository{db: db}
}

const accountColumns = "id, first_name, last_name, email, role, created_at, updated_at"

func scanAccount(row pgx.Row) (domain.Account, error) {
	var account domain.Account
	var id pgtype.UUID
	err := row.Scan(&id, &account.FirstName, &account.LastName, &account.Email, &account.Role, &account.CreatedAt, &account.UpdatedAt)
	if err != nil {
		return domain.Account{}, err
	}
	account.ID = id.Bytes
	return account, nil
}

func (r *AuthRepository) CompleteLogin(ctx context.Context, identity domain.GoogleIdentity, hash []byte, expiresAt time.Time, promote bool) (domain.Account, error) {
	account, err := r.completeLogin(ctx, identity, hash, expiresAt, promote)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.TableName == "people" {
			return domain.Account{}, domain.ErrIdentityConflict
		}
		return domain.Account{}, fmt.Errorf("postgres: complete login: %w", err)
	}
	return account, nil
}

func (r *AuthRepository) completeLogin(ctx context.Context, identity domain.GoogleIdentity, hash []byte, expiresAt time.Time, promote bool) (domain.Account, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	defer tx.Rollback(ctx)
	account, err := scanAccount(tx.QueryRow(ctx,
		"INSERT INTO people (first_name, last_name, email, google_sub) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING RETURNING "+accountColumns,
		identity.FirstName, identity.LastName, identity.Email, identity.Subject))
	if errors.Is(err, pgx.ErrNoRows) {
		account, err = scanAccount(tx.QueryRow(ctx,
			"UPDATE people SET email = $2 WHERE google_sub = $1 RETURNING "+accountColumns,
			identity.Subject, identity.Email))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Account{}, domain.ErrIdentityConflict
		}
	}
	if err != nil {
		return domain.Account{}, err
	}
	if promote {
		marker, err := tx.Exec(ctx,
			"INSERT INTO admin_email_bindings (email_hash) VALUES (sha256(convert_to($1::text, 'UTF8'))) ON CONFLICT DO NOTHING",
			identity.Email)
		if err != nil {
			return domain.Account{}, err
		}
		if marker.RowsAffected() == 1 && account.Role != "admin" {
			account, err = scanAccount(tx.QueryRow(ctx,
				"UPDATE people SET role = 'admin' WHERE google_sub = $1 RETURNING "+accountColumns, identity.Subject))
			if err != nil {
				return domain.Account{}, err
			}
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= now()"); err != nil {
		return domain.Account{}, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO sessions (token_hash, person_id, expires_at) VALUES ($1, $2, $3)", hash, account.ID, expiresAt); err != nil {
		return domain.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

func (r *AuthRepository) FindSession(ctx context.Context, hash []byte, now time.Time) (domain.Account, error) {
	account, err := scanAccount(r.db.QueryRow(ctx,
		"SELECT p.id, p.first_name, p.last_name, p.email, p.role, p.created_at, p.updated_at FROM sessions s JOIN people p ON p.id = s.person_id WHERE s.token_hash = $1 AND s.expires_at > $2 AND p.email IS NOT NULL AND p.google_sub IS NOT NULL",
		hash, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("postgres: find session: %w", err)
	}
	return account, nil
}

func (r *AuthRepository) DeleteSession(ctx context.Context, hash []byte) error {
	if _, err := r.db.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hash); err != nil {
		return fmt.Errorf("postgres: delete session: %w", err)
	}
	return nil
}
