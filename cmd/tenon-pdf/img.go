package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/image"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
)

// cmdImg 将图片文件合成为 PDF，每张图片占一页（按 72dpi 原始尺寸，居中放置，
// 超出页面时等比缩小）。
func cmdImg(args []string) error {
	fs := flag.NewFlagSet("img", flag.ExitOnError)
	out := fs.String("o", "out.pdf", "输出文件")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("需要至少一个图片文件（JPEG/PNG/GIF）")
	}

	doc := pdf.New()
	doc.Info().Creator = "tenon-pdf img"
	for _, path := range fs.Args() {
		fp, err := os.Open(path)
		if err != nil {
			return err
		}
		im, err := image.Decode(fp)
		fp.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		pw, ph := float64(im.Width), float64(im.Height)
		if pw < 72 {
			pw = 72
		}
		if ph < 72 {
			ph = 72
		}
		p := doc.AddPage(page.Size{W: pw, H: ph})
		p.DrawImageNatural(im, 0, 0)
	}
	return doc.SaveFile(*out)
}
