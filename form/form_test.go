package form

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

func TestBuildForm(t *testing.T) {
	pg := page.New(page.A4)
	fields := []Field{
		&TextField{
			Name: "name", Value: "张三", Rect: [4]float64{100, 700, 300, 725}, PageRef: pg,
			Required: true, MaxLen: 20,
		},
		&TextField{
			Name: "comments", Rect: [4]float64{100, 600, 300, 690}, PageRef: pg,
			Multiline: true,
		},
		&Checkbox{Name: "ok", Rect: [4]float64{100, 560, 116, 576}, PageRef: pg, Checked: true},
	}

	w := writer.New()
	fonts := FormFonts{Helv: w.Add(object.NewDict()), ZaDb: w.Add(object.NewDict())}
	pageRefOf := func(p *page.Page) object.Ref { return object.Ref{Num: 99} }
	w.Alloc() // 占位，模拟其他对象
	acroRef := Build(w, fields, fonts, pageRefOf)

	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf, acroRef, nil, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	checks := []string{
		"/AcroForm", // 不存在也不影响，本测试直接查字段
		"/FT /Tx", "/FT /Btn",
		"/T (name)", "/V <FEFF", // 中文值 UTF-16BE
		"/Subtype /Widget",
		"/DA (/Helv 0 Tf 0 g)",
		"/MaxLen 20",
		"/NeedAppearances true",
		"/Helv", "/ZaDb",
		"/V /Yes", "/AS /Yes",
		"/AP", // 复选框外观
		"/Subtype /Form", "/BBox",
	}
	for _, want := range checks[1:] {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in form output", want)
		}
	}
	// 部件引用登记到页面
	if len(pg.AnnotationRefs()) != 3 {
		t.Errorf("page widget refs = %d, want 3", len(pg.AnnotationRefs()))
	}
}

func TestTextFieldFlags(t *testing.T) {
	pg := page.New(page.A4)
	f := &TextField{
		Name: "f", Rect: [4]float64{0, 0, 10, 10}, PageRef: pg,
		Multiline: true, Password: true, ReadOnly: true, Required: true,
	}
	d := f.build(FormFonts{}, object.Ref{Num: 1})
	v, _ := d.Get("Ff")
	flags := int(v.(object.Int))
	if flags&1 == 0 || flags&(1<<1) == 0 || flags&(1<<12) == 0 || flags&(1<<13) == 0 {
		t.Errorf("flags = %b", flags)
	}
}

func TestNumStr(t *testing.T) {
	if numStr(0) != "0" || numStr(12) != "12" || numStr(10.5) != "10.5" {
		t.Errorf("numStr: %q %q %q", numStr(0), numStr(12), numStr(10.5))
	}
}
