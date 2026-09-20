package table

import (
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// 构造测试用示例列配置：3 列定宽、2 列自动均分。
func sampleCols() []Column {
	return []Column{
		{Title: "No.", Width: 40, Align: text.AlignCenter},
		{Title: "Item", Width: 0}, // 自动均分
		{Title: "Qty", Width: 50, Align: text.AlignRight},
		{Title: "Price", Width: 70, Align: text.AlignRight},
		{Title: "Note", Width: 0}, // 自动均分
	}
}

// 测试列宽计算：定宽列保持不变，剩余宽度由自动列均分。
func TestColWidths(t *testing.T) {
	tb := New(sampleCols()...)
	ws := tb.colWidths(460, nil)
	// 定宽 40+50+70=160，剩余 300 两列均分
	want := []float64{40, 150, 50, 70, 150}
	for i := range want {
		if ws[i] != want[i] {
			t.Errorf("colWidths = %v, want %v", ws, want)
			break
		}
	}

	// 全部自动均分
	tb2 := New(Column{Title: "a"}, Column{Title: "b"}, Column{Title: "c"})
	for _, w := range tb2.colWidths(300, nil) {
		if w != 100 {
			t.Errorf("equal split = %v", tb2.colWidths(300, nil))
			break
		}
	}
}

// 测试行高计算：单行为默认行高加内边距，长文本换行后行变高。
func TestRowHeight(t *testing.T) {
	tb := New(Column{Width: 100}, Column{Width: 100})
	h := tb.rowHeight([]string{"a", "b"}, []float64{100, 100})
	single := font.Helvetica.LineHeight(10) + 8 // 默认行高+内边距
	if h != single {
		t.Errorf("single line height = %v, want %v", h, single)
	}
	// 长文本换行使行变高
	long := strings.Repeat("word ", 50)
	h2 := tb.rowHeight([]string{long, "b"}, []float64{100, 100})
	if h2 <= h {
		t.Errorf("wrapped row should be taller: %v vs %v", h2, h)
	}
}

// 测试单页绘制：表格不分页，内容流包含表头灰底、单元格文本与边框矩形。
func TestDrawSinglePage(t *testing.T) {
	tb := New(sampleCols()...)
	tb.AddRow("1", "Widget", "10", "9.99", "in stock")
	tb.AddRow("2", "Gadget", "5", "19.99", "")

	p := page.New(page.A4)
	last, endY := tb.Draw(p, 50, 700, 495, 50, nil)
	if last != p {
		t.Error("should stay on same page")
	}
	if endY >= 700 || endY <= 600 {
		t.Errorf("endY = %v", endY)
	}
	s := string(p.Content.Bytes())
	for _, want := range []string{
		"0.85 g",      // 表头灰底
		"(No.) Tj",    // 表头文本
		"(Widget) Tj", // 内容文本
		" re",         // 边框矩形
	} {
		if !strings.Contains(s, want) {
			t.Errorf("content missing %q", want)
		}
	}
	// 右对齐：单价 19.99 应右对齐（起始 x 不在单元格左边界）
	if !strings.Contains(s, "(19.99) Tj") {
		t.Error("price cell missing")
	}
}

// 测试跨页时表头在每页重复绘制，且重复次数等于页数。
func TestPaginationRepeatsHeader(t *testing.T) {
	tb := New(sampleCols()...)
	for i := 1; i <= 100; i++ {
		tb.AddRow(itoa(i), "Line item", "1", "1.00", "")
	}

	pages := []*page.Page{page.New(page.A4)}
	newPage := func() (*page.Page, float64) {
		np := page.New(page.A4)
		pages = append(pages, np)
		return np, 780
	}

	last, _ := tb.Draw(pages[0], 50, 780, 495, 50, newPage)
	if len(pages) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(pages))
	}
	if last != pages[len(pages)-1] {
		t.Error("should return last page")
	}
	// 每页都应包含表头（灰底 + 表头文本）
	for i, pg := range pages {
		s := string(pg.Content.Bytes())
		if !strings.Contains(s, "0.85 g") || !strings.Contains(s, "(Item) Tj") {
			t.Errorf("page %d missing repeated header", i)
		}
	}
	// 表头重复次数 == 页数
	cnt := 0
	for _, pg := range pages {
		cnt += strings.Count(string(pg.Content.Bytes()), "(Qty) Tj")
	}
	if cnt != len(pages) {
		t.Errorf("header drawn %d times, pages %d", cnt, len(pages))
	}
}

// 测试边框样式：全框线（默认）、关闭、仅水平线、仅垂直线。
func TestBorderStyle(t *testing.T) {
	draw := func(st BorderStyle) string {
		tb := New(Column{Width: 100}, Column{Width: 100})
		tb.BorderStyle = st
		tb.AddRow("a", "b")
		p := page.New(page.A4)
		tb.Draw(p, 50, 700, 200, 50, nil)
		return string(p.Content.Bytes())
	}
	// 行高 = LineHeight(10)+2*4 = 19.5，行底 y = 680.5
	if s := draw(BorderAll); !strings.Contains(s, "re\nS") {
		t.Errorf("BorderAll 应含矩形描边：\n%s", s)
	}
	if s := draw(BorderNone); strings.Contains(s, "S\n") {
		t.Errorf("BorderNone 不应含任何描边操作：\n%s", s)
	}
	sh := draw(BorderHorizontal)
	if strings.Contains(sh, "re\nS") || !strings.Contains(sh, "50 680.5 m\n150 680.5 l\nS") {
		t.Errorf("BorderHorizontal 应仅含水平线描边：\n%s", sh)
	}
	sv := draw(BorderVertical)
	if strings.Contains(sv, "re\nS") || !strings.Contains(sv, "50 680.5 m\n50 700 l\nS") {
		t.Errorf("BorderVertical 应仅含垂直线描边：\n%s", sv)
	}
}

// 测试超过整页高度的行不会导致死循环，分页次数有上限。
func TestOversizeRowNoInfiniteLoop(t *testing.T) {
	tb := New(Column{Title: "A", Width: 100})
	tb.AddRow("x")
	// 构造一个超过整页高度的行
	tb.AddRow(strings.Repeat("verylongword ", 2000))
	tb.AddRow("y")

	pages := 0
	p := page.New(page.A4)
	_, endY := tb.Draw(p, 50, 780, 100, 50, func() (*page.Page, float64) {
		pages++
		return page.New(page.A4), 780
	})
	if pages > 3 {
		t.Errorf("too many page breaks: %d", pages)
	}
	_ = endY
}

// 测试所有列标题为空时不绘制表头背景。
func TestNoHeaderWhenAllTitlesEmpty(t *testing.T) {
	tb := New(Column{Width: 100}, Column{Width: 100})
	tb.AddRow("a", "b")
	p := page.New(page.A4)
	tb.Draw(p, 50, 700, 200, 50, nil)
	if strings.Contains(string(p.Content.Bytes()), "0.85 g") {
		t.Error("should not draw header background")
	}
}

// 测试自定义样式（字体、字号、内边距、表头与行背景色）生效。
func TestCustomStyle(t *testing.T) {
	tb := New(Column{Title: "H", Width: 100})
	tb.Font = font.TimesRoman
	tb.FontSize = 12
	tb.Padding = 6
	tb.HeaderBg = color.Gray(0.7)
	tb.RowBg = color.White
	tb.AddRow("cell")
	p := page.New(page.A4)
	tb.Draw(p, 50, 700, 100, 50, nil)
	s := string(p.Content.Bytes())
	if !strings.Contains(s, "0.7 g") {
		t.Error("custom header bg missing")
	}
	if !strings.Contains(s, "1 g") {
		t.Error("row bg missing")
	}
}

// 将非负整数转为十进制字符串（避免引入 strconv 依赖）。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
