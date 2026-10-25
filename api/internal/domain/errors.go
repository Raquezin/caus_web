package domain

import (
	"errors"
	"sort"
	"strings"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type ValidationError struct {
	Fields map[string]string
}

func (e ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for key := range e.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	messages := make([]string, 0, len(keys))
	for _, key := range keys {
		messages = append(messages, key+": "+e.Fields[key])
	}
	if len(messages) == 0 {
		return "validation failed"
	}
	return "validation failed: " + strings.Join(messages, ", ")
}

func NewValidationError(fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	return ValidationError{Fields: fields}
}
