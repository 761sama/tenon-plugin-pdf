package table

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
)

// --- 表头行合并 ---

func TestHeaderRowMergeGrid(t *testing.T) {
	tb := New(Column{Width: 60}, Column{Width: 60}, Column{Width: 60})
	tb.HeaderRows = 2
	tb.AddRowCells(C("H-AB").Span(2, 1), C("H-C-2rows").Span(1, 2))
	tb.AddRowCells(C("h-a2"), C("h-b2"))
	tb.AddRow("d1", "d2", "d3")

	grid, _ := tb.resolveGrid()
	if len(grid[0]) != 2 || grid[0][0].ColSpan != 2 || grid[0][1].RowSpan != 2 {
		t.Fatalf("表头行合并展开错误: %+v", grid[0])
	}
	// 第 2 表头行的 C 列被上方跨行单元格占用
	if len(grid[1]) != 2 || grid[1][0].col != 0 || grid[1][1].col != 1 {
		t.Fatalf("表头第 2 行占位错误: %+v", grid[1])
	}

	p := page.New(page.A4)
	tb.Draw(p, 50, 700, 180, 50, nil)
	s := string(p.Content.Bytes())
	for _, want := range []string{"(H-AB) Tj", "(H-C-2rows) Tj", "(h-a2) Tj", "(h-b2) Tj", "(d1) Tj"} {
		if !strings.Contains(s, want) {
			t.Errorf("渲染缺少 %q", want)
		}
	}
	// 跨 2 列的表头单元格边框宽应为两列之和 120
	if !strings.Contains(s, "120 19.5 re") {
		t.Errorf("跨列表头边框宽度错误:\n%s", s)
	}
}

func TestHeaderRowSpanClamp(t *testing.T) {
	tb := New(Column{Width: 60}, Column{Width: 60})
	tb.HeaderRows = 2
	// 表头行跨行越过表头区 → 截断到 HeaderRows 边界
	tb.AddRowCells(C("H").Span(1, 5), C("h-b1"))
	tb.AddRowCells(C("h-b2"))
	tb.AddRow("d1", "d2")

	grid, breakBefore := tb.resolveGrid()
	if grid[0][0].RowSpan != 2 {
		t.Errorf("表头跨行应截断为 2, got %d", grid[0][0].RowSpan)
	}
	// 表头区结束处（首个数据行）必须是安全分页边界
	if !breakBefore[2] {
		t.Error("表头/数据边界应允许分页")
	}
}

func TestHeaderMergePaginationRepeat(t *testing.T) {
	tb := New(Column{Width: 80}, Column{Width: 80})
	tb.HeaderRows = 1
	tb.AddRowCells(C("MERGED-HEADER").Span(2, 1))
	for i := 0; i < 30; i++ {
		tb.AddRow(fmt.Sprintf("row%02d-a", i), fmt.Sprintf("row%02d-b", i))
	}

	pages := []*page.Page{page.New(page.A4)}
	newPage := func() (*page.Page, float64) {
		np := page.New(page.A4)
		pages = append(pages, np)
		return np, 780
	}
	// bottomY 调高强制分页
	tb.Draw(pages[0], 50, 780, 160, 500, newPage)
	if len(pages) < 2 {
		t.Fatalf("应分页, got %d 页", len(pages))
	}
	// 合并表头在每页重复；全部数据行恰好出现一次
	total := 0
	for i, pg := range pages {
		s := string(pg.Content.Bytes())
		if !strings.Contains(s, "(MERGED-HEADER) Tj") {
			t.Errorf("第 %d 页缺少重复的合并表头", i)
		}
		total += strings.Count(s, "-a) Tj")
	}
	if total != 30 {
		t.Errorf("数据行总数 = %d, 应为 30（不重复不丢失）", total)
	}
}

// --- 超高行 / 跨行块跨页拆分 ---

var tjRe = regexp.MustCompile(`\(([^()]*)\) Tj`)

// extractLines 按绘制顺序提取页面内容流中的文本行。
func extractLines(pg *page.Page) []string {
	var out []string
	for _, m := range tjRe.FindAllStringSubmatch(string(pg.Content.Bytes()), -1) {
		out = append(out, m[1])
	}
	return out
}

// splitPages 用固定页面几何绘制表格，返回全部页面。
func splitPages(tb *Table, bottomY float64) []*page.Page {
	pages := []*page.Page{page.New(page.A4)}
	newPage := func() (*page.Page, float64) {
		np := page.New(page.A4)
		pages = append(pages, np)
		return np, 780
	}
	tb.Draw(pages[0], 50, 780, 200, bottomY, newPage)
	return pages
}

func TestTallRowSplitAcrossPages(t *testing.T) {
	// 单行文本远超整页可用高度 → 按文本行拆分
	var words []string
	for i := 0; i < 600; i++ {
		words = append(words, fmt.Sprintf("w%04d", i))
	}
	full := strings.Join(words, " ")

	tb := New(Column{Width: 200})
	tb.AddRow(full)
	tb.Font = font.Courier
	tb.FontSize = 10

	pages := splitPages(tb, 60)
	if len(pages) < 2 {
		t.Fatalf("超高行应拆分到多页, got %d", len(pages))
	}
	// 汇总各页文本行：顺序一致、无重复、无丢失
	var got []string
	for _, pg := range pages {
		got = append(got, extractLines(pg)...)
	}
	gotText := strings.Join(got, " ")
	for i, w := range words {
		if !strings.Contains(gotText, w) {
			t.Fatalf("第 %d 个词 %q 丢失", i, w)
		}
	}
	// 行框按窗口归属：每个拆分文本行只出现一次
	seen := map[string]int{}
	for _, l := range got {
		seen[l]++
		if seen[l] > 1 {
			t.Errorf("文本行 %q 重复绘制 %d 次", l, seen[l])
		}
	}
	// 顺序：第一个词在首页，最后一个词在末页
	if !strings.Contains(strings.Join(extractLines(pages[0]), " "), "w0000") {
		t.Error("首页应含起始内容")
	}
	if !strings.Contains(strings.Join(extractLines(pages[len(pages)-1]), " "), "w0599") {
		t.Error("末页应含结尾内容")
	}
}

func TestOversizedRowspanBlockSplit(t *testing.T) {
	// 跨 40 行的合并块超过整页 → 按行边界拆分
	tb := New(Column{Width: 100}, Column{Width: 100})
	var longWords []string
	for i := 0; i < 200; i++ {
		longWords = append(longWords, fmt.Sprintf("m%03d", i))
	}
	tb.AddRowCells(C(strings.Join(longWords, " ")).Span(1, 40), C("blk-row-00"))
	for i := 1; i < 40; i++ {
		tb.AddRowCells(C(fmt.Sprintf("blk-row-%02d", i)))
	}
	// 块前块后各加普通行，验证块在页流中的位置与续绘
	for i := 0; i < 5; i++ {
		tb.Rows = append([][]Cell{{C(fmt.Sprintf("pre-%d", i)), C("x")}}, tb.Rows...)
	}
	for i := 0; i < 5; i++ {
		tb.AddRow(fmt.Sprintf("post-%d", i), "y")
	}
	tb.Font = font.Courier
	tb.FontSize = 10

	pages := splitPages(tb, 60)
	if len(pages) < 3 {
		t.Fatalf("超页跨行块应拆分到多页, got %d", len(pages))
	}
	var got []string
	for _, pg := range pages {
		got = append(got, extractLines(pg)...)
	}
	gotText := strings.Join(got, " ")
	// 块内全部短行标签不丢不重
	seen := map[string]int{}
	for _, l := range got {
		seen[l]++
	}
	for i := 0; i < 40; i++ {
		label := fmt.Sprintf("blk-row-%02d", i)
		if seen[label] != 1 {
			t.Errorf("%s 出现 %d 次（应恰好 1 次）", label, seen[label])
		}
	}
	for i := 0; i < 5; i++ {
		if seen[fmt.Sprintf("pre-%d", i)] != 1 || seen[fmt.Sprintf("post-%d", i)] != 1 {
			t.Errorf("块前/块后行丢失或重复: pre-%d=%d post-%d=%d",
				i, seen[fmt.Sprintf("pre-%d", i)], i, seen[fmt.Sprintf("post-%d", i)])
		}
	}
	// 跨行合并单元格的长文本连续完整
	for i, w := range longWords {
		if !strings.Contains(gotText, w) {
			t.Fatalf("合并单元格第 %d 个词 %q 丢失", i, w)
		}
	}
	// 顺序：pre 行在 blk 行之前，post 行在最后
	idx := func(s string) int { return strings.Index(gotText, s) }
	if idx("pre-0") > idx("blk-row-00") || idx("blk-row-39") > idx("post-4") {
		t.Error("块前/块内/块后内容顺序错误")
	}
}

func TestSplitBordersWithinPage(t *testing.T) {
	// 拆分页上所有矩形边框不得越过 bottomY
	var words []string
	for i := 0; i < 600; i++ {
		words = append(words, fmt.Sprintf("w%04d", i))
	}
	tb := New(Column{Title: "H", Width: 200})
	tb.AddRow(strings.Join(words, " "))
	tb.Font = font.Courier

	const bottomY = 60.0
	pages := splitPages(tb, bottomY)
	re := regexp.MustCompile(`(?m)^[\d.]+ ([\d.]+) [\d.]+ [\d.]+ re$`)
	for i, pg := range pages {
		for _, m := range re.FindAllStringSubmatch(string(pg.Content.Bytes()), -1) {
			y := 0.0
			fmt.Sscanf(m[1], "%g", &y)
			if y < bottomY-0.5 { // 线宽一半的容差
				t.Errorf("第 %d 页边框下边缘 y=%v 越过 bottomY=%v", i, y, bottomY)
			}
		}
	}
}
