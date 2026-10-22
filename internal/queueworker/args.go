package queueworker

import "lara4ever/internal/i18n"

// SplitArgs splits a command-line-like string into individual args, honoring
// single quotes, double quotes, and backslash escapes (POSIX-ish). Empty
// input yields an empty slice. Unterminated quotes return an error.
func SplitArgs(s string) ([]string, error) {
	var out []string
	var cur []rune
	inSingle, inDouble := false, false
	hasToken := false

	for i := 0; i < len(s); i++ {
		c := rune(s[i])
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur = append(cur, c)
			}
		case inDouble:
			if c == '\\' && i+1 < len(s) {
				next := rune(s[i+1])
				if next == '"' || next == '\\' || next == '$' || next == '`' {
					cur = append(cur, next)
					i++
					continue
				}
				cur = append(cur, c)
			} else if c == '"' {
				inDouble = false
			} else {
				cur = append(cur, c)
			}
		default:
			switch c {
			case ' ', '\t':
				if hasToken {
					out = append(out, string(cur))
					cur = cur[:0]
					hasToken = false
				}
			case '\'':
				inSingle = true
				hasToken = true
			case '"':
				inDouble = true
				hasToken = true
			case '\\':
				if i+1 < len(s) {
					cur = append(cur, rune(s[i+1]))
					i++
					hasToken = true
				}
			default:
				cur = append(cur, c)
				hasToken = true
			}
		}
	}
	if inSingle || inDouble {
		return nil, i18n.Errorf("errors.queueworker.unterminatedQuote", nil)
	}
	if hasToken {
		out = append(out, string(cur))
	}
	return out, nil
}
