package domain

import (
	"errors"

	"github.com/google/uuid"
)

var ErrPersonNotFound = errors.New("person not found")
var ErrInvalidPagination = errors.New("invalid pagination")

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

type Person struct {
	ID        uuid.UUID
	FirstName string
	LastName  string
}

type Page struct {
	Limit  int
	Offset int
}

type PeoplePage struct {
	People  []Person
	HasMore bool
}
