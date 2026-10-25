package domain

import (
	"time"

	"github.com/google/uuid"
)

const MaxUserSearchLength = 200

type User struct {
	ID        uuid.UUID
	FirstName string
	LastName  string
	Email     *string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UserPage struct {
	Users   []User
	HasMore bool
}

type SessionMetadata struct {
	CreatedAt time.Time
	ExpiresAt time.Time
}

type AccountExport struct {
	Account      Account
	GoogleSub    string
	Sessions     []SessionMetadata
	Talks        []Talk
	Teams        []Team
	Competitions []Competition
}
