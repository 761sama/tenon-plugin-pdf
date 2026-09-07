package table

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

func TestColSpanGrid(t *testing.T) {
	tb := New(
		Column{Title: "A", Width: 50},
		Column{Title: "B", Width: 50},
		Column{Title: "C", Width: 50},
	)
	tb.AddRowCells(C("a1"), C("merged-BC").Span(2, 1))
	tb.AddRowCells(C("a2"), C("b2"), C("c2"))

	grid, breakBefore := tb.resolveGrid()
	// 第 1 行：两个单元格，第二个跨 2 列
	if len(grid[0]) != 2 || grid[0][1].col != 1 || grid[0][1].ColSpan != 2 {
		t.Fatalf("row0 grid: %+v", grid[0])
	}
	// 第 2 行：三个单元格正常排布
	if len(grid[1]) != 3 || grid[1][2].col != 2 {
		t.Fatalf("row1 grid: %+v", grid[1])
	}
	if !breakBefore[1] {
		t.Error("无跨行合并时不应锁定分页边界")
	}
}

func TestRowSpanGridAndHeight(t *testing.T) {
	tb := New(Column{Width: 60}, Column{Width: 60}, Column{Width: 60})
	// 第 1 行 A 列跨 3 行；后续行该列被自动跳过
	tb.AddRowCells(C("span-3-rows").Span(1, 3), C("b1"), C("c1"))
	tb.AddRowCells(C("b2"), C("c2")) // A 列被占用，从 B 列开始
	tb.AddRowCells(C("b3"), C("c3"))

	grid, breakBefore := tb.resolveGrid()
	if grid[0][0].RowSpan != 3 {
		t.Fatalf("rowspan: %+v", grid[0][0])
	}
	// 第 2、3 行的首个单元格应落在第 1 列（第 0 列被占用跳过）
	if grid[1][0].col != 1 || grid[2][0].col != 1 {
		t.Fatalf("occupied col not skipped: %+v / %+v", grid[1][0], grid[2][0])
	}
	// 跨行块内部不可分页
	if breakBefore[1] || breakBefore[2] {
		t.Error("rowspan 块内不应允许分页")
	}
	if !breakBefore[0] {
		t.Error("块起始前应允许分页")
	}

	// 行高：跨行文本超出行高之和时撑高末行
	tb.Rows[0][0] = C(strings.Repeat("word ", 40)).Span(1, 3)
	grid, _ = tb.resolveGrid() // 内容变更后重新展开网格
	widths := tb.colWidths(180, grid)
	hs := tb.rowHeights(grid, widths)
	need := tb.cellHeight(placedCell{Cell: tb.Rows[0][0]}, 60)
	got := hs[0] + hs[1] + hs[2]
	if got < need {
		t.Errorf("rowspan 总高 %v < 需求 %v", got, need)
	}
}

func TestRowSpanClamp(t *testing.T) {
	tb := New(Column{Width: 50})
	tb.AddRowCells(C("x").Span(1, 99)) // 超出行数 → 截断
	grid, _ := tb.resolveGrid()
	if grid[0][0].RowSpan != 1 {
		t.Errorf("rowspan 应截断为 1, got %d", grid[0][0].RowSpan)
	}
}

func TestRowSpanPaginationNotSplit(t *testing.T) {
	tb := New(
		Column{Title: "A", Width: 60},
		Column{Title: "B", Width: 60},
	)
	// 制造多行，中间有一个跨 4 行的块
	for i := 0; i < 20; i++ {
		tb.AddRow(fmt.Sprintf("r%d", i), "x")
	}
	// 把第 10 行改为跨 4 行合并
	tb.Rows[10][0] = C("merge-10-13").Span(1, 4)

	pages := []*page.Page{page.New(page.A4)}
	newPage := func() (*page.Page, float64) {
		np := page.New(page.A4)
		pages = append(pages, np)
		return np, 780
	}
	// bottomY 调高强制每页只放约 8 行，让合并块必然逼近分页边界
	tb.Draw(pages[0], 50, 780, 120, 650, newPage)

	if len(pages) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(pages))
	}
	// 合并单元格与其覆盖行必须在同一页
	for i, pg := range pages {
		s := string(pg.Content.Bytes())
		hasMerge := strings.Contains(s, "(merge-10-13) Tj")
		hasR11 := strings.Contains(s, "(r11) Tj")
		hasR13 := strings.Contains(s, "(r13) Tj")
		if hasMerge && !(hasR11 && hasR13) {
			t.Errorf("page %d: rowspan 块被拆分", i)
		}
	}
}

func TestAutoWidth(t *testing.T) {
	// Courier 12pt：每字符 7.2pt，padding 默认 4 → "1234" 内容宽 4*7.2+8=36.8
	tb := New(
		Column{Title: "No."},  // 自动
		Column{Title: "Item"}, // 自动
		Column{Title: "Fixed", Width: 100},
	)
	tb.AutoWidth = true
	tb.Font = font.Courier
	tb.FontSize = 12
	tb.AddRow("1", "AAAA", "x")
	tb.AddRow("22", "AAAAAAAA", "y") // Item 列最长 8 字符

	grid, _ := tb.resolveGrid()
	ws := tb.colWidths(300, grid)
	if ws[2] != 100 {
		t.Errorf("定宽列 = %v, want 100", ws[2])
	}
	// Item 列内容更宽 → 自适应后应比 No. 列宽
	if ws[1] <= ws[0] {
		t.Errorf("自适应列宽失效: %v", ws)
	}
	// 总宽应等于 300
	if sum := ws[0] + ws[1] + ws[2]; sum != 300 {
		t.Errorf("总宽 = %v, want 300", sum)
	}
}

func TestAutoWidthShrink(t *testing.T) {
	// 内容超宽时按比例压缩但不低于 minW（最长单词）
	tb := New(Column{Title: "A"}, Column{Title: "B"})
	tb.AutoWidth = true
	tb.Font = font.Courier
	tb.FontSize = 12
	long := strings.Repeat("w", 100)
	tb.AddRow(long, "short")
	grid, _ := tb.resolveGrid()
	ws := tb.colWidths(200, grid)
	if ws[0]+ws[1] < 200-0.01 {
		t.Errorf("压缩后应填满总宽: %v", ws)
	}
	// B 列 minW = 5*7.2+8 = 44
	if ws[1] < 44-0.01 {
		t.Errorf("B 列宽 %v 低于 minW", ws[1])
	}
}

func TestDrawWithSpans(t *testing.T) {
	tb := New(
		Column{Title: "Category", Width: 0},
		Column{Title: "Item", Width: 0},
		Column{Title: "Amount", Width: 0, Align: text.AlignRight},
	)
	tb.AddRowCells(C("Materials").Span(1, 2), C("Steel"), C("1000"))
	tb.AddRowCells(C("Cement"), C("500"))
	tb.AddRowCells(C("TOTAL").Span(2, 1), C("1500"))

	p := page.New(page.A4)
	_, endY := tb.Draw(p, 50, 700, 300, 50, nil)
	s := string(p.Content.Bytes())
	for _, want := range []string{"(Materials) Tj", "(Cement) Tj", "(TOTAL) Tj", "(1500) Tj"} {
		if !strings.Contains(s, want) {
			t.Errorf("内容缺少 %q", want)
		}
	}
	if endY >= 700 {
		t.Errorf("endY = %v", endY)
	}
}
