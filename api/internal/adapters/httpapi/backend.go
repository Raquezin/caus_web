package httpapi

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type BackendService interface {
	ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error)
	GetTalk(ctx context.Context, id uuid.UUID, includeUnpublished bool) (domain.Talk, error)
	CalendarTalks(ctx context.Context) ([]domain.Talk, error)
	CreateTalk(ctx context.Context, input domain.TalkInput) (domain.Talk, error)
	UpdateTalk(ctx context.Context, id uuid.UUID, patch domain.TalkPatch) (domain.Talk, error)
	DeleteTalk(ctx context.Context, id uuid.UUID) error
	ListCompetitions(ctx context.Context, page domain.Page) (domain.CompetitionPage, error)
	GetCompetition(ctx context.Context, id uuid.UUID) (domain.CompetitionDetail, error)
	CreateCompetition(ctx context.Context, input domain.CompetitionInput) (domain.Competition, error)
	UpdateCompetition(ctx context.Context, id uuid.UUID, patch domain.CompetitionPatch) (domain.Competition, error)
	DeleteCompetition(ctx context.Context, id uuid.UUID) error
	ListTeams(ctx context.Context, competitionID uuid.UUID, page domain.Page) (domain.TeamPage, error)
	GetTeam(ctx context.Context, id uuid.UUID) (domain.Team, error)
	CreateTeam(ctx context.Context, competitionID uuid.UUID, input domain.TeamInput) (domain.Team, error)
	UpdateTeam(ctx context.Context, id uuid.UUID, patch domain.TeamPatch) (domain.Team, error)
	DeleteTeam(ctx context.Context, id uuid.UUID) error
	ListUsers(ctx context.Context, query string, page domain.Page) (domain.UserPage, error)
	UpdateUserRole(ctx context.Context, id uuid.UUID, role string) (domain.User, error)
	ExportAccount(ctx context.Context, id uuid.UUID) (domain.AccountExport, error)
	DeleteAccount(ctx context.Context, id uuid.UUID) error
}

type Backend struct {
	service BackendService
	auth    *Auth
	log     *slog.Logger
}

func NewBackend(service BackendService, auth *Auth, log *slog.Logger) *Backend {
	return &Backend{service: service, auth: auth, log: log}
}
