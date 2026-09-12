// Package color 实现 PDF 颜色模型（ISO 32000-1 §8.6）：
// 灰度（DeviceGray）、RGB（DeviceRGB）与 CMYK（DeviceCMYK）。
package color

import "strconv"

// Color 表示一种可直接用于填充或描边的 PDF 颜色。
type Color interface {
	// Operands 返回颜色操作数，如 "1 0 0"。
	Operands() string
	// FillOp 返回设置填充色的操作符（g / rg / k）。
	FillOp() string
	// StrokeOp 返回设置描边色的操作符（G / RG / K）。
	StrokeOp() string
	// Components 返回归一化分量，用于渐变等场景。
	Components() []float64
	// Space 返回设备色彩空间名（DeviceGray / DeviceRGB / DeviceCMYK）。
	Space() string
}

// 将浮点数格式化为 PDF 数值字符串。
func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// 将多个浮点分量以空格连接为操作数字符串。
func join(vals ...float64) string {
	s := ftoa(vals[0])
	for _, v := range vals[1:] {
		s += " " + ftoa(v)
	}
	return s
}

// 将分量值截断到 [0, 1] 区间。
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Gray 灰度颜色，0 为黑，1 为白。
type Gray float64

// 返回灰度操作数。
func (g Gray) Operands() string { return ftoa(clamp(float64(g))) }

// 返回灰度填充操作符 g。
func (g Gray) FillOp() string { return "g" }

// 返回灰度描边操作符 G。
func (g Gray) StrokeOp() string { return "G" }

// 返回归一化灰度分量。
func (g Gray) Components() []float64 { return []float64{clamp(float64(g))} }

// 返回色彩空间名 DeviceGray。
func (g Gray) Space() string { return "DeviceGray" }

// RGB 颜色，分量取值 0–1。
type RGB struct{ R, G, B float64 }

// 以 0–1 浮点分量构造 RGB。
func RGBf(r, g, b float64) RGB { return RGB{clamp(r), clamp(g), clamp(b)} }

// 以 0–255 分量构造 RGB。
func RGB255(r, g, b uint8) RGB {
	return RGB{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

// 以 0xRRGGBB 构造 RGB。
func Hex(v uint32) RGB {
	return RGB255(uint8(v>>16), uint8(v>>8), uint8(v))
}

// 返回 RGB 操作数。
func (c RGB) Operands() string { return join(c.R, c.G, c.B) }

// 返回 RGB 填充操作符 rg。
func (c RGB) FillOp() string { return "rg" }

// 返回 RGB 描边操作符 RG。
func (c RGB) StrokeOp() string { return "RG" }

// 返回归一化 RGB 分量。
func (c RGB) Components() []float64 { return []float64{c.R, c.G, c.B} }

// 返回色彩空间名 DeviceRGB。
func (c RGB) Space() string { return "DeviceRGB" }

// CMYK 颜色，分量取值 0–1。
type CMYK struct{ C, M, Y, K float64 }

// 构造 CMYK 颜色。
func CMYKf(c, m, y, k float64) CMYK { return CMYK{clamp(c), clamp(m), clamp(y), clamp(k)} }

// 返回 CMYK 操作数。
func (c CMYK) Operands() string { return join(c.C, c.M, c.Y, c.K) }

// 返回 CMYK 填充操作符 k。
func (c CMYK) FillOp() string { return "k" }

// 返回 CMYK 描边操作符 K。
func (c CMYK) StrokeOp() string { return "K" }

// 返回归一化 CMYK 分量。
func (c CMYK) Components() []float64 { return []float64{c.C, c.M, c.Y, c.K} }

// 返回色彩空间名 DeviceCMYK。
func (c CMYK) Space() string { return "DeviceCMYK" }

// 常用颜色。
var (
	Black   = Gray(0)
	White   = Gray(1)
	Red     = RGB{1, 0, 0}
	Green   = RGB{0, 1, 0}
	Blue    = RGB{0, 0, 1}
	Yellow  = RGB{1, 1, 0}
	Cyan    = RGB{0, 1, 1}
	Magenta = RGB{1, 0, 1}
)

// ToRGB 将任意颜色转换为 RGB（CMYK 使用简单换算 r=(1-c)(1-k)）。
// 渐变等场景要求两端颜色处于同一色彩空间时使用。
func ToRGB(c Color) RGB {
	switch v := c.(type) {
	case Gray:
		return RGB{float64(v), float64(v), float64(v)}
	case RGB:
		return v
	case CMYK:
		return RGB{(1 - v.C) * (1 - v.K), (1 - v.M) * (1 - v.K), (1 - v.Y) * (1 - v.K)}
	}
	return RGB{}
}
