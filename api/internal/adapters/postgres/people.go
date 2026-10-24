package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

type QueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PeopleRepository struct {
	db QueryRower
}

func NewPeopleRepository(db QueryRower) *PeopleRepository {
	return &PeopleRepository{db: db}
}

func (r *PeopleRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error) {
	var person domain.Person
	var pgID pgtype.UUID
	err := r.db.QueryRow(ctx,
		"SELECT id, first_name, last_name FROM people WHERE id = $1",
		pgtype.UUID{Bytes: id, Valid: true},
	).Scan(&pgID, &person.FirstName, &person.LastName)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Person{}, domain.ErrPersonNotFound
	}
	if err != nil {
		return domain.Person{}, fmt.Errorf("postgres: get person by id: %w", err)
	}
	person.ID = pgID.Bytes
	return person, nil
}
