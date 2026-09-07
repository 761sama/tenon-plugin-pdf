package font

import (
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// Resource 是可作为页面字体资源使用的字体。
// 标准 14 字体（*Font）与嵌入子集字体（*CJKFont）均实现该接口。
type Resource interface {
	// Encode 将 UTF-8 文本编码为该字体字符编码下的字节序列。
	Encode(s string) []byte
	// WidthOf 返回文本在 1/1000 em 单位下的总宽度。
	WidthOf(s string) int
	// TextWidth 返回文本在给定字号下的宽度（磅）。
	TextWidth(s string, size float64) float64
	// Ascent 返回指定字号下的上升部高度。
	Ascent(size float64) float64
	// Descent 返回指定字号下的下降部高度（负值）。
	Descent(size float64) float64
	// CapHeight 返回指定字号下的大写字母高度。
	CapHeight(size float64) float64
	// LineHeight 返回指定字号下的建议行高。
	LineHeight(size float64) float64
	// BuildDict 在文档写入器上构建字体资源字典（嵌入字体此时完成子集化）。
	BuildDict(w *writer.Writer) *object.Dict
}

// Kerned 可选接口：支持字距调整的嵌入字体（*CJKFont 在源字体带 kern 表时）。
// page 层绘制时检测到该接口，改用 TJ 数组输出以应用字距。
type Kerned interface {
	// EncodeKerned 将文本编码为 TJ 数组段（[]byte 文本段与 float64
	// 千分 em 调整值交替）。返回 nil 表示该文本无字距调整，
	// 调用方应回退到 Encode + Tj。
	EncodeKerned(s string) []any
}

// BuildDict 实现 Resource：标准 14 字体无需嵌入，直接返回字典。
func (f *Font) BuildDict(w *writer.Writer) *object.Dict { return f.Dict() }
