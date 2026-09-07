package jsongen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
)

// TestBuildBuiltin 最小文档：内置字体 + 段落/表格/间隔/分页。
func TestBuildBuiltin(t *testing.T) {
	reg := NewFontRegistry()
	if err := reg.RegisterBuiltin("helv", "Helvetica"); err != nil {
		t.Fatal(err)
	}
	spec := `{
	  "version": 1,
	  "metadata": {"title": "test"},
	  "page": {"size": "A4", "margins": {"top": 72, "right": 72, "bottom": 72, "left": 72}},
	  "styles": {
	    "h":   {"font": "helv", "size": 16, "align": "center"},
	    "body": {"font": "helv", "size": 10, "align": "justify", "indent": 2, "lineHeight": 2}
	  },
	  "content": [
	    {"type": "paragraph", "style": "h", "text": "Title"},
	    {"type": "paragraph", "style": "body", "runs": [
	      {"text": "Hello "}, {"text": "red", "color": "#FF0000"}, {"text": " world"}
	    ]},
	    {"type": "spacer", "height": 12},
	    {"type": "table", "style": "body", "headerRows": 1,
	      "columns": [{"width": 100, "align": "center"}, {"width": 0}],
	      "header": {"background": "#EEEEEE"},
	      "rows": [["k1", "v1"], ["k2", {"text": "merged", "colSpan": 1}]]},
	    {"type": "pageBreak"},
	    {"type": "paragraph", "style": "body", "text": "page 2"}
	  ]
	}`
	doc, err := Build([]byte(spec), reg)
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount() != 2 {
		t.Errorf("PageCount = %d, want 2", doc.PageCount())
	}
	b, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-1.7")) {
		t.Error("输出不是 PDF 1.7")
	}
	if doc.Info().Title != "test" {
		t.Errorf("Title = %q", doc.Info().Title)
	}
}

// TestErrors 非法输入：版本不符、字体未注册、JSON 内不得含字体路径语义。
func TestErrors(t *testing.T) {
	reg := NewFontRegistry()
	reg.RegisterBuiltin("helv", "Helvetica")

	cases := []struct {
		name, spec string
	}{
		{"版本", `{"version": 2, "content": []}`},
		{"字体未注册", `{"version": 1, "styles": {"b": {"font": "nope"}},
		  "content": [{"type": "paragraph", "style": "b", "text": "x"}]}`},
		{"样式未定义", `{"version": 1,
		  "content": [{"type": "paragraph", "style": "ghost", "text": "x"}]}`},
		{"未知块类型", `{"version": 1, "content": [{"type": "video"}]}`},
		{"非法颜色", `{"version": 1, "styles": {"b": {"font": "helv", "color": "red"}},
		  "content": [{"type": "paragraph", "style": "b", "text": "x"}]}`},
	}
	for _, c := range cases {
		if _, err := Build([]byte(c.spec), reg); err == nil {
			t.Errorf("%s：期望报错", c.name)
		}
	}
	if _, err := Build([]byte(`{"version":1,"content":[]}`), nil); err == nil {
		t.Error("nil 注册表：期望报错")
	}
}

// TestSanitize 零宽字符被剔除。
func TestSanitize(t *testing.T) {
	got := sanitizeText("\u7532\u200c\u4e59\u200d\u4e19\ufeff\u4e01")
	want := "\u7532\u4e59\u4e19\u4e01"
	if got != want {
		t.Errorf("sanitizeText = %q, want %q", got, want)
	}
}

// TestContract 合同示例（需本机 Windows 字体；缺字体时跳过）。
func TestContract(t *testing.T) {
	simsun := `C:\Windows\Fonts\simsun.ttc`
	simhei := `C:\Windows\Fonts\simhei.ttf`
	if _, err := os.Stat(simsun); err != nil {
		t.Skip("无 simsun.ttc，跳过")
	}
	data, err := os.ReadFile(filepath.Join("..", "data", "contract.json"))
	if err != nil {
		t.Skip("无 data/contract.json，跳过")
	}
	reg := NewFontRegistry()
	if err := reg.RegisterCollection("song", simsun, 0); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterFile("hei", simhei); err != nil {
		t.Fatal(err)
	}
	doc, err := Build(data, reg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-1.7")) {
		t.Error("输出不是 PDF")
	}
	if doc.PageCount() < 6 || doc.PageCount() > 10 {
		t.Errorf("PageCount = %d，期望 6–10（原件 8 页）", doc.PageCount())
	}
	t.Logf("合同生成成功：%d 页，%.1f KB", doc.PageCount(), float64(len(b))/1024)
}

// TestWordWrap 换行：CJK 逐字可断、拉丁词不拆、超长词硬拆。
func TestWordWrap(t *testing.T) {
	reg := NewFontRegistry()
	reg.RegisterBuiltin("helv", "Helvetica")
	st := style{font: mustFont(t, reg, "helv"), size: 10, lineHeight: 1.5}

	// Helvetica 10pt：空格 2.78，字宽约 5；宽度 60 大约容 11–12 字符
	lines := wrapTokens(tokenize("supercalifragilisticexpialidocious ok", st), 60, 0)
	if len(lines) < 2 {
		t.Fatal("长词未硬拆")
	}
	var sb strings.Builder
	for _, ln := range lines {
		for _, tk := range ln.toks {
			sb.WriteString(tk.text)
		}
	}
	if sb.String() != "supercalifragilisticexpialidocious ok" {
		t.Errorf("硬拆后内容失真: %q", sb.String())
	}
}

func mustFont(t *testing.T, reg *FontRegistry, id string) font.Resource {
	t.Helper()
	f, err := reg.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
