package httpapi

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func patchedText(field jsonField, name string, fields map[string]string) *string {
	if !field.present {
		return nil
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return nil
	}
	value, ok := decodeText(field, name, fields)
	if !ok {
		return nil
	}
	return &value
}

func patchedNumber[T int | int64](field jsonField, name string, fields map[string]string) *T {
	if !field.present {
		return nil
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return nil
	}
	var value T
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a whole number"
		return nil
	}
	return &value
}

func patchedBool(field jsonField, name string, fields map[string]string) *bool {
	if !field.present {
		return nil
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return nil
	}
	var value bool
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a boolean"
		return nil
	}
	return &value
}

func patchedDate(field jsonField, name string, fields map[string]string) *time.Time {
	if !field.present {
		return nil
	}
	if field.isNull() {
		fields[name] = name + " must not be null"
		return nil
	}
	var text string
	if err := json.Unmarshal(field.raw, &text); err != nil {
		fields[name] = name + dateFormatMessage
		return nil
	}
	date, err := readEventDate(text)
	if err != nil {
		fields[name] = name + dateFormatMessage
		return nil
	}
	return &date
}

func patchedUUIDs(field jsonField, name string, fields map[string]string) *[]uuid.UUID {
	if !field.present {
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
	return &ids
}

func patchedNullableText(field jsonField, name string, fields map[string]string) *domain.NullableText {
	if !field.present {
		return nil
	}
	if field.isNull() {
		return &domain.NullableText{}
	}
	value, ok := decodeText(field, name, fields)
	if !ok {
		return nil
	}
	return &domain.NullableText{Value: &value}
}

func patchedNullableInt(field jsonField, name string, fields map[string]string) *domain.NullableInt {
	if !field.present {
		return nil
	}
	if field.isNull() {
		return &domain.NullableInt{}
	}
	var value int
	if err := json.Unmarshal(field.raw, &value); err != nil {
		fields[name] = name + " must be a whole number"
		return nil
	}
	return &domain.NullableInt{Value: &value}
}
