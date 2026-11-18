package httpapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const dateFormatMessage = " must be a YYYY-MM-DDTHH:MM:SS local date"

type jsonField struct {
	present bool
	raw     json.RawMessage
}

func (f *jsonField) UnmarshalJSON(data []byte) error {
	f.present = true
	f.raw = append(f.raw[:0], data...)
	return nil
}

func (f jsonField) isNull() bool {
	return f.present && bytes.Equal(bytes.TrimSpace(f.raw), []byte("null"))
}

func invalidFields(fields map[string]string) error {
	return domain.NewValidationError(fields)
}

func decodeText(field jsonField, name string, fields map[string]string) (string, bool) {
	var value string
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a string"
		return "", false
	}
	return strings.TrimSpace(value), true
}

func requiredText(field jsonField, name string, fields map[string]string) string {
	if !field.present {
		fields[name] = name + " is required"
		return ""
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return ""
	}
	value, _ := decodeText(field, name, fields)
	return value
}

func strictText(field jsonField, name string, fields map[string]string) (string, bool) {
	if !field.present {
		return "", false
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return "", false
	}
	return decodeText(field, name, fields)
}

func nullableText(field jsonField, name string, fields map[string]string) *string {
	if !field.present || field.isNull() {
		return nil
	}
	value, ok := decodeText(field, name, fields)
	if !ok {
		return nil
	}
	return &value
}

func requiredNumber[T int | int64](field jsonField, name string, fields map[string]string) T {
	var value T
	if !field.present {
		fields[name] = name + " is required"
		return value
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return value
	}
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a whole number"
		return 0
	}
	return value
}

func strictNumber[T int | int64](field jsonField, name string, fields map[string]string) (T, bool) {
	var value T
	if !field.present {
		return value, false
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return value, false
	}
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a whole number"
		return value, false
	}
	return value, true
}

func nullableNumber[T int | int64](field jsonField, name string, fields map[string]string) *T {
	if !field.present || field.isNull() {
		return nil
	}
	var value T
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a whole number"
		return nil
	}
	return &value
}

func strictBool(field jsonField, name string, fields map[string]string) (bool, bool) {
	if !field.present {
		return false, false
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return false, false
	}
	var value bool
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a boolean"
		return false, false
	}
	return value, true
}

func requiredDate(field jsonField, name string, fields map[string]string) time.Time {
	var value time.Time
	if !field.present {
		fields[name] = name + " is required"
		return value
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return value
	}
	var text string
	if err := json.Unmarshal(field.raw, &text); err != nil {
		fields[name] = name + dateFormatMessage
		return value
	}
	date, err := readEventDate(text)
	if err != nil {
		fields[name] = name + dateFormatMessage
		return time.Time{}
	}
	return date
}

func requiredUUIDs(field jsonField, name string, fields map[string]string) []uuid.UUID {
	if !field.present {
		fields[name] = name + " is required"
		return nil
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return nil
	}
	var ids []uuid.UUID
	if err := json.Unmarshal(field.raw, &ids); err != nil {
		fields[name] = name + " must be an array of UUIDs"
		return nil
	}
	return ids
}
