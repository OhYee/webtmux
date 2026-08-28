// Package ansi turns terminal output into something a browser can reflow.
//
// A terminal is a fixed grid: every line is hard-wrapped at the pane width, so
// on a phone you either scroll sideways or shrink the text until it is
// unreadable. HTML has no such constraint — given the logical lines and their
// colours, it can wrap them to whatever width the screen actually is.
//
// So the escape sequences are parsed here into spans rather than replayed into
// a terminal emulator. Colours become class names so the page's palette stays
// in CSS; only 256-colour and true-colour values, which no fixed palette can
// cover, fall back to an inline style.
package ansi

import (
	"fmt"
	"strconv"
	"strings"
)

// Span is a run of text sharing one appearance.
type Span struct {
	Text  string `json:"t"`
	Class string `json:"c,omitempty"`
	Style string `json:"s,omitempty"`
}

// Line is one logical line of output.
type Line []Span

type style struct {
	fg, bg        string // class suffix, e.g. "3"
	fgHex, bgHex  string // set instead for 256/true colour
	bold, dim     bool
	italic, under bool
	inverse       bool
}

func (s style) class() string {
	var parts []string
	if s.fg != "" {
		parts = append(parts, "f"+s.fg)
	}
	if s.bg != "" {
		parts = append(parts, "b"+s.bg)
	}
	if s.bold {
		parts = append(parts, "bo")
	}
	if s.dim {
		parts = append(parts, "di")
	}
	if s.italic {
		parts = append(parts, "it")
	}
	if s.under {
		parts = append(parts, "un")
	}
	if s.inverse {
		parts = append(parts, "in")
	}
	return strings.Join(parts, " ")
}

func (s style) inline() string {
	var parts []string
	if s.fgHex != "" {
		parts = append(parts, "color:"+s.fgHex)
	}
	if s.bgHex != "" {
		parts = append(parts, "background:"+s.bgHex)
	}
	return strings.Join(parts, ";")
}

// Parse converts captured output into lines of spans. Input is expected to
// contain SGR sequences only, which is what `tmux capture-pane -e` produces.
func Parse(input string) []Line {
	var lines []Line

	for _, raw := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		lines = append(lines, parseLine(raw))
	}
	return lines
}

func parseLine(raw string) Line {
	// Non-nil so a blank line marshals as [] rather than null; a null would
	// blow up any consumer that maps over the spans.
	line := Line{}

	var (
		cur style
		buf strings.Builder
	)

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		line = append(line, Span{Text: buf.String(), Class: cur.class(), Style: cur.inline()})
		buf.Reset()
	}

	for i := 0; i < len(raw); {
		if raw[i] != 0x1b {
			// Copy a whole rune so multi-byte characters survive intact.
			size := runeLen(raw[i])
			if i+size > len(raw) {
				size = 1
			}
			buf.WriteString(raw[i : i+size])
			i += size
			continue
		}

		params, next, ok := readCSI(raw, i)
		if !ok {
			// Not a sequence we understand; drop the escape byte rather than
			// letting it show up as a stray glyph.
			i++
			continue
		}
		flush()
		cur = applySGR(cur, params)
		i = next
	}

	flush()
	return line
}

// runeLen reports the byte length of a UTF-8 rune from its first byte.
func runeLen(b byte) int {
	switch {
	case b&0x80 == 0:
		return 1
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	}
	return 1
}

// readCSI reads an SGR sequence starting at i, returning its parameters and
// the index just past it.
func readCSI(raw string, i int) (params []int, next int, ok bool) {
	if i+1 >= len(raw) || raw[i+1] != '[' {
		return nil, 0, false
	}

	j := i + 2
	start := j
	for j < len(raw) && (raw[j] == ';' || (raw[j] >= '0' && raw[j] <= '9')) {
		j++
	}
	if j >= len(raw) {
		return nil, 0, false
	}
	final := raw[j]
	body := raw[start:j]
	j++

	if final != 'm' {
		// Cursor moves and the like carry no styling; skip them entirely.
		return []int{}, j, true
	}

	if body == "" {
		return []int{0}, j, true
	}
	for _, p := range strings.Split(body, ";") {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		params = append(params, n)
	}
	return params, j, true
}

func applySGR(s style, params []int) style {
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case p == 0:
			s = style{}
		case p == 1:
			s.bold = true
		case p == 2:
			s.dim = true
		case p == 3:
			s.italic = true
		case p == 4:
			s.under = true
		case p == 7:
			s.inverse = true
		case p == 22:
			s.bold, s.dim = false, false
		case p == 23:
			s.italic = false
		case p == 24:
			s.under = false
		case p == 27:
			s.inverse = false
		case p >= 30 && p <= 37:
			s.fg, s.fgHex = strconv.Itoa(p-30), ""
		case p >= 90 && p <= 97:
			s.fg, s.fgHex = strconv.Itoa(p-90+8), ""
		case p == 39:
			s.fg, s.fgHex = "", ""
		case p >= 40 && p <= 47:
			s.bg, s.bgHex = strconv.Itoa(p-40), ""
		case p >= 100 && p <= 107:
			s.bg, s.bgHex = strconv.Itoa(p-100+8), ""
		case p == 49:
			s.bg, s.bgHex = "", ""
		case p == 38 || p == 48:
			hex, used := extendedColour(params[i:])
			if used == 0 {
				return s
			}
			if p == 38 {
				s.fg, s.fgHex = "", hex
			} else {
				s.bg, s.bgHex = "", hex
			}
			i += used - 1
		}
	}
	return s
}

// extendedColour reads a 38/48 sequence, returning a CSS colour and how many
// parameters it consumed.
func extendedColour(params []int) (string, int) {
	if len(params) < 2 {
		return "", 0
	}

	switch params[1] {
	case 5: // 38;5;n
		if len(params) < 3 {
			return "", 0
		}
		return xterm256(params[2]), 3
	case 2: // 38;2;r;g;b
		if len(params) < 5 {
			return "", 0
		}
		return fmt.Sprintf("#%02x%02x%02x", clamp(params[2]), clamp(params[3]), clamp(params[4])), 5
	}
	return "", 0
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// xterm256 converts a palette index to CSS. The first sixteen are left to the
// page's own palette by name so the reader matches the terminal.
func xterm256(n int) string {
	switch {
	case n < 16:
		return fmt.Sprintf("var(--ansi-%d)", n)
	case n < 232:
		n -= 16
		steps := []int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", steps[n/36%6], steps[n/6%6], steps[n%6])
	case n < 256:
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
	return ""
}
