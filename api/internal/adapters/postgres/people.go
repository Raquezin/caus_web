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

type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PeopleRepository struct {
	db Querier
}

func NewPeopleRepository(db Querier) *PeopleRepository {
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

func (r *PeopleRepository) List(ctx context.Context, limit, offset int) ([]domain.Person, error) {
	rows, err := r.db.Query(ctx,
		"SELECT id, first_name, last_name FROM people ORDER BY id LIMIT $1 OFFSET $2",
		limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list people: %w", err)
	}
	defer rows.Close()
	var people []domain.Person
	for rows.Next() {
		var person domain.Person
		var pgID pgtype.UUID
		if err := rows.Scan(&pgID, &person.FirstName, &person.LastName); err != nil {
			return nil, fmt.Errorf("postgres: list people: %w", err)
		}
		person.ID = pgID.Bytes
		people = append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list people: %w", err)
	}
	return people, nil
}
