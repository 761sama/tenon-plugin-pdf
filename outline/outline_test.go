package outline

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

func TestBuild(t *testing.T) {
	var o Outline
	if !o.Empty() {
		t.Fatal("new outline should be empty")
	}

	d0 := annot.Destination{PageIndex: 0}
	d1 := annot.Destination{PageIndex: 1}
	a := o.Add("第一章", d0)
	a.Bold = true
	a.Color = &color.RGB{R: 1}
	a.Add("1.1 节", annot.Destination{PageIndex: 0, Top: 500})
	b := o.Add("第二章", d1)
	b.SetClosed(true)
	b.Add("2.1 节", d1)
	b.Add("2.2 节", d1)

	w := writer.New()
	res := func(d annot.Destination) object.Object {
		return object.Array{object.Ref{Num: 99}, object.Name("Fit")}
	}
	o.Build(w, res)

	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf, object.Ref{Num: 1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"/Type /Outlines",
		"/Count 5",  // 根：2 顶级 + 3 子级
		"/Count -2", // 第二章折叠
		"/First", "/Last", "/Prev", "/Next", "/Parent",
		"/Dest [99 0 R /Fit]",
		"/F 2",       // 第一章加粗
		"/C [1 0 0]", // 红色标题
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in outline output", want)
		}
	}
	// 中文标题用 UTF-16BE 十六进制
	if !strings.Contains(out, "/Title <FEFF") {
		t.Error("title should be UTF-16BE hex string")
	}
}
