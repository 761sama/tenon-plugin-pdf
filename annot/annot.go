// Package annot 实现 PDF 批注（ISO 32000-1 §12.5）：
// URI 链接、页内跳转链接、文本注释与标记类批注（高亮/下划线/删除线/波浪线）。
package annot

import (
	"math"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Destination 页内跳转目标。
type Destination struct {
	PageIndex int     // 目标页序号（按文档 AddPage 顺序）
	Top       float64 // 目标 y 坐标（页面坐标系，自底向上）；NaN 表示页顶
}

// Resolver 将跳转目标解析为 PDF 目标数组，由文档序列化时提供。
type Resolver func(Destination) object.Object

// Annotation 批注接口。Dict 由文档序列化时调用，res 用于解析跳转目标。
type Annotation interface {
	Dict(res Resolver) *object.Dict
}

// Base 批注公共字段。
type Base struct {
	Rect     [4]float64 // 批注矩形 [x0 y0 x1 y1]
	Contents string     // 弹出备注内容
}

func (b Base) fill(d *object.Dict) {
	d.Set("Type", object.Name("Annot"))
	d.Set("Rect", object.Rect(b.Rect[0], b.Rect[1], b.Rect[2], b.Rect[3]))
	if b.Contents != "" {
		d.Set("Contents", object.TextStr(b.Contents))
	}
}

// Border 批注边框样式。
type Border struct {
	Width float64   // 线宽，默认 1
	Dash  []float64 // 虚线模式，nil 为实线
}

func (bd Border) apply(d *object.Dict) {
	w := bd.Width
	if w == 0 {
		w = 1
	}
	d.Set("Border", object.Array{object.Int(0), object.Int(0), object.Real(w)})
	if len(bd.Dash) > 0 {
		dash := make(object.Array, len(bd.Dash))
		for i, v := range bd.Dash {
			dash[i] = object.Real(v)
		}
		d.Set("BS", object.NewDict().Set("S", object.Name("D")).Set("W", object.Real(w)).Set("D", object.Array{dash}))
	}
}

// LinkURI URI 链接批注。
type LinkURI struct {
	Base
	URI    string
	Border Border     // 零值无边框（仅热区）
	Color  *color.RGB // 边框颜色，可为空
}

func (l LinkURI) Dict(res Resolver) *object.Dict {
	d := object.NewDict()
	l.Base.fill(d)
	d.Set("Subtype", object.Name("Link"))
	d.Set("A", object.NewDict().Set("S", object.Name("URI")).Set("URI", object.Str(l.URI)))
	if l.Border.Width > 0 || len(l.Border.Dash) > 0 {
		l.Border.apply(d)
	} else {
		d.Set("Border", object.Array{object.Int(0), object.Int(0), object.Int(0)})
	}
	if l.Color != nil {
		c := l.Color.Components()
		d.Set("C", object.Array{object.Real(c[0]), object.Real(c[1]), object.Real(c[2])})
	}
	return d
}

// LinkGoTo 页内跳转链接批注。
type LinkGoTo struct {
	Base
	Dest   Destination
	Border Border
}

func (l LinkGoTo) Dict(res Resolver) *object.Dict {
	d := object.NewDict()
	l.Base.fill(d)
	d.Set("Subtype", object.Name("Link"))
	d.Set("Dest", res(l.Dest))
	if l.Border.Width > 0 || len(l.Border.Dash) > 0 {
		l.Border.apply(d)
	} else {
		d.Set("Border", object.Array{object.Int(0), object.Int(0), object.Int(0)})
	}
	return d
}

// Note 文本注释（便签）。
type Note struct {
	Base
	Title string // 弹出窗口标题（作者）
	Icon  string // Comment / Key / Note / Help / NewParagraph / Paragraph / Insert
}

func (n Note) Dict(res Resolver) *object.Dict {
	d := object.NewDict()
	n.Base.fill(d)
	d.Set("Subtype", object.Name("Text"))
	if n.Title != "" {
		d.Set("T", object.TextStr(n.Title))
	}
	icon := n.Icon
	if icon == "" {
		icon = "Note"
	}
	d.Set("Name", object.Name(icon))
	return d
}

// Markup 标记类批注：高亮、下划线、删除线、波浪线。
type Markup struct {
	Base
	Kind  MarkupKind
	Color color.Color // 标记颜色，默认按种类取黄/红
}

// MarkupKind 标记种类。
type MarkupKind int

const (
	Highlight MarkupKind = iota
	Underline
	StrikeOut
	Squiggly
)

var markupSubtype = map[MarkupKind]string{
	Highlight: "Highlight", Underline: "Underline", StrikeOut: "StrikeOut", Squiggly: "Squiggly",
}

var markupColor = map[MarkupKind]color.RGB{
	Highlight: {R: 1, G: 1}, Underline: {G: 0.5}, StrikeOut: {R: 1}, Squiggly: {R: 1, B: 1},
}

func (m Markup) Dict(res Resolver) *object.Dict {
	d := object.NewDict()
	m.Base.fill(d)
	d.Set("Subtype", object.Name(markupSubtype[m.Kind]))
	c := m.Color
	if c == nil {
		mc := markupColor[m.Kind]
		c = mc
	}
	comps := c.Components()
	arr := make(object.Array, len(comps))
	for i, v := range comps {
		arr[i] = object.Real(v)
	}
	d.Set("C", arr)
	// 由矩形推导 QuadPoints（左上、右上、左下、右下）
	x0, y0, x1, y1 := m.Rect[0], m.Rect[1], m.Rect[2], m.Rect[3]
	d.Set("QuadPoints", object.Array{
		object.Real(x0), object.Real(y1), object.Real(x1), object.Real(y1),
		object.Real(x0), object.Real(y0), object.Real(x1), object.Real(y0),
	})
	return d
}

// Raw 原始批注：直接包装一个字典（转义出口）。
type Raw struct{ D *object.Dict }

func (r Raw) Dict(res Resolver) *object.Dict { return r.D }

// TopOrNaN 供序列化使用：NaN 表示页顶。
func TopOrNaN(top float64, pageHeight float64) float64 {
	if math.IsNaN(top) {
		return pageHeight
	}
	return top
}
