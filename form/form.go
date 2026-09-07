// Package form 实现 PDF 交互表单（AcroForm，ISO 32000-1 §12.7）：
// 文本域与复选框。字段与部件批注合并为单一字典。
package form

import (
	"strconv"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/content"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// Field 表单字段接口。
type Field interface {
	// Page 返回字段所在页面。
	Page() *page.Page
	// build 生成字段（兼部件批注）字典。fonts 为 /DR 字体资源引用。
	build(fonts FormFonts, pageRef object.Ref) *object.Dict
	// appearance 生成外观流（仅复选框需要），返回 /AP /N 字典键值。
	appearances(w *writer.Writer, fonts FormFonts) *object.Dict
}

// FormFonts 表单默认资源中的字体。
type FormFonts struct {
	Helv object.Ref // /Helv Helvetica
	ZaDb object.Ref // /ZaDb ZapfDingbats
}

// TextField 文本域。
type TextField struct {
	Name      string
	Value     string
	Rect      [4]float64
	PageRef   *page.Page
	FontSize  float64 // 0 表示自动
	Multiline bool
	Password  bool
	ReadOnly  bool
	Required  bool
	MaxLen    int
	Align     int // /Q：0 左 1 中 2 右
	ToolTip   string
}

// Page 返回字段所在页面。
func (f *TextField) Page() *page.Page { return f.PageRef }

func (f *TextField) build(fonts FormFonts, pageRef object.Ref) *object.Dict {
	d := object.NewDict()
	d.Set("Type", object.Name("Annot"))
	d.Set("Subtype", object.Name("Widget"))
	d.Set("FT", object.Name("Tx"))
	d.Set("T", object.TextStr(f.Name))
	d.Set("Rect", object.Rect(f.Rect[0], f.Rect[1], f.Rect[2], f.Rect[3]))
	d.Set("F", object.Int(4)) // Print 标志
	d.Set("P", pageRef)
	if f.Value != "" {
		d.Set("V", object.TextStr(f.Value))
	}
	d.Set("DA", object.Str("/Helv "+numStr(f.FontSize)+" Tf 0 g"))
	if f.ToolTip != "" {
		d.Set("TU", object.TextStr(f.ToolTip))
	}
	var flags int
	if f.Multiline {
		flags |= 1 << 12
	}
	if f.Password {
		flags |= 1 << 13
	}
	if f.ReadOnly {
		flags |= 1
	}
	if f.Required {
		flags |= 1 << 1
	}
	if flags != 0 {
		d.Set("Ff", object.Int(flags))
	}
	if f.MaxLen > 0 {
		d.Set("MaxLen", object.Int(f.MaxLen))
	}
	if f.Align != 0 {
		d.Set("Q", object.Int(f.Align))
	}
	d.Set("MK", object.NewDict().Set("BC", object.Array{object.Real(0), object.Real(0), object.Real(0)}))
	d.Set("BS", object.NewDict().Set("W", object.Int(1)).Set("S", object.Name("S")))
	return d
}

func (f *TextField) appearances(w *writer.Writer, fonts FormFonts) *object.Dict { return nil }

// Checkbox 复选框。
type Checkbox struct {
	Name     string
	Rect     [4]float64
	PageRef  *page.Page
	Checked  bool
	ReadOnly bool
	ToolTip  string
}

// Page 返回字段所在页面。
func (f *Checkbox) Page() *page.Page { return f.PageRef }

func (f *Checkbox) build(fonts FormFonts, pageRef object.Ref) *object.Dict {
	d := object.NewDict()
	d.Set("Type", object.Name("Annot"))
	d.Set("Subtype", object.Name("Widget"))
	d.Set("FT", object.Name("Btn"))
	d.Set("T", object.TextStr(f.Name))
	d.Set("Rect", object.Rect(f.Rect[0], f.Rect[1], f.Rect[2], f.Rect[3]))
	d.Set("F", object.Int(4))
	d.Set("P", pageRef)
	state := "Off"
	if f.Checked {
		state = "Yes"
	}
	d.Set("V", object.Name(state))
	d.Set("AS", object.Name(state))
	if f.ToolTip != "" {
		d.Set("TU", object.TextStr(f.ToolTip))
	}
	if f.ReadOnly {
		d.Set("Ff", object.Int(1))
	}
	// /MK /CA 为 ZapfDingbats 对勾字符 "4"
	d.Set("MK", object.NewDict().
		Set("CA", object.Str("4")).
		Set("BC", object.Array{object.Real(0), object.Real(0), object.Real(0)}).
		Set("BG", object.Array{object.Real(1), object.Real(1), object.Real(1)}))
	d.Set("BS", object.NewDict().Set("W", object.Int(1)).Set("S", object.Name("S")))
	return d
}

func (f *Checkbox) appearances(w *writer.Writer, fonts FormFonts) *object.Dict {
	x0, y0, x1, y1 := f.Rect[0], f.Rect[1], f.Rect[2], f.Rect[3]
	wd, h := x1-x0, y1-y0

	mkStream := func(checked bool) *object.Stream {
		c := content.New()
		// 边框与白底
		c.SaveState().SetFillColor(color.Gray(1)).Rect(0, 0, wd, h).Fill().RestoreState()
		c.SaveState().SetStrokeColor(color.Gray(0)).LineWidth(1).Rect(0.5, 0.5, wd-1, h-1).Stroke().RestoreState()
		if checked {
			// ZapfDingbats "4" = ✔
			c.BeginText().SetFont("ZaDb", minF(wd, h)*0.8).
				TextPosition(wd*0.18, h*0.15).
				ShowText([]byte("4")).EndText()
		}
		st := object.NewStream(c.Bytes())
		st.Dict.Set("Type", object.Name("XObject"))
		st.Dict.Set("Subtype", object.Name("Form"))
		st.Dict.Set("BBox", object.Rect(0, 0, wd, h))
		res := object.NewDict().Set("Font", object.NewDict().Set("ZaDb", fonts.ZaDb))
		st.Dict.Set("Resources", res)
		return st
	}

	ap := object.NewDict()
	ap.Set("Yes", w.Add(mkStream(true)))
	ap.Set("Off", w.Add(mkStream(false)))
	return ap
}

// --- 序列化入口 ---

// Build 构建 AcroForm 字典并将各字段部件批注引用登记到所在页面。
// extra 为额外的字段引用（如数字签名字段）。返回 AcroForm 字典引用。
func Build(w *writer.Writer, fields []Field, fonts FormFonts, pageRefOf func(*page.Page) object.Ref, extra ...object.Ref) object.Ref {
	fieldRefs := make(object.Array, 0, len(fields)+len(extra))
	for _, f := range fields {
		pageRef := pageRefOf(f.Page())
		d := f.build(fonts, pageRef)
		if ap := f.appearances(w, fonts); ap != nil {
			d.Set("AP", object.NewDict().Set("N", ap))
		}
		ref := w.Add(d)
		f.Page().AddAnnotationRef(ref)
		fieldRefs = append(fieldRefs, ref)
	}
	for _, r := range extra {
		fieldRefs = append(fieldRefs, r)
	}

	dr := object.NewDict().Set("Font", object.NewDict().
		Set("Helv", fonts.Helv).
		Set("ZaDb", fonts.ZaDb))
	acro := object.NewDict()
	acro.Set("Fields", fieldRefs)
	acro.Set("DR", dr)
	acro.Set("DA", object.Str("/Helv 0 Tf 0 g"))
	acro.Set("NeedAppearances", object.Bool(true))
	return w.Add(acro)
}

// --- 内部工具 ---

func numStr(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
