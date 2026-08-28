package ansi

import "testing"

func TestParsePlainText(t *testing.T) {
	lines := Parse("hello\nworld")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0][0].Text != "hello" || lines[0][0].Class != "" {
		t.Errorf("plain text should carry no styling, got %+v", lines[0][0])
	}
}

func TestParseBasicColours(t *testing.T) {
	lines := Parse("\x1b[1;32m✓ PASS\x1b[0m auth.spec.ts")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}

	spans := lines[0]
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if spans[0].Text != "✓ PASS" {
		t.Errorf("span text = %q, want %q", spans[0].Text, "✓ PASS")
	}
	if spans[0].Class != "f2 bo" {
		t.Errorf("span class = %q, want %q", spans[0].Class, "f2 bo")
	}
	if spans[1].Class != "" {
		t.Errorf("reset should clear styling, got %q", spans[1].Class)
	}
}

// Multi-byte characters must not be split, or the output is mojibake.
func TestParseKeepsRunesIntact(t *testing.T) {
	lines := Parse("\x1b[31m这是中文\x1b[39m")
	if got := lines[0][0].Text; got != "这是中文" {
		t.Errorf("text = %q, want 这是中文", got)
	}
}

func TestParseExtendedColours(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		style string
	}{
		{"true colour", "\x1b[38;2;255;176;58mx", "color:#ffb03a"},
		{"256 cube", "\x1b[38;5;208mx", "color:#ff8700"},
		{"256 greyscale", "\x1b[38;5;240mx", "color:#585858"},
		{"256 low maps to palette", "\x1b[38;5;4mx", "color:var(--ansi-4)"},
	}

	for _, tc := range cases {
		spans := Parse(tc.in)[0]
		if spans[0].Style != tc.style {
			t.Errorf("%s: style = %q, want %q", tc.name, spans[0].Style, tc.style)
		}
	}
}

// Cursor movement and other non-SGR sequences carry no styling and must not
// leak into the text.
func TestParseDropsNonSGRSequences(t *testing.T) {
	spans := Parse("\x1b[2J\x1b[Hclean")[0]
	joined := ""
	for _, s := range spans {
		joined += s.Text
	}
	if joined != "clean" {
		t.Errorf("text = %q, want %q", joined, "clean")
	}
}

func TestParseBrightColours(t *testing.T) {
	spans := Parse("\x1b[91mbright red")[0]
	if spans[0].Class != "f9" {
		t.Errorf("class = %q, want f9", spans[0].Class)
	}
}

// A blank line must marshal as an empty list, not null: consumers map over the
// spans, and null stops the render dead.
func TestParseBlankLineIsNotNil(t *testing.T) {
	lines := Parse("a\n\nb")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[1] == nil {
		t.Error("blank line must be an empty slice, not nil")
	}
}
