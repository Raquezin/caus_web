package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type PersonRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error)
}

type People struct {
	repo PersonRepository
}

func NewPeople(repo PersonRepository) *People {
	return &People{repo: repo}
}

func (s *People) GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error) {
	return s.repo.GetByID(ctx, id)
}
