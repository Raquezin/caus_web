package domain

import (
	"strings"
	"unicode/utf8"
)

func ValidText(value string) bool {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\uFEFF') {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 0x20 && character != 0x7f {
			continue
		}
		if character != '\t' && character != '\r' && character != '\n' {
			return false
		}
	}
	return true
}
