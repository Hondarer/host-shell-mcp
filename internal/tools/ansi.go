package tools

import (
	"strings"
	"unicode/utf8"
)

// stripTerminal は端末向けの出力を、エージェントが読める平文にします。
//
//   - エスケープ シーケンス (色、カーソル移動、ウィンドウ タイトルなど) を除く。
//   - CR LF を LF にし、行の途中の CR (進捗表示の上書き) は、最後に書かれた内容だけを残す。
//   - バックスペースは直前の文字を消す。タブと改行以外の制御文字は除く。
//
// 末尾が途中で切れたエスケープ シーケンスの場合は、その長さを pending に返します。
// 次の読み取りをその位置から始めると、シーケンスを分断せずに済みます。
func stripTerminal(s string) (out string, pending int) {
	var sb strings.Builder
	line := []rune{} // 現在の行。CR とバックスペースの処理のために行単位で組み立てる。
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
				break // CR LF は LF として扱う。
			}
			col = 0
		case c == '\b':
			if col > 0 {
				col--
			}
		case c == '\t':
			put('\t')
		case c < 0x20 || c == 0x7f:
			// その他の制御文字は除く。
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

// escapeLen は s の先頭のエスケープ シーケンスの長さを返します。途中で切れている場合は 0 です。
func escapeLen(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch s[1] {
	case '[': // CSI: パラメーターと中間バイトの後、0x40-0x7e で終わる。
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return 0
	case ']', 'P', 'X', '^', '_': // OSC、DCS など: BEL または ESC \ で終わる。
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return 0
	case '(', ')', '*', '+', '#', '%': // 文字集合の指定など: ESC と 2 文字。
		if len(s) < 3 {
			return 0
		}
		return 3
	default: // ESC と 1 文字 (ESC =、ESC > など)。
		return 2
	}
}
