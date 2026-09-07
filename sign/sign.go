// Package sign 实现 PDF 数字签名（PKCS#7/CMS，adbe.pkcs7.detached）：
// 签名字段定义、ByteRange 两遍回填签名、签名校验、自签名证书生成。
//
// 用法：先用 pdf.Document.SetSignature 添加签名字段（占位符），
// 序列化后调用 Sign 回填；Verify 用于校验。
package sign

import "strings"

// PlaceholderHexLen 是 /Contents 占位符的十六进制字符数（16384 字节签名空间）。
const PlaceholderHexLen = 32768

// ByteRangePlaceholder 是 /ByteRange 占位符（定宽 10 位数字，回填不改变长度）。
const ByteRangePlaceholder = "[0000000000 0000000000 0000000000 0000000000]"

// ContentsPlaceholder 返回 /Contents 占位符（全零十六进制字符串）。
func ContentsPlaceholder() string {
	return "<" + strings.Repeat("0", PlaceholderHexLen) + ">"
}

// Field 签名字段定义（不可见签名使用零矩形）。
type Field struct {
	Name       string     // 签名字段名，默认 "Signature1"
	Reason     string     // 签名原因
	Location   string     // 签名地点
	Contact    string     // 联系方式
	Rect       [4]float64 // 可见签名的矩形；零值表示不可见签名
	PageIndex  int        // 字段部件所在页
	SignerName string     // 可见签名外观中显示的签署人名称（仅 Rect 非零时生效）
}

// Visible 报告该字段是否为可见签名（指定了非零矩形）。
func (f *Field) Visible() bool {
	return f.Rect != [4]float64{}
}
