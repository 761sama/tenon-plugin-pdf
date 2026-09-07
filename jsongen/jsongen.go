// Package jsongen 从 JSON 描述生成 PDF 文档（格式见 doc/json-format.md）。
//
// 安全约定：字体文件路径一律不允许出现在 JSON 中。调用方须在代码层通过
// FontRegistry 注册字体（Register / RegisterFile / RegisterCollection /
// RegisterBuiltin），JSON 的样式仅按 id 引用已注册字体，防止路径注入。
package jsongen

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	pdf "gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// ---------------------------------------------------------------------------
// 字体注册表（代码层）

// FontRegistry 字体注册表：JSON 中样式按 id 引用此处注册的字体。
type FontRegistry struct {
	fonts map[string]font.Resource
}

// NewFontRegistry 创建空的字体注册表。
func NewFontRegistry() *FontRegistry {
	return &FontRegistry{fonts: map[string]font.Resource{}}
}

// Register 注册已加载的字体资源。
func (r *FontRegistry) Register(id string, f font.Resource) error {
	if id == "" || f == nil {
		return fmt.Errorf("jsongen: 注册字体需要非空 id 与字体实例")
	}
	r.fonts[id] = f
	return nil
}

// RegisterFile 注册 TTF 字体文件；.ttc 集合取成员 0（需指定成员请用 RegisterCollection）。
func (r *FontRegistry) RegisterFile(id, path string) error {
	if strings.EqualFold(filepath.Ext(path), ".ttc") {
		return r.RegisterCollection(id, path, 0)
	}
	f, err := font.LoadCJKFile(path)
	if err != nil {
		return fmt.Errorf("jsongen: 加载字体 %q: %w", path, err)
	}
	return r.Register(id, f)
}

// RegisterCollection 注册 TTC 集合的成员字体。
func (r *FontRegistry) RegisterCollection(id, path string, index int) error {
	f, err := font.LoadCJKCollectionFile(path, index)
	if err != nil {
		return fmt.Errorf("jsongen: 加载字体 %q[%d]: %w", path, index, err)
	}
	return r.Register(id, f)
}

// builtinFonts 标准 14 字体（不嵌入，仅 WinAnsi 字符）。
var builtinFonts = map[string]font.Resource{
	"Helvetica":             font.Helvetica,
	"Helvetica-Bold":        font.HelveticaBold,
	"Helvetica-Oblique":     font.HelveticaOblique,
	"Helvetica-BoldOblique": font.HelveticaBoldOblique,
	"Times-Roman":           font.TimesRoman,
	"Times-Bold":            font.TimesBold,
	"Times-Italic":          font.TimesItalic,
	"Times-BoldItalic":      font.TimesBoldItalic,
	"Courier":               font.Courier,
	"Courier-Bold":          font.CourierBold,
	"Courier-Oblique":       font.CourierOblique,
	"Courier-BoldOblique":   font.CourierBoldOblique,
	"Symbol":                font.Symbol,
	"ZapfDingbats":          font.ZapfDingbats,
}

// RegisterBuiltin 注册标准 14 字体（如 "Helvetica"、"Times-Bold"）。
func (r *FontRegistry) RegisterBuiltin(id, name string) error {
	f, ok := builtinFonts[name]
	if !ok {
		return fmt.Errorf("jsongen: 未知内置字体 %q", name)
	}
	return r.Register(id, f)
}

func (r *FontRegistry) lookup(id string) (font.Resource, error) {
	if r == nil {
		return nil, fmt.Errorf("jsongen: 字体注册表为 nil")
	}
	f, ok := r.fonts[id]
	if !ok {
		return nil, fmt.Errorf("jsongen: 字体 %q 未注册（须在代码层注册）", id)
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// JSON 规格

type documentSpec struct {
	Version  int                  `json:"version"`
	Metadata metadataSpec         `json:"metadata"`
	Page     pageSpec             `json:"page"`
	Styles   map[string]styleSpec `json:"styles"`
	Content  []blockSpec          `json:"content"`
}

type metadataSpec struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Subject  string `json:"subject"`
	Keywords string `json:"keywords"`
	Creator  string `json:"creator"`
}

type pageSpec struct {
	Size      json.RawMessage `json:"size"`
	Landscape bool            `json:"landscape"`
	Margins   *marginsSpec    `json:"margins"`
}

type marginsSpec struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

// styleSpec 命名样式；零值字段取默认值。
type styleSpec struct {
	Font          string  `json:"font"`
	Size          float64 `json:"size"`
	Color         string  `json:"color"`
	Align         string  `json:"align"`
	LineHeight    float64 `json:"lineHeight"`
	Indent        float64 `json:"indent"`
	SpaceBefore   float64 `json:"spaceBefore"`
	SpaceAfter    float64 `json:"spaceAfter"`
	Underline     bool    `json:"underline"`
	StrikeThrough bool    `json:"strikeThrough"`
}

// styleOverride 块级 / run 级样式覆盖；指针区分“未设置”。
type styleOverride struct {
	Font          *string  `json:"font"`
	Size          *float64 `json:"size"`
	Color         *string  `json:"color"`
	Align         *string  `json:"align"`
	LineHeight    *float64 `json:"lineHeight"`
	Indent        *float64 `json:"indent"`
	SpaceBefore   *float64 `json:"spaceBefore"`
	SpaceAfter    *float64 `json:"spaceAfter"`
	Underline     *bool    `json:"underline"`
	StrikeThrough *bool    `json:"strikeThrough"`
}

type runSpec struct {
	Text  string `json:"text"`
	Style string `json:"style"`
	styleOverride
}

type columnSpec struct {
	Width float64 `json:"width"`
	Align string  `json:"align"`
}

type headerSpec struct {
	Background string `json:"background"`
	Align      string `json:"align"`
	Style      string `json:"style"`
}

type borderSpec struct {
	Width float64 `json:"width"`
	Color string  `json:"color"`
}

type cellSpec struct {
	Text       string `json:"text"`
	ColSpan    int    `json:"colSpan"`
	RowSpan    int    `json:"rowSpan"`
	Align      string `json:"align"`
	Color      string `json:"color"`
	Background string `json:"background"`
	Style      string `json:"style"`
}

// UnmarshalJSON 支持字符串简写（等价于 {"text": s}）。
func (c *cellSpec) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &c.Text)
	}
	type alias cellSpec
	return json.Unmarshal(data, (*alias)(c))
}

type blockSpec struct {
	Type  string `json:"type"`
	Style string `json:"style"`
	styleOverride
	// paragraph
	Text string    `json:"text"`
	Runs []runSpec `json:"runs"`
	// spacer
	Height float64 `json:"height"`
	// table
	Columns    []columnSpec `json:"columns"`
	HeaderRows int          `json:"headerRows"`
	Header     headerSpec   `json:"header"`
	Border     borderSpec   `json:"border"`
	Padding    float64      `json:"padding"`
	Rows       [][]cellSpec `json:"rows"`
	// columns（多栏）
	Gap      float64       `json:"gap"`
	Widths   []float64     `json:"widths"`
	Children [][]blockSpec `json:"children"`
}

// ---------------------------------------------------------------------------
// 构建入口

// Build 按 JSON 描述构建 PDF 文档。fonts 为代码层注册的字体表（必填）。
func Build(data []byte, fonts *FontRegistry) (*pdf.Document, error) {
	var spec documentSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("jsongen: JSON 解析失败: %w", err)
	}
	if spec.Version != 1 {
		return nil, fmt.Errorf("jsongen: 不支持的版本 %d（当前为 1）", spec.Version)
	}
	if fonts == nil {
		return nil, fmt.Errorf("jsongen: 字体注册表为 nil（字体须在代码层注册）")
	}

	size, err := parsePageSize(spec.Page.Size)
	if err != nil {
		return nil, err
	}
	if spec.Page.Landscape {
		size = size.Landscape()
	}

	e := &engine{
		doc:    pdf.New(),
		fonts:  fonts,
		styles: spec.Styles,
		size:   size,
	}
	e.margins = [4]float64{72, 72, 72, 72} // top, right, bottom, left
	if m := spec.Page.Margins; m != nil {
		e.margins = [4]float64{m.Top, m.Right, m.Bottom, m.Left}
	}

	info := e.doc.Info()
	info.Title = spec.Metadata.Title
	info.Author = spec.Metadata.Author
	info.Subject = spec.Metadata.Subject
	info.Keywords = spec.Metadata.Keywords
	info.Creator = spec.Metadata.Creator

	e.addPage()
	for i := range spec.Content {
		b := &spec.Content[i]
		var err error
		switch b.Type {
		case "paragraph":
			err = e.paragraph(b)
		case "table":
			err = e.table(b)
		case "columns":
			err = e.columns(b)
		case "spacer":
			e.spacer(b.Height)
		case "pageBreak":
			e.addPage()
		default:
			err = fmt.Errorf("jsongen: 未知内容块类型 %q", b.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("jsongen: 内容块 #%d（%s）: %w", i, b.Type, err)
		}
	}
	return e.doc, nil
}

// ---------------------------------------------------------------------------
// 解析辅助

var pageSizes = map[string]page.Size{
	"A3": page.A3, "A4": page.A4, "A5": page.A5,
	"B5": page.B5, "Letter": page.Letter, "Legal": page.Legal,
}

func parsePageSize(raw json.RawMessage) (page.Size, error) {
	if len(raw) == 0 {
		return page.A4, nil
	}
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		if s, ok := pageSizes[name]; ok {
			return s, nil
		}
		return page.Size{}, fmt.Errorf("jsongen: 未知页面尺寸 %q", name)
	}
	var custom struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := json.Unmarshal(raw, &custom); err != nil || custom.Width <= 0 || custom.Height <= 0 {
		return page.Size{}, fmt.Errorf("jsongen: 非法页面尺寸 %s", raw)
	}
	return page.Size{W: custom.Width, H: custom.Height}, nil
}

// parseColor 解析 "#RRGGBB"；空串返回 nil（表示“未设置”）。
func parseColor(s string) (color.Color, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) != 7 || s[0] != '#' {
		return nil, fmt.Errorf("jsongen: 非法颜色 %q（应为 #RRGGBB）", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return nil, fmt.Errorf("jsongen: 非法颜色 %q: %w", s, err)
	}
	return color.Hex(uint32(v)), nil
}

func parseAlign(s string, def text.Alignment) (text.Alignment, error) {
	switch s {
	case "":
		return def, nil
	case "left":
		return text.AlignLeft, nil
	case "center":
		return text.AlignCenter, nil
	case "right":
		return text.AlignRight, nil
	case "justify":
		return text.AlignJustify, nil
	}
	return def, fmt.Errorf("jsongen: 未知对齐方式 %q", s)
}

// sanitizeText 剔除零宽与格式控制字符（ZWNJ/ZWJ/BOM 等），
// 避免字体无字形时渲染为 .notdef 占位框。
func sanitizeText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x200B && r <= 0x200F, r == 0x2060, r == 0xFEFF, r == 0x00AD:
			return -1
		}
		return r
	}, s)
}
