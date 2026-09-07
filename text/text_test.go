package text

import (
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
)

func TestWrap(t *testing.T) {
	// Courier 12pt：每字符 7.2pt；宽度 36pt 一行只能放 5 个字符
	lines := Wrap(font.Courier, 12, 36, "aaa bbb ccc")
	if len(lines) != 3 {
		t.Fatalf("Wrap = %v", lines)
	}
	for _, l := range []string{"aaa", "bbb", "ccc"} {
		found := false
		for _, got := range lines {
			if got == l {
				found = true
			}
		}
		if !found {
			t.Errorf("missing line %q in %v", l, lines)
		}
	}
}

func TestWrapLongWord(t *testing.T) {
	lines := Wrap(font.Courier, 12, 36, "abcdefghij") // 10 字符需拆成 5+5
	if len(lines) != 2 || lines[0] != "abcde" || lines[1] != "fghij" {
		t.Errorf("Wrap long word = %v", lines)
	}
}

func TestWrapNewlines(t *testing.T) {
	lines := Wrap(font.Helvetica, 12, 1000, "a\nb")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Errorf("Wrap newlines = %v", lines)
	}
}

func TestWrapEmpty(t *testing.T) {
	if lines := Wrap(font.Helvetica, 12, 100, ""); len(lines) != 1 || lines[0] != "" {
		t.Errorf("Wrap empty = %v", lines)
	}
}

func TestOffsetX(t *testing.T) {
	w := font.Courier.TextWidth("abc", 12) // 3*7.2 = 21.6
	if got := OffsetX(font.Courier, 12, 100, "abc", AlignRight); got != 100-w {
		t.Errorf("AlignRight offset = %v", got)
	}
	if got := OffsetX(font.Courier, 12, 100, "abc", AlignCenter); got != (100-w)/2 {
		t.Errorf("AlignCenter offset = %v", got)
	}
	if got := OffsetX(font.Courier, 12, 100, "abc", AlignLeft); got != 0 {
		t.Errorf("AlignLeft offset = %v", got)
	}
}

func TestJustifyWordSpace(t *testing.T) {
	line := "a b c"
	ws := JustifyWordSpace(font.Courier, 12, 100, line)
	lw := font.Courier.TextWidth(line, 12)
	if ws*2+lw != 100 {
		t.Errorf("justify: %v + %v != 100", ws*2, lw)
	}
	if JustifyWordSpace(font.Courier, 12, 100, "nospaces") != 0 {
		t.Error("no spaces should return 0")
	}
}
