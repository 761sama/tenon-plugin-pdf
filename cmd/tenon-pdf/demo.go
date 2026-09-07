package main

import (
	"bytes"
	"flag"
	stdimage "image"
	stdcolor "image/color"
	"image/jpeg"
	"image/png"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/content"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/form"
	"gopkg.761sama.com/tenon-plugin-pdf/image"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// cmdDemo 生成功能演示 PDF。
func cmdDemo(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	out := fs.String("o", "demo.pdf", "输出文件")
	fs.Parse(args)

	doc := pdf.New()
	doc.Info().Title = "tenon-plugin-pdf 功能演示"
	doc.Info().Author = "tenon-pdf"
	doc.Info().Subject = "PDF 组件 / 格式 / 样式 演示"
	doc.Info().Keywords = "pdf,demo,tenon"
	doc.Info().Creator = "tenon-pdf demo"
	doc.Info().CreationDate = time.Now()
	doc.SetPageLayout(pdf.LayoutSinglePage)

	demoTextPage(doc)
	demoGraphicsPage(doc)
	demoImagePage(doc)
	demoFormPage(doc)

	it := doc.Outline().Add("1. 文本与字体", annot.Destination{PageIndex: 0})
	it.Bold = true
	it.Add("标准 14 字体", annot.Destination{PageIndex: 0, Top: 640})
	doc.Outline().Add("2. 图形与样式", annot.Destination{PageIndex: 1})
	doc.Outline().Add("3. 图像与批注", annot.Destination{PageIndex: 2})
	doc.Outline().Add("4. 交互表单", annot.Destination{PageIndex: 3})

	return doc.SaveFile(*out)
}

func demoTextPage(doc *pdf.Document) {
	p := doc.AddPage(page.A4)
	p.SetFillColor(color.Hex(0x1a3a6b))
	p.DrawText(font.HelveticaBold, 22, 50, 790, "tenon-plugin-pdf")
	p.SetFillColor(color.Gray(0.4))
	p.DrawText(font.HelveticaOblique, 11, 50, 772, "Go PDF generation library — feature showcase")
	p.SetStrokeColor(color.Hex(0x1a3a6b)).SetLineWidth(1.5)
	p.Line(50, 762, 545, 762)

	// 标准 14 字体
	p.SetFillColor(color.Black)
	p.DrawText(font.HelveticaBold, 14, 50, 730, "1. Standard 14 Fonts")
	fonts := []*font.Font{
		font.Helvetica, font.HelveticaBold, font.HelveticaOblique, font.HelveticaBoldOblique,
		font.TimesRoman, font.TimesBold, font.TimesItalic, font.TimesBoldItalic,
		font.Courier, font.CourierBold, font.CourierOblique, font.CourierBoldOblique,
		font.Symbol, font.ZapfDingbats,
	}
	y := 706.0
	for _, f := range fonts {
		p.SetFillColor(color.Gray(0.45))
		p.DrawText(font.Courier, 9, 50, y, f.BaseFont)
		p.SetFillColor(color.Black)
		sample := "The quick brown fox jumps over the lazy dog 0123456789"
		if f == font.Symbol {
			sample = "abgde zqyjk abcdefg 0123456789"
		} else if f == font.ZapfDingbats {
			sample = "3456 abcd NOKP qrst"
		}
		p.DrawText(f, 12, 200, y, sample)
		y -= 17
	}

	// 段落排版
	p.SetFillColor(color.Black)
	p.DrawText(font.HelveticaBold, 14, 50, y-14, "1.2 Paragraph (justified)")
	p.TextBox(font.TimesRoman, 10.5, 50, y-32, 300,
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod "+
			"tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, "+
			"quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.",
		text.AlignJustify, 0)

	// 文本样式
	p.DrawText(font.HelveticaBold, 14, 380, y-14, "1.3 Styles")
	p.SetFillColor(color.RGB{R: 0.8, G: 0.1, B: 0.1})
	p.DrawText(font.HelveticaBold, 12, 380, y-34, "Underline + Strike")
	p.Underline(font.HelveticaBold, 12, 380, y-34, "Underline + Strike")
	p.StrikeThrough(font.HelveticaBold, 12, 380, y-34, "Underline + Strike")
	p.Save().SetFillColor(color.White).SetStrokeColor(color.Black).SetLineWidth(0.8)
	p.Content.BeginText().SetFont("F1", 18).SetTextRenderMode(2).
		TextPosition(380, y-60).ShowText([]byte("Outline")).EndText()
	p.Restore()
	p.SetFillColor(color.Black)
	p.DrawText(font.Helvetica, 9, 380, y-80, "char space:")
	p.Content.BeginText().SetFont("F1", 12).CharSpace(2).
		TextPosition(450, y-80).ShowText([]byte("Spaced out")).EndText()

	// 上标/下标
	p.DrawText(font.Helvetica, 12, 380, y-100, "E = mc")
	p.Content.BeginText().SetFont("F1", 12).TextRise(5).
		TextPosition(438, y-100).ShowText([]byte("2")).EndText()
}

func demoGraphicsPage(doc *pdf.Document) {
	p := doc.AddPage(page.A4)
	p.SetFillColor(color.Black)
	p.DrawText(font.HelveticaBold, 14, 50, 790, "2. Graphics & Styles")

	// 基本形状
	p.DrawText(font.Helvetica, 10, 50, 765, "Shapes:")
	p.SetStrokeColor(color.Hex(0x2050a0)).SetLineWidth(2)
	p.StrokeRect(50, 690, 90, 60)
	p.StrokeRoundRect(160, 690, 90, 60, 12)
	p.StrokeCircle(330, 720, 32)
	p.StrokeEllipse(440, 720, 50, 28)
	p.SetFillColor(color.Hex(0x40a060))
	p.FillRoundRect(160, 690, 90, 60, 12)
	p.SetStrokeColor(color.Hex(0x2050a0))
	p.StrokeRoundRect(160, 690, 90, 60, 12)

	// 多边形 + 虚线 + 线帽
	p.SetStrokeColor(color.Black).SetLineWidth(1)
	p.StrokePolygon([]page.Point{{X: 60, Y: 640}, {X: 85, Y: 680}, {X: 110, Y: 640}})
	p.SetDash([]float64{8, 4}, 0).SetLineCap(content.CapRound).SetLineWidth(3)
	p.Line(160, 640, 260, 680)
	p.SetDash([]float64{2, 3}, 0).SetLineWidth(1.5)
	p.Line(160, 660, 260, 640)
	p.SetDash(nil, 0).SetLineCap(content.CapButt).SetLineWidth(1)

	// 贝塞尔曲线
	p.SetStrokeColor(color.Hex(0xc05030)).SetLineWidth(2)
	p.Content.MoveTo(300, 640).CurveTo(340, 700, 420, 600, 460, 680).Stroke()

	// 渐变
	p.DrawText(font.Helvetica, 10, 50, 610, "Gradients (axial / radial):")
	p.FillAxialGradient(50, 520, 200, 70, color.Hex(0x4080e0), color.Hex(0xe04080), true)
	p.FillAxialGradient(270, 520, 200, 70, color.Yellow, color.Hex(0x20a040), false)
	p.FillRadialGradient(530, 555, 50, color.White, color.Hex(0x8040c0))

	// 透明度与混合模式
	p.DrawText(font.Helvetica, 10, 50, 490, "Transparency & blend modes:")
	p.SetFillColor(color.Red)
	p.FillCircle(110, 440, 35)
	p.SetAlpha(0.55, 0.55)
	p.SetFillColor(color.Blue)
	p.FillCircle(145, 440, 35)
	p.SetAlpha(1, 1)
	p.SetBlendMode("Multiply")
	p.SetFillColor(color.Hex(0x00a0a0))
	p.FillCircle(260, 440, 35)
	p.SetFillColor(color.Hex(0xe0a000))
	p.FillCircle(295, 440, 35)
	p.SetBlendMode("Normal")

	// 旋转变换
	p.DrawText(font.Helvetica, 10, 50, 380, "Transforms (rotate 45°):")
	p.Save().Translate(430, 330).RotateCanvas(45)
	p.SetFillColor(color.Hex(0x3070c0))
	p.FillRect(-40, -25, 80, 50)
	p.SetFillColor(color.White)
	p.DrawText(font.HelveticaBold, 10, -30, -4, "Rotated")
	p.Restore()

	// 裁剪
	p.DrawText(font.Helvetica, 10, 50, 300, "Clipping (circle):")
	p.Save()
	p.CirclePath(120, 230, 45)
	p.Content.Clip().EndPath()
	p.FillAxialGradient(60, 180, 120, 100, color.Hex(0xffe080), color.Hex(0xe05030), false)
	p.Restore()
	p.SetStrokeColor(color.Black).SetLineWidth(1.5)
	p.StrokeCircle(120, 230, 45)
}

func demoImagePage(doc *pdf.Document) {
	p := doc.AddPage(page.A4)
	p.SetFillColor(color.Black)
	p.DrawText(font.HelveticaBold, 14, 50, 790, "3. Images & Annotations")

	// 程序生成 PNG（带 alpha）
	rgba := stdimage.NewNRGBA(stdimage.Rect(0, 0, 120, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			rgba.SetNRGBA(x, y, stdcolor.NRGBA{
				R: uint8(x * 2), G: uint8(y*2 + 60), B: 180,
				A: uint8(255 - (x * 255 / 120)),
			})
		}
	}
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, rgba)
	pngImg, _ := image.Decode(&pngBuf)

	// 程序生成 JPEG
	jpg := stdimage.NewRGBA(stdimage.Rect(0, 0, 120, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			jpg.SetRGBA(x, y, stdcolor.RGBA{R: uint8(y * 2), G: uint8(x * 2), B: 60, A: 255})
		}
	}
	var jpgBuf bytes.Buffer
	jpeg.Encode(&jpgBuf, jpg, nil)
	jpgImg, _ := image.Decode(bytes.NewReader(jpgBuf.Bytes()))

	p.DrawText(font.Helvetica, 10, 50, 765, "PNG with alpha (SMask) over colored background:")
	p.SetFillColor(color.Hex(0xf0e0c0))
	p.FillRect(50, 650, 260, 100)
	p.DrawImage(pngImg, 50, 650, 120, 90)
	p.DrawImage(pngImg, 190, 650, 120, 90) // 复用同一图像资源
	p.DrawText(font.Helvetica, 10, 50, 630, "JPEG (DCTDecode, embedded as-is):")
	p.DrawImage(jpgImg, 50, 520, 120, 90)

	// 批注
	p.DrawText(font.Helvetica, 10, 50, 500, "Annotations: link / note / highlight / underline")
	p.SetFillColor(color.Hex(0x1040c0))
	p.DrawText(font.Helvetica, 12, 50, 475, "https://gopkg.761sama.com/tenon-plugin-pdf")
	p.AddURILink([4]float64{50, 472, 275, 490}, "https://gopkg.761sama.com/tenon-plugin-pdf")
	p.AddPageLink([4]float64{50, 445, 200, 465}, annot.Destination{PageIndex: 0})
	p.SetFillColor(color.Black)
	p.DrawText(font.Helvetica, 12, 50, 448, "Jump to page 1")
	p.AddAnnotation(annot.Note{
		Base:  annot.Base{Rect: [4]float64{300, 470, 320, 490}, Contents: "这是一条文本批注（便签）。"},
		Title: "tenon-pdf",
		Icon:  "Comment",
	})
	p.DrawText(font.Helvetica, 12, 50, 420, "This sentence is highlighted and underlined.")
	p.AddAnnotation(annot.Markup{
		Base: annot.Base{Rect: [4]float64{50, 417, 190, 432}, Contents: "高亮"},
		Kind: annot.Highlight,
	})
	p.AddAnnotation(annot.Markup{
		Base:  annot.Base{Rect: [4]float64{192, 417, 340, 432}, Contents: "下划线"},
		Kind:  annot.Underline,
		Color: color.RGB{R: 0, G: 0, B: 1},
	})
}

func demoFormPage(doc *pdf.Document) {
	p := doc.AddPage(page.A4)
	p.SetFillColor(color.Black)
	p.DrawText(font.HelveticaBold, 14, 50, 790, "4. Interactive Form (AcroForm)")

	p.DrawText(font.Helvetica, 11, 50, 755, "Name:")
	doc.AddField(&form.TextField{
		Name: "name", Rect: [4]float64{130, 742, 380, 764}, PageRef: p,
		ToolTip: "请输入姓名",
	})
	p.DrawText(font.Helvetica, 11, 50, 720, "Email:")
	doc.AddField(&form.TextField{
		Name: "email", Rect: [4]float64{130, 707, 380, 729}, PageRef: p,
		Value: "user@example.com",
	})
	p.DrawText(font.Helvetica, 11, 50, 685, "Comments:")
	doc.AddField(&form.TextField{
		Name: "comments", Rect: [4]float64{130, 610, 380, 678}, PageRef: p,
		Multiline: true, ToolTip: "多行输入",
	})
	p.DrawText(font.Helvetica, 11, 50, 575, "Subscribe:")
	doc.AddField(&form.Checkbox{
		Name: "subscribe", Rect: [4]float64{130, 570, 146, 586}, PageRef: p, Checked: true,
	})
	doc.AddField(&form.Checkbox{
		Name: "agree", Rect: [4]float64{180, 570, 196, 586}, PageRef: p,
	})
	p.DrawText(font.Helvetica, 9, 50, 545, "(字段可交互：文本域可输入，复选框可勾选)")
}
