// Package content 构建 PDF 内容流（ISO 32000-1 §7.8、§8、§9）：
// 图形状态、路径构造与绘制、裁剪、坐标变换、文本操作与 XObject 调用。
//
// Builder 仅负责生成操作符序列，字体编码与资源登记由上层（page 包）完成。
package content

import (
	"strconv"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Builder 内容流构建器。
type Builder struct {
	buf []byte
}

// New 创建构建器。
func New() *Builder { return &Builder{} }

// Bytes 返回已生成的内容流字节。
func (b *Builder) Bytes() []byte { return b.buf }

// Len 返回已生成内容的长度。
func (b *Builder) Len() int { return len(b.buf) }

// Reset 清空构建器。
func (b *Builder) Reset() { b.buf = b.buf[:0] }

func (b *Builder) num(f float64) {
	b.buf = strconv.AppendFloat(b.buf, f, 'f', -1, 64)
}

func (b *Builder) op(s string) *Builder {
	// 仅在需要与前一个操作数分隔时补空格
	if n := len(b.buf); n > 0 {
		if c := b.buf[n-1]; c != '\n' && c != ' ' {
			b.buf = append(b.buf, ' ')
		}
	}
	b.buf = append(b.buf, s...)
	b.buf = append(b.buf, '\n')
	return b
}

// Raw 追加原始操作数与操作符行（转义出口）。
func (b *Builder) Raw(s string) {
	b.buf = append(b.buf, s...)
	b.buf = append(b.buf, '\n')
}

// --- 图形状态（§8.4） ---

// SaveState 保存图形状态（q）。
func (b *Builder) SaveState() *Builder { b.op("q"); return b }

// RestoreState 恢复图形状态（Q）。
func (b *Builder) RestoreState() *Builder { b.op("Q"); return b }

// Transform 左乘变换矩阵（cm）。
func (b *Builder) Transform(a, bb, c, d, e, f float64) *Builder {
	b.num(a)
	b.buf = append(b.buf, ' ')
	b.num(bb)
	b.buf = append(b.buf, ' ')
	b.num(c)
	b.buf = append(b.buf, ' ')
	b.num(d)
	b.buf = append(b.buf, ' ')
	b.num(e)
	b.buf = append(b.buf, ' ')
	b.num(f)
	return b.op("cm")
}

// Translate 平移坐标系。
func (b *Builder) Translate(x, y float64) *Builder { return b.Transform(1, 0, 0, 1, x, y) }

// Scale 缩放坐标系。
func (b *Builder) Scale(sx, sy float64) *Builder { return b.Transform(sx, 0, 0, sy, 0, 0) }

// Rotate 绕原点旋转坐标系（角度制）。
func (b *Builder) Rotate(deg float64) *Builder {
	r := deg * 0.017453292519943295
	sin, cos := sinCos(r)
	return b.Transform(cos, sin, -sin, cos, 0, 0)
}

// LineWidth 设置线宽（w）。
func (b *Builder) LineWidth(w float64) *Builder { b.num(w); return b.op("w") }

// LineCap 线帽样式。
type LineCap int

const (
	CapButt   LineCap = 0 // 平头
	CapRound  LineCap = 1 // 圆头
	CapSquare LineCap = 2 // 方头
)

// SetLineCap 设置线帽（J）。
func (b *Builder) SetLineCap(c LineCap) *Builder { b.num(float64(c)); return b.op("J") }

// LineJoin 连接样式。
type LineJoin int

const (
	JoinMiter LineJoin = 0 // 尖接
	JoinRound LineJoin = 1 // 圆接
	JoinBevel LineJoin = 2 // 斜接
)

// SetLineJoin 设置连接样式（j）。
func (b *Builder) SetLineJoin(j LineJoin) *Builder { b.num(float64(j)); return b.op("j") }

// MiterLimit 设置尖接限制（M）。
func (b *Builder) MiterLimit(m float64) *Builder { b.num(m); return b.op("M") }

// Dash 设置虚线样式（d）：pattern 为虚实长度序列，phase 为相位。
// 空 pattern 恢复实线。
func (b *Builder) Dash(pattern []float64, phase float64) *Builder {
	b.buf = append(b.buf, '[')
	for i, v := range pattern {
		if i > 0 {
			b.buf = append(b.buf, ' ')
		}
		b.num(v)
	}
	b.buf = append(b.buf, ']', ' ')
	b.num(phase)
	return b.op("d")
}

// SetExtGState 应用扩展图形状态资源（gs），如透明度。
func (b *Builder) SetExtGState(name string) *Builder {
	b.buf = object.Name(name).Encode(b.buf)
	return b.op("gs")
}

// --- 颜色（§8.6） ---

// SetFillColor 设置填充颜色（g / rg / k）。
func (b *Builder) SetFillColor(c color.Color) *Builder {
	b.buf = append(b.buf, c.Operands()...)
	return b.op(c.FillOp())
}

// SetStrokeColor 设置描边颜色（G / RG / K）。
func (b *Builder) SetStrokeColor(c color.Color) *Builder {
	b.buf = append(b.buf, c.Operands()...)
	return b.op(c.StrokeOp())
}

// --- 路径构造（§8.5.2） ---

// MoveTo 移动当前点（m）。
func (b *Builder) MoveTo(x, y float64) *Builder {
	b.num(x)
	b.buf = append(b.buf, ' ')
	b.num(y)
	return b.op("m")
}

// LineTo 连线（l）。
func (b *Builder) LineTo(x, y float64) *Builder {
	b.num(x)
	b.buf = append(b.buf, ' ')
	b.num(y)
	return b.op("l")
}

// CurveTo 三次贝塞尔曲线（c）：两个控制点 + 终点。
func (b *Builder) CurveTo(x1, y1, x2, y2, x3, y3 float64) *Builder {
	b.num(x1)
	b.buf = append(b.buf, ' ')
	b.num(y1)
	b.buf = append(b.buf, ' ')
	b.num(x2)
	b.buf = append(b.buf, ' ')
	b.num(y2)
	b.buf = append(b.buf, ' ')
	b.num(x3)
	b.buf = append(b.buf, ' ')
	b.num(y3)
	return b.op("c")
}

// CurveToV 首控制点与当前点重合的贝塞尔曲线（v）。
func (b *Builder) CurveToV(x2, y2, x3, y3 float64) *Builder {
	b.num(x2)
	b.buf = append(b.buf, ' ')
	b.num(y2)
	b.buf = append(b.buf, ' ')
	b.num(x3)
	b.buf = append(b.buf, ' ')
	b.num(y3)
	return b.op("v")
}

// CurveToY 末控制点与终点重合的贝塞尔曲线（y）。
func (b *Builder) CurveToY(x1, y1, x3, y3 float64) *Builder {
	b.num(x1)
	b.buf = append(b.buf, ' ')
	b.num(y1)
	b.buf = append(b.buf, ' ')
	b.num(x3)
	b.buf = append(b.buf, ' ')
	b.num(y3)
	return b.op("y")
}

// Rect 矩形路径（re）。
func (b *Builder) Rect(x, y, w, h float64) *Builder {
	b.num(x)
	b.buf = append(b.buf, ' ')
	b.num(y)
	b.buf = append(b.buf, ' ')
	b.num(w)
	b.buf = append(b.buf, ' ')
	b.num(h)
	return b.op("re")
}

// ClosePath 闭合路径（h）。
func (b *Builder) ClosePath() *Builder { b.op("h"); return b }

// --- 路径绘制（§8.5.3） ---

// Stroke 描边（S）。
func (b *Builder) Stroke() *Builder { b.op("S"); return b }

// CloseAndStroke 闭合并描边（s）。
func (b *Builder) CloseAndStroke() *Builder { b.op("s"); return b }

// Fill 非零环绕填充（f）。
func (b *Builder) Fill() *Builder { b.op("f"); return b }

// FillEvenOdd 奇偶规则填充（f*）。
func (b *Builder) FillEvenOdd() *Builder { b.op("f*"); return b }

// FillStroke 填充并描边（B）。
func (b *Builder) FillStroke() *Builder { b.op("B"); return b }

// FillStrokeEvenOdd 奇偶填充并描边（B*）。
func (b *Builder) FillStrokeEvenOdd() *Builder { b.op("B*"); return b }

// CloseFillStroke 闭合、填充并描边（b）。
func (b *Builder) CloseFillStroke() *Builder { b.op("b"); return b }

// EndPath 结束路径但不绘制（n），用于裁剪后清除路径。
func (b *Builder) EndPath() *Builder { b.op("n"); return b }

// Clip 以当前路径（非零规则）裁剪（W），路径保留需配合后续操作符。
func (b *Builder) Clip() *Builder { b.op("W"); return b }

// ClipEvenOdd 奇偶规则裁剪（W*）。
func (b *Builder) ClipEvenOdd() *Builder { b.op("W*"); return b }

// PaintShading 以渐变资源填充当前裁剪区域（sh）。
func (b *Builder) PaintShading(name string) *Builder {
	b.buf = object.Name(name).Encode(b.buf)
	return b.op("sh")
}

// --- XObject（§8.8） ---

// DrawXObject 调用具名 XObject（Do），通常为图像或表单。
func (b *Builder) DrawXObject(name string) *Builder {
	b.buf = object.Name(name).Encode(b.buf)
	return b.op("Do")
}

// DrawImage 在指定位置以指定尺寸绘制图像 XObject（自动包裹 q/cm/Do/Q）。
func (b *Builder) DrawImage(name string, x, y, w, h float64) *Builder {
	return b.SaveState().Transform(w, 0, 0, h, x, y).DrawXObject(name).RestoreState()
}

// --- 文本（§9.4） ---

// BeginText 开始文本对象（BT）。
func (b *Builder) BeginText() *Builder { b.op("BT"); return b }

// EndText 结束文本对象（ET）。
func (b *Builder) EndText() *Builder { b.op("ET"); return b }

// SetFont 设置字体资源与字号（Tf）。
func (b *Builder) SetFont(resName string, size float64) *Builder {
	b.buf = object.Name(resName).Encode(b.buf)
	b.buf = append(b.buf, ' ')
	b.num(size)
	return b.op("Tf")
}

// TextPosition 移动文本位置（Td）。
func (b *Builder) TextPosition(tx, ty float64) *Builder {
	b.num(tx)
	b.buf = append(b.buf, ' ')
	b.num(ty)
	return b.op("Td")
}

// TextMatrix 设置文本矩阵（Tm）。
func (b *Builder) TextMatrix(a, bb, c, d, e, f float64) *Builder {
	b.num(a)
	b.buf = append(b.buf, ' ')
	b.num(bb)
	b.buf = append(b.buf, ' ')
	b.num(c)
	b.buf = append(b.buf, ' ')
	b.num(d)
	b.buf = append(b.buf, ' ')
	b.num(e)
	b.buf = append(b.buf, ' ')
	b.num(f)
	return b.op("Tm")
}

// TextLeading 设置行距（TL）。
func (b *Builder) TextLeading(leading float64) *Builder { b.num(leading); return b.op("TL") }

// NextLine 换行（T*）。
func (b *Builder) NextLine() *Builder { b.op("T*"); return b }

// CharSpace 字符间距（Tc）。
func (b *Builder) CharSpace(v float64) *Builder { b.num(v); return b.op("Tc") }

// WordSpace 单词间距（Tw）。
func (b *Builder) WordSpace(v float64) *Builder { b.num(v); return b.op("Tw") }

// HorizontalScaling 水平缩放百分比（Tz）。
func (b *Builder) HorizontalScaling(v float64) *Builder { b.num(v); return b.op("Tz") }

// TextRise 文本抬升（Ts），用于上下标。
func (b *Builder) TextRise(v float64) *Builder { b.num(v); return b.op("Ts") }

// TextRenderMode 文本渲染模式（Tr）。
type TextRenderMode int

const (
	TextFill           TextRenderMode = 0 // 填充（默认）
	TextStroke         TextRenderMode = 1 // 描边（空心字）
	TextFillStroke     TextRenderMode = 2 // 填充并描边
	TextInvisible      TextRenderMode = 3 // 不可见
	TextFillClip       TextRenderMode = 4 // 填充并加入裁剪路径
	TextStrokeClip     TextRenderMode = 5 // 描边并加入裁剪路径
	TextFillStrokeClip TextRenderMode = 6
	TextClip           TextRenderMode = 7 // 仅裁剪
)

// SetTextRenderMode 设置文本渲染模式（Tr）。
func (b *Builder) SetTextRenderMode(m TextRenderMode) *Builder {
	b.num(float64(m))
	return b.op("Tr")
}

// ShowText 显示已编码文本（Tj）。字节须为字体编码后的内容。
func (b *Builder) ShowText(encoded []byte) *Builder {
	b.buf = object.String(encoded).Encode(b.buf)
	return b.op("Tj")
}

// ShowTextAdjusted 以 TJ 数组显示文本，segments 为 []byte（文本）
// 或 float64（千分em 调整量，负值拉宽间距）。
func (b *Builder) ShowTextAdjusted(segments ...any) *Builder {
	b.buf = append(b.buf, '[')
	for i, s := range segments {
		if i > 0 {
			b.buf = append(b.buf, ' ')
		}
		switch v := s.(type) {
		case []byte:
			b.buf = object.String(v).Encode(b.buf)
		case float64:
			b.num(v)
		case int:
			b.num(float64(v))
		}
	}
	b.buf = append(b.buf, ']')
	return b.op("TJ")
}

// --- 兼容节（§7.8.2 可选） ---

// BeginMarkedContent 开始标记内容（BMC）。
func (b *Builder) BeginMarkedContent(tag string) *Builder {
	b.buf = object.Name(tag).Encode(b.buf)
	return b.op("BMC")
}

// EndMarkedContent 结束标记内容（EMC）。
func (b *Builder) EndMarkedContent() *Builder { b.op("EMC"); return b }
