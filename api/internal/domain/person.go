package domain

import (
	"errors"

	"github.com/google/uuid"
)

var ErrPersonNotFound = errors.New("person not found")

type Person struct {
	ID        uuid.UUID
	FirstName string
	LastName  string
}
