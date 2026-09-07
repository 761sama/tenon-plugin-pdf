package annot

import (
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

var testResolver Resolver = func(d Destination) object.Object {
	return object.Array{object.Ref{Num: 5}, object.Name("XYZ"), object.Null, object.Real(700), object.Null}
}

func TestLinkURI(t *testing.T) {
	l := LinkURI{
		Base: Base{Rect: [4]float64{10, 10, 100, 30}, Contents: "go"},
		URI:  "https://example.com",
	}
	s := string(l.Dict(testResolver).Encode(nil))
	for _, want := range []string{"/Subtype /Link", "/URI (https://example.com)", "/Rect [10 10 100 30]", "/Border [0 0 0]"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestLinkGoTo(t *testing.T) {
	l := LinkGoTo{
		Base: Base{Rect: [4]float64{0, 0, 50, 20}},
		Dest: Destination{PageIndex: 1},
	}
	s := string(l.Dict(testResolver).Encode(nil))
	if !strings.Contains(s, "/Dest [5 0 R /XYZ null 700 null]") {
		t.Errorf("dest wrong: %s", s)
	}
}

func TestNote(t *testing.T) {
	n := Note{Base: Base{Rect: [4]float64{0, 0, 20, 20}, Contents: "备注"}, Title: "审阅者"}
	s := string(n.Dict(testResolver).Encode(nil))
	if !strings.Contains(s, "/Subtype /Text") || !strings.Contains(s, "/Name /Note") {
		t.Errorf("note: %s", s)
	}
	// 中文应以 UTF-16BE 十六进制编码
	if !strings.Contains(s, "<FEFF") {
		t.Errorf("contents should be UTF-16BE: %s", s)
	}
}

func TestMarkup(t *testing.T) {
	m := Markup{Base: Base{Rect: [4]float64{10, 20, 110, 32}}, Kind: Highlight}
	s := string(m.Dict(testResolver).Encode(nil))
	for _, want := range []string{"/Subtype /Highlight", "/C [1 1 0]", "/QuadPoints [10 32 110 32 10 20 110 20]"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	m2 := Markup{Base: Base{Rect: [4]float64{0, 0, 1, 1}}, Kind: Underline, Color: color.Blue}
	s2 := string(m2.Dict(testResolver).Encode(nil))
	if !strings.Contains(s2, "/C [0 0 1]") {
		t.Errorf("custom color: %s", s2)
	}
}
