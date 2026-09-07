package page

import (
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
)

func TestSizes(t *testing.T) {
	if A4 != (Size{595.28, 841.89}) {
		t.Errorf("A4 = %v", A4)
	}
	if A4.Landscape() != (Size{841.89, 595.28}) {
		t.Errorf("A4 landscape = %v", A4.Landscape())
	}
	if Letter.Portrait() != Letter {
		t.Errorf("Letter portrait = %v", Letter.Portrait())
	}
}

func TestDrawText(t *testing.T) {
	p := New(A4)
	p.DrawText(font.Helvetica, 12, 72, 720, "Hello")
	s := string(p.Content.Bytes())
	if !strings.Contains(s, "/F1 12 Tf") || !strings.Contains(s, "(Hello) Tj") {
		t.Errorf("content: %s", s)
	}
	if len(p.Fonts()) != 1 {
		t.Errorf("fonts = %v", p.Fonts())
	}
	// 同一字体复用资源名
	p.DrawText(font.Helvetica, 10, 72, 700, "Again")
	if len(p.Fonts()) != 1 {
		t.Errorf("font dedup failed: %v", p.Fonts())
	}
}

func TestTextBox(t *testing.T) {
	p := New(A4)
	h := p.TextBox(font.Courier, 12, 72, 720, 100, "aaa bbb ccc ddd eee", 0, 0)
	if h <= 0 {
		t.Errorf("height = %v", h)
	}
	if !strings.Contains(string(p.Content.Bytes()), "Tj") {
		t.Error("no text drawn")
	}
}

func TestShapesAndGradients(t *testing.T) {
	p := New(A4)
	p.SetFillColor(color.Red)
	p.FillRect(10, 10, 100, 50)
	p.StrokeCircle(200, 200, 50)
	p.FillEllipse(300, 300, 40, 20)
	p.StrokePolygon([]Point{{0, 0}, {10, 10}, {20, 0}})
	p.FillAxialGradient(0, 0, 200, 100, color.Red, color.Blue, true)
	p.FillRadialGradient(400, 400, 80, color.Yellow, color.Blue)
	if len(p.Shadings()) != 2 {
		t.Errorf("shadings = %d", len(p.Shadings()))
	}
	// q/Q 平衡
	s := string(p.Content.Bytes())
	if strings.Count(s, "\nq\n")+boolInt(strings.HasPrefix(s, "q\n")) != strings.Count(s, "\nQ\n") {
		t.Errorf("unbalanced q/Q in:\n%s", s)
	}
}

func TestAlphaDedup(t *testing.T) {
	p := New(A4)
	p.SetAlpha(0.5, 0.5)
	p.SetAlpha(0.5, 0.5)
	if len(p.ExtGStates()) != 1 {
		t.Errorf("gstates = %d, want 1 (dedup)", len(p.ExtGStates()))
	}
	p.SetAlpha(0.8, 0.5)
	if len(p.ExtGStates()) != 2 {
		t.Errorf("gstates = %d, want 2", len(p.ExtGStates()))
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
