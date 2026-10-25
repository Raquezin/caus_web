package httpapi

import (
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type talkDTO struct {
	ID          uuid.UUID        `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Date        string           `json:"date"`
	Duration    int64            `json:"duration"`
	Location    string           `json:"location"`
	Language    string           `json:"language"`
	StreamURL   *string          `json:"stream_url"`
	PosterURL   *string          `json:"poster_url"`
	Published   bool             `json:"published"`
	Speakers    []personResponse `json:"speakers"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type competitionDTO struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Date        string    `json:"date"`
	Location    string    `json:"location"`
	TeamSize    int       `json:"team_size"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type competitionDetailDTO struct {
	Competition competitionDTO `json:"competition"`
	Teams       []any          `json:"teams"`
}

type teamDTO struct {
	ID            uuid.UUID        `json:"id"`
	CompetitionID uuid.UUID        `json:"competition_id"`
	Name          *string          `json:"name"`
	PhotoURL      *string          `json:"photo_url"`
	Position      *int             `json:"position"`
	Members       []personResponse `json:"members"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type userDTO struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     *string   `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type accountDTO struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sessionMetadataDTO struct {
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type accountExportDTO struct {
	Account      accountDTO           `json:"account"`
	GoogleSub    string               `json:"google_sub"`
	Sessions     []sessionMetadataDTO `json:"sessions"`
	Talks        []any                `json:"talks"`
	Teams        []any                `json:"teams"`
	Competitions []any                `json:"competitions"`
}

func personResponses(people []domain.Person) []personResponse {
	responses := make([]personResponse, 0, len(people))
	for _, person := range people {
		responses = append(responses, personResponse{ID: person.ID, FirstName: person.FirstName, LastName: person.LastName})
	}
	return responses
}

func talkResponse(talk domain.Talk) any {
	return talkDTO{
		ID:          talk.ID,
		Title:       talk.Title,
		Description: talk.Description,
		Date:        formatEventDate(talk.Date),
		Duration:    talk.Duration,
		Location:    talk.Location,
		Language:    talk.Language,
		StreamURL:   talk.StreamURL,
		PosterURL:   talk.PosterURL,
		Published:   talk.Published,
		Speakers:    personResponses(talk.Speakers),
		CreatedAt:   talk.CreatedAt.UTC(),
		UpdatedAt:   talk.UpdatedAt.UTC(),
	}
}

func talkResponses(talks []domain.Talk) []any {
	responses := make([]any, 0, len(talks))
	for _, talk := range talks {
		responses = append(responses, talkResponse(talk))
	}
	return responses
}

func competitionDTOOf(competition domain.Competition) competitionDTO {
	return competitionDTO{
		ID:          competition.ID,
		Name:        competition.Name,
		Description: competition.Description,
		Date:        formatEventDate(competition.Date),
		Location:    competition.Location,
		TeamSize:    competition.TeamSize,
		CreatedAt:   competition.CreatedAt.UTC(),
		UpdatedAt:   competition.UpdatedAt.UTC(),
	}
}

func competitionResponse(competition domain.Competition) any {
	return competitionDTOOf(competition)
}

func competitionResponses(competitions []domain.Competition) []any {
	responses := make([]any, 0, len(competitions))
	for _, competition := range competitions {
		responses = append(responses, competitionResponse(competition))
	}
	return responses
}

func competitionDetailResponse(detail domain.CompetitionDetail) any {
	return competitionDetailDTO{
		Competition: competitionDTOOf(detail.Competition),
		Teams:       teamResponses(detail.Teams),
	}
}

func teamResponse(team domain.Team) any {
	return teamDTO{
		ID:            team.ID,
		CompetitionID: team.CompetitionID,
		Name:          team.Name,
		PhotoURL:      team.PhotoURL,
		Position:      team.Position,
		Members:       personResponses(team.Members),
		CreatedAt:     team.CreatedAt.UTC(),
		UpdatedAt:     team.UpdatedAt.UTC(),
	}
}

func teamResponses(teams []domain.Team) []any {
	responses := make([]any, 0, len(teams))
	for _, team := range teams {
		responses = append(responses, teamResponse(team))
	}
	return responses
}

func userResponse(user domain.User) any {
	return userDTO{
		ID:        user.ID,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Email:     user.Email,
		Role:      user.Role,
		CreatedAt: user.CreatedAt.UTC(),
		UpdatedAt: user.UpdatedAt.UTC(),
	}
}

func userResponses(users []domain.User) []any {
	responses := make([]any, 0, len(users))
	for _, user := range users {
		responses = append(responses, userResponse(user))
	}
	return responses
}

func accountDTOOf(account domain.Account) accountDTO {
	return accountDTO{
		ID:        account.ID,
		FirstName: account.FirstName,
		LastName:  account.LastName,
		Email:     account.Email,
		Role:      account.Role,
		CreatedAt: account.CreatedAt.UTC(),
		UpdatedAt: account.UpdatedAt.UTC(),
	}
}

func accountResponse(account domain.Account) any {
	return accountDTOOf(account)
}

func exportResponse(export domain.AccountExport) any {
	sessions := make([]sessionMetadataDTO, 0, len(export.Sessions))
	for _, session := range export.Sessions {
		sessions = append(sessions, sessionMetadataDTO{CreatedAt: session.CreatedAt.UTC(), ExpiresAt: session.ExpiresAt.UTC()})
	}
	return accountExportDTO{
		Account:      accountDTOOf(export.Account),
		GoogleSub:    export.GoogleSub,
		Sessions:     sessions,
		Talks:        talkResponses(export.Talks),
		Teams:        teamResponses(export.Teams),
		Competitions: competitionResponses(export.Competitions),
	}
}
