package pdf_test

import (
	"fmt"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// 最小示例：创建文档、绘制文本并保存。
func Example() {
	doc := pdf.New()
	doc.Info().Title = "Hello"
	p := doc.AddPage(page.A4)
	p.DrawText(font.HelveticaBold, 24, 72, 760, "Hello PDF")
	p.SetFillColor(color.RGB{R: 0.2, G: 0.4, B: 0.8})
	p.TextBox(font.TimesRoman, 11, 72, 730, 450,
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		text.AlignJustify, 0)
	doc.Outline().Add("首页", annot.Destination{PageIndex: 0})
	data, _ := doc.Bytes() // 或 doc.SaveFile("hello.pdf")
	fmt.Println(len(data) > 0)
	// Output: true
}
