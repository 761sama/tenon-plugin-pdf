package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

var pageSizes = map[string]page.Size{
	"A4": page.A4, "A5": page.A5, "A3": page.A3,
	"Letter": page.Letter, "Legal": page.Legal, "B5": page.B5,
}

var textFonts = map[string]*font.Font{
	"Helvetica": font.Helvetica, "Times-Roman": font.TimesRoman, "Courier": font.Courier,
}

// cmdText 将纯文本文件转换为 PDF（自动换行、自动分页）。
func cmdText(args []string) error {
	fs := flag.NewFlagSet("text", flag.ExitOnError)
	out := fs.String("o", "out.pdf", "输出文件")
	fontName := fs.String("font", "Helvetica", "正文字体")
	size := fs.Float64("size", 12, "字号")
	sizeName := fs.String("pagesize", "A4", "页面尺寸")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("需要一个输入文本文件")
	}
	f, ok := textFonts[*fontName]
	if !ok {
		return fmt.Errorf("不支持的字体 %q（可选 Helvetica / Times-Roman / Courier）", *fontName)
	}
	sz, ok := pageSizes[*sizeName]
	if !ok {
		return fmt.Errorf("不支持的页面尺寸 %q", *sizeName)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}

	doc := pdf.New()
	doc.Info().Title = fs.Arg(0)
	doc.Info().Creator = "tenon-pdf text"

	const margin = 50.0
	leading := f.LineHeight(*size) * 1.2
	contentW := sz.W - 2*margin
	lines := text.Wrap(f, *size, contentW, string(data))

	p := doc.AddPage(sz)
	y := sz.H - margin
	for _, line := range lines {
		if y-f.Descent(*size) < margin {
			p = doc.AddPage(sz)
			y = sz.H - margin
		}
		if line != "" {
			p.DrawText(f, *size, margin, y-f.Ascent(*size), line)
		}
		y -= leading
	}
	return doc.SaveFile(*out)
}
