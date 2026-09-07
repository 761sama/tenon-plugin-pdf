package font

import (
	"strings"
	"testing"
)

func TestWinAnsiEncode(t *testing.T) {
	enc := Helvetica.Encode("Hello, 世界 € — “x”")
	want := []byte("Hello, ")
	want = append(want, '?', '?') // 中文无法映射 → '?'
	want = append(want, ' ', 0x80, ' ', 0x97, ' ', 0x93, 'x', 0x94)
	if string(enc) != string(want) {
		t.Errorf("Encode = %v, want %v", enc, want)
	}
	// 编码结果应可回读
	if got := DecodeWinAnsi(enc); got != "Hello, ?? € — “x”" {
		t.Errorf("DecodeWinAnsi = %q", got)
	}
}

func TestWinAnsiLatin1(t *testing.T) {
	// Latin-1 区间恒等映射
	enc := Helvetica.Encode("éèê à £")
	want := []byte{0xe9, 0xe8, 0xea, ' ', 0xe0, ' ', 0xa3}
	if string(enc) != string(want) {
		t.Errorf("Encode = %v, want %v", enc, want)
	}
}

func TestWidths(t *testing.T) {
	// Helvetica 标准 AFM 值：space=278，M=833
	if got := Helvetica.WidthOf(" "); got != 278 {
		t.Errorf("space width = %d, want 278", got)
	}
	if got := Helvetica.WidthOf("M"); got != 833 {
		t.Errorf("M width = %d, want 833", got)
	}
	// Courier 等宽 600
	if got := Courier.WidthOf("iiWW"); got != 2400 {
		t.Errorf("Courier width = %d, want 2400", got)
	}
	// TextWidth 按字号缩放
	if got := Helvetica.TextWidth("MM", 10); got != 16.66 {
		t.Errorf("TextWidth = %v, want 16.66", got)
	}
	if Helvetica.TextWidth("", 12) != 0 {
		t.Error("empty string should be zero width")
	}
}

func TestMetricsSanity(t *testing.T) {
	for _, f := range Standard14()[:12] {
		if f.metrics.Ascent <= 0 || f.metrics.Descent >= 0 {
			t.Errorf("%s: bad ascent/descent %d/%d", f.BaseFont, f.metrics.Ascent, f.metrics.Descent)
		}
		if f.metrics.Widths['A'] == 0 {
			t.Errorf("%s: missing width for 'A'", f.BaseFont)
		}
	}
}

func TestFontDict(t *testing.T) {
	d := Helvetica.Dict()
	s := string(d.Encode(nil))
	for _, want := range []string{"/Type /Font", "/Subtype /Type1", "/BaseFont /Helvetica", "/Encoding /WinAnsiEncoding", "/Widths", "/FirstChar 32", "/LastChar 255"} {
		if !strings.Contains(s, want) {
			t.Errorf("font dict missing %q", want)
		}
	}
	// Symbol 使用内置编码，不带 Encoding 与 Widths
	sd := string(Symbol.Dict().Encode(nil))
	if strings.Contains(sd, "/Encoding") || strings.Contains(sd, "/Widths") {
		t.Errorf("Symbol dict should omit Encoding/Widths: %s", sd)
	}
}
