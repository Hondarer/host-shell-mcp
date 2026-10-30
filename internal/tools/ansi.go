package tools

import (
	"strings"
	"unicode/utf8"
)

// stripTerminal turns output meant for a terminal into plain text the agent can read.
//
//   - Removes escape sequences (colors, cursor movement, window titles and so on).
//   - Turns CR LF into LF; for a CR in the middle of a line (overwritten progress), keeps only what was written last.
//   - A backspace deletes the previous character. Control characters other than tab and newline are removed.
//
// When the text ends with an incomplete escape sequence, its length is returned in pending.
// Starting the next read at that position avoids splitting the sequence.
func stripTerminal(s string) (out string, pending int) {
	var sb strings.Builder
	line := []rune{} // the current line, built per line to handle CR and backspace
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

// escapeLen returns the length of the escape sequence at the start of s, or 0 when it is incomplete.
func escapeLen(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch s[1] {
	case '[': // CSI: parameters and intermediate bytes, ending with 0x40-0x7e.
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return 0
	case ']', 'P', 'X', '^', '_': // OSC, DCS and so on: end with BEL or ESC \.
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return 0
	case '(', ')', '*', '+', '#', '%': // character set selection and so on: ESC and two characters.
		if len(s) < 3 {
			return 0
		}
		return 3
	default: // ESC and one character (ESC =, ESC > and so on).
		return 2
	}
}
