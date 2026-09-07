package pdf_test

import (
	"bytes"
	stdimage "image"
	stdcolor "image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/form"
	"gopkg.761sama.com/tenon-plugin-pdf/image"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// testImage 生成带透明通道的测试 PNG。
func testImage(t *testing.T) *image.Image {
	t.Helper()
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 60, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 60; x++ {
			img.SetNRGBA(x, y, stdcolor.NRGBA{R: 200, G: uint8(x * 4), B: 100, A: uint8(128 + x)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	im, err := image.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func testJPEG(t *testing.T) *image.Image {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 80, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, stdcolor.RGBA{R: uint8(x * 3), G: uint8(y * 5), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	im, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return im
}

// buildSampleDoc 构造一个覆盖主要功能的文档。
func buildSampleDoc(t *testing.T) *pdf.Document {
	t.Helper()
	doc := pdf.New()
	doc.Info().Title = "集成测试 Integration Test"
	doc.Info().Author = "tenon"
	doc.Info().Keywords = "pdf,go,test"
	doc.Info().CreationDate = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	doc.SetPageLayout(pdf.LayoutTwoColumnLeft)

	// 第 1 页：文本与字体
	p1 := doc.AddPage(page.A4)
	p1.SetFillColor(color.Black)
	p1.DrawText(font.HelveticaBold, 24, 72, 780, "PDF Integration Test")
	p1.DrawText(font.TimesItalic, 12, 72, 750, "The quick brown fox jumps over the lazy dog")
	p1.DrawText(font.Courier, 12, 72, 730, "0123456789 monospace")
	p1.SetFillColor(color.RGB{R: 0.2, G: 0.3, B: 0.8})
	p1.DrawText(font.Helvetica, 14, 72, 700, "Colored text with umlauts: äöü ÄÖÜ ß €")
	p1.TextBox(font.TimesRoman, 11, 72, 670, 300,
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit. "+
			"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. "+
			"Ut enim ad minim veniam, quis nostrud exercitation ullamco.",
		text.AlignJustify, 0)
	p1.Underline(font.Helvetica, 14, 72, 700, "Colored text with umlauts: äöü ÄÖÜ ß €")

	// 第 2 页：图形、渐变、透明度
	p2 := doc.AddPage(page.A4)
	p2.SetStrokeColor(color.Red).SetLineWidth(2)
	p2.StrokeRect(72, 650, 200, 100)
	p2.SetFillColor(color.CMYKf(0, 0.5, 1, 0))
	p2.FillCircle(150, 550, 40)
	p2.SetDash([]float64{6, 3}, 0)
	p2.Line(72, 500, 300, 500)
	p2.SetDash(nil, 0)
	p2.FillAxialGradient(72, 350, 250, 80, color.Yellow, color.Magenta, true)
	p2.FillRadialGradient(350, 380, 70, color.White, color.Blue)
	p2.SetAlpha(0.5, 0.5)
	p2.SetFillColor(color.Green)
	p2.FillRect(200, 330, 150, 100)
	p2.SetAlpha(1, 1)
	p2.SetBlendMode("Multiply")
	p2.SetFillColor(color.RGB{R: 1, G: 0.5, B: 0})
	p2.FillEllipse(280, 380, 60, 30)
	p2.SetBlendMode("Normal")

	// 第 3 页：图像与链接
	p3 := doc.AddPage(page.A4)
	png := testImage(t)
	jpg := testJPEG(t)
	p3.DrawImage(png, 72, 600, 120, 80)
	p3.DrawImage(png, 220, 600, 60, 40) // 同一图像复用
	p3.DrawImage(jpg, 72, 480, 160, 100)
	p3.DrawText(font.Helvetica, 10, 72, 460, "Visit example.com")
	p3.AddURILink([4]float64{72, 455, 160, 470}, "https://example.com")
	p3.AddPageLink([4]float64{72, 430, 160, 450}, annot.Destination{PageIndex: 0})
	p3.AddAnnotation(annot.Markup{
		Base: annot.Base{Rect: [4]float64{72, 400, 200, 416}, Contents: "高亮批注"},
		Kind: annot.Highlight,
	})

	// 第 4 页：表单
	p4 := doc.AddPage(page.A4)
	p4.DrawText(font.HelveticaBold, 14, 72, 780, "Feedback Form")
	doc.AddField(&form.TextField{
		Name: "name", Rect: [4]float64{120, 720, 400, 745}, PageRef: p4,
		ToolTip: "Your name",
	})
	doc.AddField(&form.TextField{
		Name: "comments", Rect: [4]float64{120, 620, 400, 710}, PageRef: p4,
		Multiline: true, Value: "default text",
	})
	doc.AddField(&form.Checkbox{
		Name: "subscribe", Rect: [4]float64{120, 580, 136, 596}, PageRef: p4, Checked: true,
	})
	p4.DrawText(font.Helvetica, 12, 72, 724, "Name:")
	p4.DrawText(font.Helvetica, 12, 72, 640, "Comments:")
	p4.DrawText(font.Helvetica, 12, 72, 582, "Subscribe:")

	// 大纲
	doc.Outline().Add("Text & Fonts", annot.Destination{PageIndex: 0})
	g := doc.Outline().Add("Graphics", annot.Destination{PageIndex: 1})
	g.Bold = true
	doc.Outline().Add("Images & Links", annot.Destination{PageIndex: 2})
	f := doc.Outline().Add("Form", annot.Destination{PageIndex: 3})
	f.Italic = true

	return doc
}

func TestDocumentRoundTrip(t *testing.T) {
	doc := buildSampleDoc(t)
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.7")) {
		t.Fatal("bad header")
	}
	if !bytes.Contains(data, []byte("%%EOF")) {
		t.Fatal("missing EOF")
	}
	for _, want := range []string{
		"/Type /Catalog", "/Type /Pages", "/Count 4",
		"/Outlines", "/PageMode /UseOutlines", "/PageLayout /TwoColumnLeft",
		"/AcroForm", "/Metadata", "/Font", "/XObject", "/Shading", "/ExtGState",
		"/Annots", "/SMask", "/DCTDecode", "/FlateDecode",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("PDF missing %q", want)
		}
	}

	// 用系统工具验证结构
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo 不可用，跳过外部验证")
	}
	path := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfinfo 失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Pages:") || !strings.Contains(string(out), "Pages:           4") {
		// 宽松匹配：解析 Pages 行
		pagesOK := false
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Pages:") && strings.TrimSpace(strings.TrimPrefix(line, "Pages:")) == "4" {
				pagesOK = true
			}
		}
		if !pagesOK {
			t.Errorf("pdfinfo 页数错误:\n%s", out)
		}
	}

	// 文本提取验证
	if _, err := exec.LookPath("pdftotext"); err == nil {
		txt, err := exec.Command("pdftotext", path, "-").Output()
		if err != nil {
			t.Fatalf("pdftotext 失败: %v", err)
		}
		for _, want := range []string{"PDF Integration Test", "quick brown fox", "monospace", "Lorem ipsum"} {
			if !strings.Contains(string(txt), want) {
				t.Errorf("提取文本缺少 %q", want)
			}
		}
	}
}

func TestEmptyDocument(t *testing.T) {
	doc := pdf.New()
	if _, err := doc.Bytes(); err == nil {
		t.Error("空文档应报错")
	}
}

func TestSaveFile(t *testing.T) {
	doc := pdf.New()
	p := doc.AddPage(page.Letter)
	p.DrawText(font.Helvetica, 12, 72, 700, "save test")
	path := filepath.Join(t.TempDir(), "out.pdf")
	if err := doc.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("file not written: %v", err)
	}
}

func TestUncompressed(t *testing.T) {
	doc := pdf.New()
	doc.SetCompress(false)
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 12, 72, 700, "plain")
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	// 未压缩时内容流明文可见
	if !bytes.Contains(data, []byte("(plain) Tj")) {
		t.Error("uncompressed content should be visible")
	}
}

// TestCJKDocument 验证中文嵌入子集字体：渲染、分页与 ToUnicode 反查。
func TestCJKDocument(t *testing.T) {
	cjk, err := font.LoadCJKFile("cmd/tenon-pdf/assets/NotoSansSC-Subset.ttf")
	if err != nil {
		t.Skip("子集字体资源不可用")
	}
	cjk.SetName("NotoSansSC")

	doc := pdf.New()
	doc.Info().Title = "中文测试"
	p := doc.AddPage(page.A4)
	p.DrawText(cjk, 18, 72, 780, "中文标题：思源黑体子集嵌入")
	p.TextBox(cjk, 11, 72, 750, 450,
		"这是一段中文正文，用于验证自动换行与排版。采购订单包含序号、品名、规格、数量、单价与金额等栏目。",
		text.AlignLeft, 0)

	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/Type0", "/Identity-H", "/ToUnicode", "/CIDFontType2", "/FontFile2"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("PDF missing %q", want)
		}
	}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext 不可用")
	}
	path := filepath.Join(t.TempDir(), "cjk.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	txt, err := exec.Command("pdftotext", path, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	for _, want := range []string{"中文标题", "思源黑体", "采购订单"} {
		if !strings.Contains(string(txt), want) {
			t.Errorf("提取文本缺少 %q", want)
		}
	}
}

// TestLigatureKerningDocument 端到端验证连字与字距：
// 用 DejaVu Sans（带 GSUB liga 与 kern 表）嵌入绘制，
// pdftotext 应经 ToUnicode 把连字还原为 "fi"/"ffi"，且内容流含 TJ 调整数组。
func TestLigatureKerningDocument(t *testing.T) {
	f, err := font.LoadCJKFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skip("系统无 DejaVuSans.ttf")
	}
	f.SetName("DejaVuSans")

	doc := pdf.New()
	doc.SetCompress(false) // 明文内容流，便于直接检查 TJ 操作符
	p := doc.AddPage(page.A4)
	p.DrawText(f, 24, 72, 780, "fi ffi fluent office")
	p.DrawText(f, 24, 72, 740, "AVOID AV")

	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("TJ")) {
		t.Error("内容流缺少 TJ 字距调整数组")
	}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext 不可用")
	}
	path := filepath.Join(t.TempDir(), "liga.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	txt, err := exec.Command("pdftotext", path, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	// 连字应被还原为原文字符序列
	for _, want := range []string{"fi ffi fluent office", "AVOID AV"} {
		if !strings.Contains(string(txt), want) {
			t.Errorf("提取文本缺少 %q，实际:\n%s", want, txt)
		}
	}
	// pdffonts 确认字体已子集嵌入
	if out, err := exec.Command("pdffonts", path).CombinedOutput(); err == nil {
		if !strings.Contains(string(out), "DejaVuSans") {
			t.Errorf("pdffonts 未列出嵌入字体:\n%s", out)
		}
	}
}
