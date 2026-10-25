package domain

import (
	"time"

	"github.com/google/uuid"
)

type Competition struct {
	ID          uuid.UUID
	Name        string
	Description string
	Location    string
	Date        time.Time
	TeamSize    int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CompetitionInput struct {
	Name        string
	Description string
	Location    string
	Date        time.Time
	TeamSize    int
}

type CompetitionPatch struct {
	Name        *string
	Description *string
	Location    *string
	Date        *time.Time
	TeamSize    *int
}

type CompetitionDetail struct {
	Competition Competition
	Teams       []Team
}

type CompetitionPage struct {
	Competitions []Competition
	HasMore      bool
}
