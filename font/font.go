// Package font 实现 PDF 标准 14 字体（ISO 32000-1 §9.6.2.2）、
// WinAnsiEncoding 文本编码与宽度度量，用于文本排版与字体资源生成。
package font

import (
	"unicode/utf8"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Metrics 字体的度量信息，单位为 1/1000 em。
type Metrics struct {
	Widths    [256]int32 // 按字符码（WinAnsi）索引的字宽，0 表示未定义
	Ascent    int32
	Descent   int32
	CapHeight int32
	LineGap   int32
}

// Font 标准 14 字体之一。
type Font struct {
	BaseFont string  // /BaseFont，如 "Helvetica-Bold"
	metrics  Metrics // 字宽与行高度量
	winansi  bool    // 是否使用 WinAnsiEncoding（Symbol/ZapfDingbats 使用内置编码）
}

// 标准 14 字体。除 Symbol 与 ZapfDingbats 外均使用 WinAnsiEncoding。
var (
	Helvetica            = &Font{BaseFont: "Helvetica", metrics: helveticaMetrics, winansi: true}
	HelveticaBold        = &Font{BaseFont: "Helvetica-Bold", metrics: helveticaBoldMetrics, winansi: true}
	HelveticaOblique     = &Font{BaseFont: "Helvetica-Oblique", metrics: helveticaMetrics, winansi: true}
	HelveticaBoldOblique = &Font{BaseFont: "Helvetica-BoldOblique", metrics: helveticaBoldMetrics, winansi: true}
	TimesRoman           = &Font{BaseFont: "Times-Roman", metrics: timesRomanMetrics, winansi: true}
	TimesBold            = &Font{BaseFont: "Times-Bold", metrics: timesBoldMetrics, winansi: true}
	TimesItalic          = &Font{BaseFont: "Times-Italic", metrics: timesItalicMetrics, winansi: true}
	TimesBoldItalic      = &Font{BaseFont: "Times-BoldItalic", metrics: timesBoldItalicMetrics, winansi: true}
	Courier              = &Font{BaseFont: "Courier", metrics: courierMetrics, winansi: true}
	CourierBold          = &Font{BaseFont: "Courier-Bold", metrics: courierMetrics, winansi: true}
	CourierOblique       = &Font{BaseFont: "Courier-Oblique", metrics: courierMetrics, winansi: true}
	CourierBoldOblique   = &Font{BaseFont: "Courier-BoldOblique", metrics: courierMetrics, winansi: true}
	Symbol               = &Font{BaseFont: "Symbol"}
	ZapfDingbats         = &Font{BaseFont: "ZapfDingbats"}
)

// Standard14 返回全部标准 14 字体。
func Standard14() []*Font {
	return []*Font{
		Helvetica, HelveticaBold, HelveticaOblique, HelveticaBoldOblique,
		TimesRoman, TimesBold, TimesItalic, TimesBoldItalic,
		Courier, CourierBold, CourierOblique, CourierBoldOblique,
		Symbol, ZapfDingbats,
	}
}

// Encode 将 UTF-8 文本编码为该字体编码下的字节序列。
// 无法映射的字符替换为 '?'。
func (f *Font) Encode(s string) []byte {
	if !f.winansi {
		// Symbol / ZapfDingbats：内置编码按字节直写，非 ASCII 无法映射
		out := make([]byte, 0, len(s))
		for _, r := range s {
			if r < 0x100 {
				out = append(out, byte(r))
			} else {
				out = append(out, '?')
			}
		}
		return out
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, encodeWinAnsi(r))
	}
	return out
}

// WidthOf 返回文本在 1/1000 em 单位下的总宽度。
func (f *Font) WidthOf(s string) int {
	if !f.winansi {
		// 内置编码字体：无宽度表时按近似等宽估算
		return utf8.RuneCountInString(s) * 600
	}
	w := 0
	for _, r := range s {
		w += int(f.metrics.Widths[encodeWinAnsi(r)])
	}
	return w
}

// TextWidth 返回文本在给定字号下的宽度（PDF 用户空间单位，磅）。
func (f *Font) TextWidth(s string, size float64) float64 {
	return float64(f.WidthOf(s)) * size / 1000
}

// Ascent 返回指定字号下的上升部高度。
func (f *Font) Ascent(size float64) float64 { return float64(f.metrics.Ascent) * size / 1000 }

// Descent 返回指定字号下的下降部高度（负值）。
func (f *Font) Descent(size float64) float64 { return float64(f.metrics.Descent) * size / 1000 }

// CapHeight 返回指定字号下的大写字母高度。
func (f *Font) CapHeight(size float64) float64 { return float64(f.metrics.CapHeight) * size / 1000 }

// LineHeight 返回指定字号下的建议行高。
func (f *Font) LineHeight(size float64) float64 {
	return float64(f.metrics.Ascent-f.metrics.Descent+f.metrics.LineGap) * size / 1000
}

// Dict 生成字体资源字典（Type1，不嵌入字体文件）。
func (f *Font) Dict() *object.Dict {
	d := object.NewDict()
	d.Set("Type", object.Name("Font"))
	d.Set("Subtype", object.Name("Type1"))
	d.Set("BaseFont", object.Name(f.BaseFont))
	if !f.winansi {
		return d // Symbol / ZapfDingbats 使用字体内置编码
	}
	d.Set("Encoding", object.Name("WinAnsiEncoding"))
	// 附带宽度表，便于查看器在无内置度量时排版
	first, last := 32, 255
	widths := make(object.Array, 0, last-first+1)
	for c := first; c <= last; c++ {
		widths = append(widths, object.Int(f.metrics.Widths[c]))
	}
	d.Set("FirstChar", object.Int(first))
	d.Set("LastChar", object.Int(last))
	d.Set("Widths", widths)
	return d
}
