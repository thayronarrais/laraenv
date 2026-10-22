// Package clipboard provides native text clipboard access for terminal actions.
package clipboard

import (
	"errors"
	"strings"
	"unicode/utf16"
)

func encodeText(text string) ([]uint16, error) {
	if strings.ContainsRune(text, '\x00') {
		return nil, errors.New("clipboard text cannot contain a NUL character")
	}
	return append(utf16.Encode([]rune(text)), 0), nil
}

func decodeText(data []uint16) (string, error) {
	for i, unit := range data {
		if unit == 0 {
			return string(utf16.Decode(data[:i])), nil
		}
	}
	return "", errors.New("clipboard text has no terminator within its allocation")
}
