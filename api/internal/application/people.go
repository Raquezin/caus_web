package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type PersonRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error)
	List(ctx context.Context, limit, offset int) ([]domain.Person, error)
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

func (s *People) List(ctx context.Context, page domain.Page) (domain.PeoplePage, error) {
	if page.Limit < 1 || page.Limit > domain.MaxPageLimit || page.Offset < 0 {
		return domain.PeoplePage{}, fmt.Errorf("list people: %w", domain.ErrInvalidPagination)
	}
	people, err := s.repo.List(ctx, page.Limit+1, page.Offset)
	if err != nil {
		return domain.PeoplePage{}, err
	}
	result := domain.PeoplePage{People: people}
	if len(people) > page.Limit {
		result.People = people[:page.Limit]
		result.HasMore = true
	}
	return result, nil
}
