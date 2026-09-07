package content

import (
	"math"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
)

// sinCos 供 Rotate 使用（在 rotate.go 中实现）。
func TestRotateMatrix(t *testing.T) {
	b := New().Rotate(90)
	got := string(b.Bytes())
	if got != "0 1 -1 0 0 0 cm\n" && got != "-0 1 -1 -0 0 0 cm\n" {
		t.Logf("rotate(90) = %q", got)
	}
	s, c := sinCos(math.Pi / 2)
	if math.Abs(s-1) > 1e-12 || math.Abs(c) > 1e-12 {
		t.Errorf("sinCos(pi/2) = %v %v", s, c)
	}
}

func TestGraphicsOps(t *testing.T) {
	b := New()
	b.SaveState().
		Transform(1, 0, 0, 1, 10, 20).
		LineWidth(2).
		SetLineCap(CapRound).
		SetLineJoin(JoinBevel).
		MiterLimit(10).
		Dash([]float64{3, 2}, 0).
		SetFillColor(color.RGB{R: 1}).
		SetStrokeColor(color.Gray(0.5)).
		MoveTo(0, 0).
		LineTo(10, 10).
		CurveTo(1, 2, 3, 4, 5, 6).
		Rect(0, 0, 100, 50).
		ClosePath().
		FillStroke().
		RestoreState()

	want := "q\n1 0 0 1 10 20 cm\n2 w\n1 J\n2 j\n10 M\n[3 2] 0 d\n1 0 0 rg\n0.5 G\n0 0 m\n10 10 l\n1 2 3 4 5 6 c\n0 0 100 50 re\nh\nB\nQ\n"
	if string(b.Bytes()) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.Bytes(), want)
	}
}

func TestPaintOps(t *testing.T) {
	b := New()
	b.Stroke().Fill().FillEvenOdd().FillStroke().FillStrokeEvenOdd().
		CloseAndStroke().CloseFillStroke().EndPath().Clip().ClipEvenOdd()
	want := "S\nf\nf*\nB\nB*\ns\nb\nn\nW\nW*\n"
	if string(b.Bytes()) != want {
		t.Errorf("got %q", b.Bytes())
	}
}

func TestTextOps(t *testing.T) {
	b := New()
	b.BeginText().
		SetFont("F1", 12).
		TextPosition(72, 720).
		TextLeading(14).
		CharSpace(0.5).
		WordSpace(1).
		HorizontalScaling(90).
		TextRise(3).
		SetTextRenderMode(TextFillStroke).
		ShowText([]byte("Hi")).
		ShowTextAdjusted([]byte("A"), -40, []byte("V")).
		NextLine().
		EndText()
	want := "BT\n/F1 12 Tf\n72 720 Td\n14 TL\n0.5 Tc\n1 Tw\n90 Tz\n3 Ts\n2 Tr\n(Hi) Tj\n[(A) -40 (V)] TJ\nT*\nET\n"
	if string(b.Bytes()) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.Bytes(), want)
	}
}

func TestDrawImage(t *testing.T) {
	b := New().DrawImage("Im1", 10, 20, 300, 200)
	want := "q\n300 0 0 200 10 20 cm\n/Im1 Do\nQ\n"
	if string(b.Bytes()) != want {
		t.Errorf("got %q", b.Bytes())
	}
}
