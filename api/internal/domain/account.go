package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrUnauthenticated = errors.New("unauthenticated")
var ErrInvalidIdentity = errors.New("invalid identity")
var ErrIdentityConflict = errors.New("identity conflict")

type Account struct {
	ID        uuid.UUID
	FirstName string
	LastName  string
	Email     string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GoogleIdentity struct {
	Subject   string
	Email     string
	FirstName string
	LastName  string
}
