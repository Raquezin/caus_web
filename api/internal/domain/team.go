package domain

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID            uuid.UUID
	CompetitionID uuid.UUID
	Name          *string
	PhotoURL      *string
	Position      *int
	Members       []Person
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TeamInput struct {
	Name      *string
	PhotoURL  *string
	Position  *int
	MemberIDs []uuid.UUID
}

type NullableInt struct {
	Value *int
}

type TeamPatch struct {
	Name      *NullableText
	PhotoURL  *NullableText
	Position  *NullableInt
	MemberIDs *[]uuid.UUID
}

type TeamPage struct {
	Teams   []Team
	HasMore bool
}
