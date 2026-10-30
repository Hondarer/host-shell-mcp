package tools

import (
	"strings"
	"unicode/utf8"
)

// stripTerminal converts terminal output into plain text:
//   - Strips ANSI escape sequences (colors, cursor movement, window titles).
//   - Normalizes CRLF to LF and resolves bare CR and backspace by overwriting characters in the current line.
//   - Strips control characters except tab and newline.
//
// If s ends with an incomplete escape sequence, pending returns its byte length so the next read can resume there.
func stripTerminal(s string) (out string, pending int) {
	var sb strings.Builder
	line := []rune{} // current line buffer for CR and backspace handling
	col := 0
	flush := func() {
		sb.WriteString(string(line))
		line, col = line[:0], 0
	}
	put := func(r rune) {
		if col < len(line) {
			line[col] = r
		} else {
			line = append(line, r)
		}
		col++
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			n := escapeLen(s[i:])
			if n == 0 {
				return sb.String() + string(line), len(s) - i
			}
			i += n
			continue
		case c == '\n':
			flush()
			sb.WriteByte('\n')
		case c == '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				break // CR LF counts as LF.
			}
			col = 0
		case c == '\b':
			if col > 0 {
				col--
			}
		case c == '\t':
			put('\t')
		case c < 0x20 || c == 0x7f:
			// Remove other control characters.
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			put(r)
			i += n
			continue
		}
		i++
	}
	flush()
	return sb.String(), 0
}

// escapeLen returns the length of the escape sequence at s[0:], or 0 if incomplete.
func escapeLen(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch s[1] {
	case '[': // CSI: parameter/intermediate bytes terminated by 0x40-0x7e.
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return 0
	case ']', 'P', 'X', '^', '_': // OSC, DCS, etc.: terminated by BEL or ST (ESC \).
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return 0
	case '(', ')', '*', '+', '#', '%': // 3-byte sequences (charset selection, etc.).
		if len(s) < 3 {
			return 0
		}
		return 3
	default: // 2-byte ESC sequences (ESC =, ESC >, etc.).
		return 2
	}
}
