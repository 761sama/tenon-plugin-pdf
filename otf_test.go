package pdf_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pdf "gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// 测试用 CFF 字体经环境变量指定（均为 CID 键 CFF 轮廓的思源宋体）：
//   TENONPDF_TEST_HAN_OTF — SourceHanSerifSC-Regular.otf 完整路径
//   TENONPDF_TEST_HAN_OTC — SourceHanSerif-Regular.ttc（OTC 集合）完整路径
// 未设置时相关测试自动跳过。

// otfDemoText 端到端演示文本（中日汉字 + 拉丁 + 数字 + 标点）。
const otfDemoText = "思源宋体 OTF 嵌入测试：天地玄黄，宇宙洪荒。The quick brown fox. 0123456789"

// 用指定字体生成一页演示 PDF
func buildOTFPDF(t *testing.T, f *font.CJKFont, out string) {
	t.Helper()
	doc := pdf.New()
	doc.Info().Title = "OTF 嵌入测试"
	p := doc.AddPage(page.A4)
	p.DrawText(f, 24, 72, 780, "思源宋体 OTF 嵌入测试")
	p.TextBox(f, 12, 72, 740, 460, otfDemoText, text.AlignLeft, 0)
	if err := doc.SaveFile(out); err != nil {
		t.Fatal(err)
	}
}

// 测试 OTF（CFF 轮廓）端到端：生成 PDF 的结构标记正确，
// 外部工具（python + fontTools）可用时交叉验证嵌入 CFF 与文本反查
func TestOTFEndToEnd(t *testing.T) {
	otfPath := os.Getenv("TENONPDF_TEST_HAN_OTF")
	if otfPath == "" {
		t.Skip("未设置 TENONPDF_TEST_HAN_OTF（思源宋体 OTF 路径）")
	}
	f, err := font.LoadCJKFile(otfPath)
	if err != nil {
		t.Skip("思源宋体 OTF 不可用")
	}
	out := filepath.Join(t.TempDir(), "otf.pdf")
	buildOTFPDF(t, f, out)
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"/CIDFontType0", "/FontFile3", "/CIDFontType0C", "/ToUnicode"} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF 缺 %q", want)
		}
	}
	if strings.Contains(s, "/CIDToGIDMap") {
		t.Error("CIDFontType0 不应含 /CIDToGIDMap")
	}
	checkCFFWithPython(t, out, []string{"思源宋体", "天地玄黄", "quick brown fox", "0123456789"})
}

// 测试 OTC 集合（CFF 版 TTC）端到端：按索引取成员生成 PDF 并交叉验证
func TestOTCEndToEnd(t *testing.T) {
	otcPath := os.Getenv("TENONPDF_TEST_HAN_OTC")
	if otcPath == "" {
		t.Skip("未设置 TENONPDF_TEST_HAN_OTC（思源宋体 OTC 路径）")
	}
	f, err := font.LoadCJKCollectionFile(otcPath, 2)
	if err != nil {
		t.Skip("思源宋体 OTC 不可用")
	}
	out := filepath.Join(t.TempDir(), "otc.pdf")
	buildOTFPDF(t, f, out)
	checkCFFWithPython(t, out, []string{"思源宋体", "天地玄黄", "quick brown fox"})
}

// 用 tools/cffcheck.py（fontTools）交叉验证 PDF 中的嵌入 CFF 与文本反查；
// python 或 fontTools 不可用时跳过
func checkCFFWithPython(t *testing.T, pdfPath string, wantText []string) {
	t.Helper()
	py, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python 不可用，跳过 fontTools 交叉验证")
	}
	script := filepath.Join("tools", "cffcheck.py")
	cmd := exec.Command(py, script, pdfPath)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8") // Windows 下默认 GBK，统一为 UTF-8 输出
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
			t.Skip("fontTools 不可用，跳过交叉验证")
		}
		t.Fatalf("cffcheck 校验失败: %v\n%s", err, out)
	}
	for _, want := range wantText {
		if !strings.Contains(string(out), want) {
			t.Errorf("交叉验证提取文本缺少 %q", want)
		}
	}
}
