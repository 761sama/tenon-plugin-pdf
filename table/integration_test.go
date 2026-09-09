package table_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/table"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// TestMergedAutoWidthPDF 生成「合并 + 自适应列宽」的合同价款表 PDF，
// 并用 pdfinfo/pdftotext 外部核对。
func TestMergedAutoWidthPDF(t *testing.T) {
	doc := pdf.New()
	doc.Info().Title = "Contract Price Schedule"
	p := doc.AddPage(page.A4)
	p.DrawText(font.HelveticaBold, 16, 72, 780, "Contract Price Schedule")

	tbl := table.New(
		table.Column{Title: "Phase"}, // 自适应
		table.Column{Title: "Item"},  // 自适应
		table.Column{Title: "Qty", Align: text.AlignRight, Width: 40},
		table.Column{Title: "Amount (USD)", Align: text.AlignRight}, // 自适应
	)
	tbl.AutoWidth = true

	// 跨行：Preparation 阶段跨 3 行
	tbl.AddRowCells(table.C("Preparation").Span(1, 3), table.C("Site survey"), table.C("1"), table.C("1200.00"))
	tbl.AddRowCells(table.C("Temporary facilities"), table.C("1"), table.C("3500.00"))
	tbl.AddRowCells(table.C("Mobilization"), table.C("1"), table.C("2000.00"))
	// 跨行 + 普通行
	tbl.AddRowCells(table.C("Materials").Span(1, 2), table.C("Structural steel Q345"), table.C("120"), table.C("96000.00"))
	tbl.AddRowCells(table.C("Ready-mixed concrete C30"), table.C("350"), table.C("31500.00"))
	// 跨列：备注行跨 3 列
	tbl.AddRowCells(table.C("Note: prices include delivery to site.").Span(3, 1), table.C("—"))
	// 跨列：合计
	tbl.AddRowCells(table.C("GRAND TOTAL").Span(3, 1), table.C("134200.00"))

	newPage := func() (*page.Page, float64) { return doc.AddPage(page.A4), 780 }
	tbl.Draw(p, 72, 750, 450, 72, newPage)

	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext 不可用")
	}
	path := filepath.Join(t.TempDir(), "merged.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	txt, err := exec.Command("pdftotext", "-layout", path, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	for _, want := range []string{"Preparation", "Mobilization", "Structural steel", "GRAND TOTAL", "134200.00", "Note: prices include"} {
		if !strings.Contains(string(txt), want) {
			t.Errorf("提取文本缺少 %q", want)
		}
	}
	// pdfinfo 结构核对
	if out, err := exec.Command("pdfinfo", path).CombinedOutput(); err != nil {
		t.Fatalf("pdfinfo: %v\n%s", err, out)
	}
	t.Logf("合并+自适应表格 PDF 验证通过（%d 字节）", len(data))
}

// TestHeaderMergeAndSplitPDF 生成「合并表头 + 超高行跨页拆分」的表格 PDF，
// 并用 pdfinfo/pdftotext 外部核对：表头合并渲染、超高行拆分后文本连续完整。
func TestHeaderMergeAndSplitPDF(t *testing.T) {
	doc := pdf.New()
	doc.Info().Title = "Header Merge + Tall Row Split"
	p := doc.AddPage(page.A4)
	p.DrawText(font.HelveticaBold, 16, 72, 780, "Header Merge + Tall Row Split")

	tbl := table.New(
		table.Column{Width: 90},
		table.Column{Width: 130},
		table.Column{Width: 130},
	)
	// 合并表头：两行表头，跨列 + 跨行
	tbl.HeaderRows = 2
	tbl.AddRowCells(table.C("GroupAB").Span(2, 1), table.C("GroupC-2rows").Span(1, 2))
	tbl.AddRowCells(table.C("sub-a"), table.C("sub-b"))
	// 超高行：单列文本远超整页可用高度
	var words []string
	for i := 0; i < 400; i++ {
		words = append(words, fmt.Sprintf("t%04d", i))
	}
	tbl.AddRow("tall", strings.Join(words, " "), "tail-cell")
	tbl.AddRow("after-split", "last", "row")

	newPage := func() (*page.Page, float64) { return doc.AddPage(page.A4), 780 }
	tbl.Draw(p, 72, 750, 350, 60, newPage)

	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext 不可用")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "split.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	txt, err := exec.Command("pdftotext", path, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	// 合并表头与拆分长文本完整提取
	for _, want := range []string{"GroupAB", "GroupC-2rows", "sub-a", "sub-b",
		"t0000", "t0399", "tail-cell", "after-split"} {
		if !strings.Contains(string(txt), want) {
			t.Errorf("提取文本缺少 %q", want)
		}
	}
	// 拆分应跨页：总页数 >= 2
	info, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfinfo: %v\n%s", err, info)
	}
	m := regexp.MustCompile(`Pages:\s+(\d+)`).FindSubmatch(info)
	if m == nil {
		t.Fatalf("pdfinfo 无 Pages 字段:\n%s", info)
	}
	if n, _ := strconv.Atoi(string(m[1])); n < 2 {
		t.Errorf("超高行拆分后页数应 >= 2, 实际 %d", n)
	}
	t.Logf("表头合并+超高行拆分 PDF 验证通过（%d 字节）", len(data))
}
