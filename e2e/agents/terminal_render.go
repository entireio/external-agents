package agents

import "strings"

func translateKey(k string) string {
	switch k {
	case "Enter":
		return "\r"
	case "Tab":
		return "\t"
	case "Escape", "Esc":
		return "\x1b"
	case "BSpace", "Backspace":
		return "\x7f"
	case "Up":
		return "\x1b[A"
	case "Down":
		return "\x1b[B"
	case "Right":
		return "\x1b[C"
	case "Left":
		return "\x1b[D"
	case "Space":
		return " "
	}
	if len(k) == 3 && (k[0] == 'C' || k[0] == 'c') && k[1] == '-' {
		c := k[2]
		if c >= 'a' && c <= 'z' {
			return string([]byte{c - 'a' + 1})
		}
		if c >= 'A' && c <= 'Z' {
			return string([]byte{c - 'A' + 1})
		}
	}
	return k
}

type renderedScreen struct {
	lines [][]rune
	row   int
	col   int
}

func newRenderedScreen() *renderedScreen {
	return &renderedScreen{lines: [][]rune{{}}}
}

func (s *renderedScreen) WriteString(input string) {
	_, _ = s.Write([]byte(input))
}

func (s *renderedScreen) Write(p []byte) (int, error) {
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\x1b':
			consumed := s.consumeEscape(p[i:])
			i += consumed - 1
		case '\r':
			s.col = 0
		case '\n':
			s.row++
			s.col = 0
			s.ensureRow(s.row)
		case '\b', 0x7f:
			if s.col > 0 {
				s.col--
			}
		case '\t':
			s.writeRune(' ')
			for s.col%8 != 0 {
				s.writeRune(' ')
			}
		default:
			if p[i] >= 0x20 {
				s.writeRune(rune(p[i]))
			}
		}
	}
	return len(p), nil
}

func (s *renderedScreen) String() string {
	last := len(s.lines) - 1
	for last > 0 && len(trimRightSpaces(s.lines[last])) == 0 {
		last--
	}

	out := make([]string, 0, last+1)
	for i := 0; i <= last; i++ {
		out = append(out, string(trimRightSpaces(s.lines[i])))
	}
	return strings.Join(out, "\n")
}

func (s *renderedScreen) consumeEscape(p []byte) int {
	if len(p) < 2 || p[1] != '[' {
		return 1
	}

	i := 2
	for i < len(p) {
		b := p[i]
		if (b >= '0' && b <= '9') || b == ';' || b == '?' {
			i++
			continue
		}
		s.applyCSI(string(p[2:i]), b)
		return i + 1
	}
	return len(p)
}

func (s *renderedScreen) applyCSI(params string, final byte) {
	values := parseCSIParams(params)
	switch final {
	case 'A':
		s.row = max(0, s.row-max1(values))
	case 'B':
		s.row += max1(values)
		s.ensureRow(s.row)
	case 'C':
		s.col += max1(values)
	case 'D':
		s.col = max(0, s.col-max1(values))
	case 'H', 'f':
		row := 1
		col := 1
		if len(values) >= 1 && values[0] > 0 {
			row = values[0]
		}
		if len(values) >= 2 && values[1] > 0 {
			col = values[1]
		}
		s.row = row - 1
		s.col = col - 1
		s.ensureRow(s.row)
	case 'J':
		if len(values) == 0 || values[0] == 0 {
			s.eraseDisplayFromCursor()
		}
	case 'K':
		mode := 0
		if len(values) > 0 {
			mode = values[0]
		}
		s.eraseLine(mode)
	case 'm':
		// SGR styling does not affect the rendered text content.
	}
}

func (s *renderedScreen) eraseDisplayFromCursor() {
	s.eraseLine(0)
	for row := s.row + 1; row < len(s.lines); row++ {
		s.lines[row] = s.lines[row][:0]
	}
}

func (s *renderedScreen) eraseLine(mode int) {
	s.ensureRow(s.row)
	line := s.lines[s.row]
	switch mode {
	case 1:
		if s.col >= len(line) {
			for i := range line {
				line[i] = ' '
			}
		} else {
			for i := 0; i <= s.col && i < len(line); i++ {
				line[i] = ' '
			}
		}
	case 2:
		s.lines[s.row] = s.lines[s.row][:0]
	default:
		if s.col < len(line) {
			s.lines[s.row] = line[:s.col]
		}
	}
}

func (s *renderedScreen) writeRune(r rune) {
	s.ensureRow(s.row)
	line := s.lines[s.row]
	for len(line) < s.col {
		line = append(line, ' ')
	}
	if s.col == len(line) {
		line = append(line, r)
	} else {
		line[s.col] = r
	}
	s.lines[s.row] = line
	s.col++
}

func (s *renderedScreen) ensureRow(row int) {
	for len(s.lines) <= row {
		s.lines = append(s.lines, []rune{})
	}
}

func trimRightSpaces(line []rune) []rune {
	end := len(line)
	for end > 0 && line[end-1] == ' ' {
		end--
	}
	return line[:end]
}

func parseCSIParams(params string) []int {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "?" {
			out = append(out, 0)
			continue
		}
		n := 0
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				n = 0
				break
			}
			n = n*10 + int(part[i]-'0')
		}
		out = append(out, n)
	}
	return out
}

func max1(values []int) int {
	if len(values) == 0 || values[0] <= 0 {
		return 1
	}
	return values[0]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
