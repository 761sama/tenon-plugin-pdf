package jsongen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
)

// 最小文档：内置字体 + 段落/表格/间隔/分页。
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

// 非法输入：版本不符、字体未注册、JSON 内不得含字体路径语义。
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
		{"页码位置非法", `{"version": 1, "styles": {"b": {"font": "helv"}},
		  "pageNumber": {"style": "b", "position": "middle"}, "content": []}`},
		{"页码样式未定义", `{"version": 1, "pageNumber": {"style": "ghost"}, "content": []}`},
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

// headerRepeat：跨页表格表头是否重复可由 JSON 控制（默认重复）。
func TestHeaderRepeat(t *testing.T) {
	reg := NewFontRegistry()
	reg.RegisterBuiltin("helv", "Helvetica")
	rows := strings.Repeat(`["r","v"],`, 100)
	build := func(headerRepeat string) (string, int) {
		spec := `{
		  "version": 1,
		  "page": {"size": "A4"},
		  "styles": {"body": {"font": "helv", "size": 10}},
		  "content": [
		    {"type": "table", "style": "body", "headerRows": 1` + headerRepeat + `,
		      "columns": [{"width": 100}, {"width": 100}],
		      "rows": [["COLHDR", "COLHDR"],` + rows + `["r","v"]]}
		  ]
		}`
		doc, err := Build([]byte(spec), reg)
		if err != nil {
			t.Fatal(err)
		}
		if doc.PageCount() < 2 {
			t.Fatalf("表格未分页（%d 页），无法验证表头重复", doc.PageCount())
		}
		doc.SetCompress(false)
		b, err := doc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return string(b), doc.PageCount()
	}
	rep, nPages := build("")
	if got := strings.Count(rep, "(COLHDR)"); got != 2*nPages {
		t.Errorf("默认（重复表头）：(COLHDR) 出现 %d 次，期望 %d（每页一行两列）", got, 2*nPages)
	}
	noRep, _ := build(`,"headerRepeat": false`)
	if got := strings.Count(noRep, "(COLHDR)"); got != 2 {
		t.Errorf("headerRepeat=false：(COLHDR) 出现 %d 次，期望 2（仅首页一行两列）", got)
	}
}

// 页码：按节回绘，section 块强制换页并重新计数。
func TestPageNumbers(t *testing.T) {
	reg := NewFontRegistry()
	reg.RegisterBuiltin("helv", "Helvetica")
	spec := `{
	  "version": 1,
	  "page": {"size": "A4"},
	  "pageNumber": {"style": "body", "format": "P{page}/{total}"},
	  "styles": {"body": {"font": "helv", "size": 10}},
	  "content": [
	    {"type": "paragraph", "style": "body", "text": "a"},
	    {"type": "pageBreak"},
	    {"type": "paragraph", "style": "body", "text": "b"},
	    {"type": "section"},
	    {"type": "paragraph", "style": "body", "text": "c"},
	    {"type": "pageBreak"},
	    {"type": "paragraph", "style": "body", "text": "d"}
	  ]
	}`
	doc, err := Build([]byte(spec), reg)
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount() != 4 {
		t.Fatalf("PageCount = %d，期望 4（section 强制换页）", doc.PageCount())
	}
	doc.SetCompress(false)
	b, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// 两节各 2 页：第 1 节页 1-2，第 2 节重新计数页 1-2
	if got := strings.Count(s, "(P1/2)"); got != 2 {
		t.Errorf("(P1/2) 出现 %d 次，期望 2（两节各自的首页）", got)
	}
	if got := strings.Count(s, "(P2/2)"); got != 2 {
		t.Errorf("(P2/2) 出现 %d 次，期望 2（两节各自的次页）", got)
	}
	if strings.Contains(s, "(P3/") || strings.Contains(s, "(P4/") {
		t.Error("第 2 节未重新计数（出现 P3/P4）")
	}
}

// 页码起始值与位置：start 偏移页码，top 绘制于页顶。
func TestPageNumbersStart(t *testing.T) {
	reg := NewFontRegistry()
	reg.RegisterBuiltin("helv", "Helvetica")
	spec := `{
	  "version": 1,
	  "page": {"size": "A4"},
	  "pageNumber": {"style": "body", "format": "{page}/{total}", "start": 0, "position": "top"},
	  "styles": {"body": {"font": "helv", "size": 10}},
	  "content": [
	    {"type": "paragraph", "style": "body", "text": "a"},
	    {"type": "section"},
	    {"type": "paragraph", "style": "body", "text": "b"}
	  ]
	}`
	doc, err := Build([]byte(spec), reg)
	if err != nil {
		t.Fatal(err)
	}
	doc.SetCompress(false)
	b, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// start 为 0 时按缺省 1 处理：两节各 1 页，均为 1/1
	if got := strings.Count(s, "(1/1)"); got != 2 {
		t.Errorf("(1/1) 出现 %d 次，期望 2", got)
	}
	// 页顶绘制：基线 y = 页高 - offset（841.89 - 36 = 805.89）
	if !strings.Contains(s, "805.89") {
		t.Error("页码未绘制在页顶（position=top）")
	}
}

// 零宽字符被剔除。
func TestSanitize(t *testing.T) {
	got := sanitizeText("\u7532\u200c\u4e59\u200d\u4e19\ufeff\u4e01")
	want := "\u7532\u4e59\u4e19\u4e01"
	if got != want {
		t.Errorf("sanitizeText = %q, want %q", got, want)
	}
}

// 合同示例（需本机 Windows 字体；缺字体时跳过）。
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

// 换行：CJK 逐字可断、拉丁词不拆、超长词硬拆。
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

// 辅助函数：按 id 从字体注册表 reg 中取出字体资源，查找失败即终止测试 t；返回字体资源。
func mustFont(t *testing.T, reg *FontRegistry, id string) font.Resource {
	t.Helper()
	f, err := reg.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
