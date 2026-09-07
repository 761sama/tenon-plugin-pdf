package pdf

import (
	"bytes"
	"fmt"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// 可见签名外观（ISO 32000-1 §12.7.5.5：Widget 批注的 /AP /N 正常外观流）。
// 外观是自带资源的 Form XObject：浅色底 + 边框 + 签署信息文本行。
// 文本使用 Helvetica（WinAnsi），非 WinAnsi 可表示的字符替换为 '?'——
// 需要中文外观时请将 SignerName/Reason 等用拉丁字符表达，或后续扩展自定义字体。

// buildSigAppearance 为可见签名字段生成外观流，返回其间接引用。
func buildSigAppearance(w *writer.Writer, s *sign.Field, rect [4]float64) object.Ref {
	width := rect[2] - rect[0]
	height := rect[3] - rect[1]

	// 外观文本行
	signer := s.SignerName
	if signer == "" {
		signer = s.Name
	}
	var lines []string
	if signer != "" {
		lines = append(lines, "Digitally signed by: "+winansiSafe(signer))
	}
	lines = append(lines, "Date: "+time.Now().Format("2006-01-02 15:04:05Z07:00"))
	if s.Reason != "" {
		lines = append(lines, "Reason: "+winansiSafe(s.Reason))
	}
	if s.Location != "" {
		lines = append(lines, "Location: "+winansiSafe(s.Location))
	}

	// 字号：按行数自适应，夹在 [6, 10]
	size := (height - 8) / float64(len(lines)) * 0.8
	if size > 10 {
		size = 10
	}
	if size < 6 {
		size = 6
	}
	leading := height / float64(len(lines))

	helvRef := w.Add(font.Helvetica.Dict())

	var c bytes.Buffer
	// 浅色底 + 边框
	fmt.Fprintf(&c, "q 0.85 0.9 0.98 rg 0 0 %.4g %.4g re f Q\n", width, height)
	fmt.Fprintf(&c, "q 0.2 0.4 0.8 RG 1 w 0.5 0.5 %.4g %.4g re S Q\n", width-1, height-1)
	// 文本行（自上而下）
	c.WriteString("BT\n")
	fmt.Fprintf(&c, "/Helv %.4g Tf %.4g TL\n", size, leading)
	fmt.Fprintf(&c, "1 %.4g Td\n", height-leading*0.72)
	for _, line := range lines {
		fmt.Fprintf(&c, "4 0 Td (%s) Tj\n", pdfEscapeASCII(line))
		fmt.Fprintf(&c, "-4 -%.4g Td\n", leading-0) // 下一行
	}
	c.WriteString("ET\n")

	st := object.NewStream(c.Bytes())
	st.Dict.Set("Type", object.Name("XObject"))
	st.Dict.Set("Subtype", object.Name("Form"))
	st.Dict.Set("FormType", object.Int(1))
	st.Dict.Set("BBox", object.Rect(0, 0, width, height))
	st.Dict.Set("Resources", object.NewDict().Set("Font",
		object.NewDict().Set("Helv", helvRef)))
	return w.Add(st)
}

// winansiSafe 将字符串限制在 WinAnsi 可绘制的 ASCII 子集。
func winansiSafe(s string) string {
	b := []byte(s)
	out := b[:0]
	for _, c := range b {
		if c >= 0x20 && c <= 0x7e {
			out = append(out, c)
		} else {
			out = append(out, '?')
		}
	}
	return string(out)
}

// pdfEscapeASCII 转义字面量字符串中的定界符。
func pdfEscapeASCII(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', ')', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
