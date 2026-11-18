package httpapi

import (
	"encoding/json"
	"testing"
)

type textReader struct {
	name string
	read func(jsonField, string, map[string]string) (string, bool)
}

func textReaders() []textReader {
	return []textReader{
		{"requiredText", func(field jsonField, name string, fields map[string]string) (string, bool) {
			return requiredText(field, name, fields), true
		}},
		{"strictText", strictText},
		{"nullableText", func(field jsonField, name string, fields map[string]string) (string, bool) {
			value := nullableText(field, name, fields)
			if value == nil {
				return "", false
			}
			return *value, true
		}},
		{"patchedText", func(field jsonField, name string, fields map[string]string) (string, bool) {
			value := patchedText(field, name, fields)
			if value == nil {
				return "", false
			}
			return *value, true
		}},
		{"patchedNullableText", func(field jsonField, name string, fields map[string]string) (string, bool) {
			value := patchedNullableText(field, name, fields)
			if value == nil || value.Value == nil {
				return "", false
			}
			return *value.Value, true
		}},
	}
}

func TestTextReadersTrimSurroundingWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"spaces", "  CompliCAUS 2027  ", "CompliCAUS 2027"},
		{"tabs", "\t\tCompliCAUS 2027\t", "CompliCAUS 2027"},
		{"newlines", "\nCompliCAUS 2027\r\n", "CompliCAUS 2027"},
		{"non breaking space", "\u00a0CompliCAUS 2027\u00a0", "CompliCAUS 2027"},
		{"inner whitespace", " Compli \t CAUS \n 2027 ", "Compli \t CAUS \n 2027"},
	}
	for _, reader := range textReaders() {
		t.Run(reader.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					raw, err := json.Marshal(tc.value)
					if err != nil {
						t.Fatal(err)
					}
					fields := map[string]string{}
					got, ok := reader.read(jsonField{present: true, raw: json.RawMessage(raw)}, "text", fields)
					if !ok {
						t.Fatalf("reader rejected %q with fields %v", tc.value, fields)
					}
					if got != tc.want {
						t.Fatalf("reader returned %q, want %q", got, tc.want)
					}
					if len(fields) != 0 {
						t.Fatalf("reader reported fields %v", fields)
					}
				})
			}
		})
	}
}
