// Package table 实现合同类文档的表格布局与绘制：
// 列定义（定宽/自动均分/内容自适应）、灰底加粗表头、单元格边框与内边距、
// 文本对齐、单元格合并（跨列/跨行）、跨页自动切分并重复表头。
//
// 表格通过 page.Page 画布绘制，分页通过调用方提供的回调完成，
// 因此本包不依赖文档门面，可独立测试与复用。
package table

import (
	"math"

	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// Column 表格列定义。
type Column struct {
	Title string         // 表头文本；所有列为空则不绘制表头
	Width float64        // 列宽（磅）；0 表示自动分配（均分或内容自适应，见 Table.AutoWidth）
	Align text.Alignment // 内容行对齐（表头始终居中），数字列可用 text.AlignRight
}

// Cell 单元格。ColSpan/RowSpan 表示跨列/跨行合并（默认 1）。
type Cell struct {
	Text    string
	ColSpan int
	RowSpan int

	// 可选覆盖（零值取表格默认）
	Color color.Color     // 文本颜色（nil 取 TextColor）
	Bg    color.Color     // 单元格底色（nil 不填充；表头行默认 HeaderBg）
	Align *text.Alignment // 水平对齐（nil 取所在列对齐）
	Font  font.Resource   // 字体（nil 取 Table.Font）
	Size  float64         // 字号（0 取 Table.FontSize）
}

// C 构造普通单元格。
func C(text string) Cell { return Cell{Text: text, ColSpan: 1, RowSpan: 1} }

// Span 返回跨 colSpan 列、rowSpan 行的合并单元格。
func (c Cell) Span(colSpan, rowSpan int) Cell {
	if colSpan < 1 {
		colSpan = 1
	}
	if rowSpan < 1 {
		rowSpan = 1
	}
	c.ColSpan = colSpan
	c.RowSpan = rowSpan
	return c
}

// Table 表格。
type Table struct {
	Columns []Column
	Rows    [][]Cell

	// 样式（零值取默认）
	Font         font.Resource // 内容字体，默认 Helvetica
	HeaderFont   font.Resource // 表头字体，默认 Helvetica-Bold
	FontSize     float64       // 内容字号，默认 10
	HeaderSize   float64       // 表头字号，默认 10
	Padding      float64       // 单元格内边距，默认 4
	HeaderBg     color.Color   // 表头背景色，默认 Gray(0.85)
	RowBg        color.Color   // 内容行背景色，默认白（nil 不填充）
	Border       color.Color   // 边框色，默认黑
	BorderW      float64       // 边框线宽，默认 0.5
	HeaderRepeat bool          // 跨页时重复表头，默认 true

	// HeaderRows 将 Rows 前 N 行作为表头行（底色 HeaderBg，跨页随 HeaderRepeat 重复）。
	// 与 Column.Title 表头互斥：HeaderRows > 0 时忽略列标题。
	// 表头行走正常单元格排版（支持多行文本与 Cell 覆盖），适合复杂表头；
	// 表头行支持单元格合并（Span，用法与数据行一致），
	// 但跨行不越过表头区（超出自动截断到 HeaderRows 边界）。
	HeaderRows int

	TextColor   color.Color // 内容文本色，默认黑
	HeaderColor color.Color // 表头文本色，默认取 TextColor（再默认黑）

	// AutoWidth 为 true 时，Width 为 0 的列按内容自适应测算列宽
	//（默认 false：均分剩余宽度）。定宽列不受影响。
	AutoWidth bool
}

// New 创建表格。
func New(cols ...Column) *Table {
	return &Table{Columns: cols, HeaderRepeat: true}
}

// AddRow 追加一行；单元格数量不足补空串，超出截断。
func (t *Table) AddRow(cells ...string) *Table {
	row := make([]Cell, len(t.Columns))
	for i := range row {
		if i < len(cells) {
			row[i] = C(cells[i])
		} else {
			row[i] = C("")
		}
	}
	t.Rows = append(t.Rows, row)
	return t
}

// AddRowCells 追加一行（支持合并单元格）。
// 一行内各单元格的 ColSpan 之和不应超过列数；被跨行单元格覆盖的位置
// 会自动跳过（无需占位空单元格）。
func (t *Table) AddRowCells(cells ...Cell) *Table {
	row := make([]Cell, len(cells))
	copy(row, cells)
	t.Rows = append(t.Rows, row)
	return t
}

// AddRows 批量追加。
func (t *Table) AddRows(rows [][]string) *Table {
	for _, r := range rows {
		t.AddRow(r...)
	}
	return t
}

func (t *Table) bodyFont() font.Resource {
	if t.Font != nil {
		return t.Font
	}
	return font.Helvetica
}

func (t *Table) headFont() font.Resource {
	if t.HeaderFont != nil {
		return t.HeaderFont
	}
	return font.HelveticaBold
}

func (t *Table) fontSize() float64 {
	if t.FontSize > 0 {
		return t.FontSize
	}
	return 10
}

func (t *Table) headerSize() float64 {
	if t.HeaderSize > 0 {
		return t.HeaderSize
	}
	return t.fontSize()
}

func (t *Table) padding() float64 {
	if t.Padding > 0 {
		return t.Padding
	}
	return 4
}

func (t *Table) headerBg() color.Color {
	if t.HeaderBg != nil {
		return t.HeaderBg
	}
	return color.Gray(0.85)
}

func (t *Table) border() color.Color {
	if t.Border != nil {
		return t.Border
	}
	return color.Black
}

func (t *Table) borderW() float64 {
	if t.BorderW > 0 {
		return t.BorderW
	}
	return 0.5
}

func (t *Table) textColor() color.Color {
	if t.TextColor != nil {
		return t.TextColor
	}
	return color.Black
}

func (t *Table) headerTextColor() color.Color {
	if t.HeaderColor != nil {
		return t.HeaderColor
	}
	return t.textColor()
}

// headerRowCount 表头行数（截断到数据行数）。
func (t *Table) headerRowCount() int {
	n := t.HeaderRows
	if n > len(t.Rows) {
		n = len(t.Rows)
	}
	return n
}

// hasHeader 所有列标题为空时不绘制表头。
func (t *Table) hasHeader() bool {
	for _, c := range t.Columns {
		if c.Title != "" {
			return true
		}
	}
	return false
}

// colWidths 解析列宽：定宽列照用；宽度为 0 的列均分剩余宽度（默认）
// 或按内容自适应（AutoWidth 开启时）。
func (t *Table) colWidths(total float64, grid [][]placedCell) []float64 {
	if t.AutoWidth {
		return t.autoColWidths(total, grid)
	}
	n := len(t.Columns)
	ws := make([]float64, n)
	fixed, auto := 0.0, 0
	for i, c := range t.Columns {
		if c.Width > 0 {
			ws[i] = c.Width
			fixed += c.Width
		} else {
			auto++
		}
	}
	rest := total - fixed
	if rest < 0 {
		rest = 0
	}
	if auto > 0 {
		share := rest / float64(auto)
		for i := range ws {
			if ws[i] == 0 {
				ws[i] = share
			}
		}
	}
	return ws
}

// rowHeight 按内容换行计算行高（内容行，不考虑合并）。
func (t *Table) rowHeight(cells []string, widths []float64) float64 {
	f, size, pad := t.bodyFont(), t.fontSize(), t.padding()
	maxLines := 1
	for i, cell := range cells {
		if i >= len(widths) {
			break
		}
		if n := len(text.Wrap(f, size, widths[i]-2*pad, cell)); n > maxLines {
			maxLines = n
		}
	}
	return float64(maxLines)*f.LineHeight(size) + 2*pad
}

func (t *Table) headerHeight() float64 {
	f := t.headFont()
	return f.LineHeight(t.headerSize()) + 2*t.padding()
}

// placedCell 网格定位后的单元格。
type placedCell struct {
	Cell
	row, col int // 起始行列
}

// resolveGrid 将行展开为网格：返回按行分组的放置单元格，
// 以及每个行边界是否可安全分页（跨行合并块内部不可拆分）。
func (t *Table) resolveGrid() ([][]placedCell, []bool) {
	nCol := len(t.Columns)
	nRow := len(t.Rows)
	grid := make([][]placedCell, nRow)
	breakBefore := make([]bool, nRow)
	for i := range breakBefore {
		breakBefore[i] = true
	}
	// 被上方跨行单元格覆盖的列占位
	occupied := make([][]bool, nRow)
	for r := range occupied {
		occupied[r] = make([]bool, nCol)
	}

	for r, row := range t.Rows {
		col := 0
		for _, cell := range row {
			for col < nCol && occupied[r][col] {
				col++
			}
			if col >= nCol {
				break // 超出列数的单元格丢弃
			}
			cs, rs := cell.ColSpan, cell.RowSpan
			if cs < 1 {
				cs = 1
			}
			if rs < 1 {
				rs = 1
			}
			if col+cs > nCol {
				cs = nCol - col // 截断到列数
			}
			if r+rs > nRow {
				rs = nRow - r // 截断到行数
			}
			if hdr := t.headerRowCount(); r < hdr && rs > hdr-r {
				rs = hdr - r // 表头行跨行不越过表头区
			}
			pc := placedCell{Cell: cell, row: r, col: col}
			pc.ColSpan, pc.RowSpan = cs, rs // 截断后的跨度
			grid[r] = append(grid[r], pc)
			if rs > 1 {
				for rr := r + 1; rr < r+rs; rr++ {
					breakBefore[rr] = false
					for cc := col; cc < col+cs; cc++ {
						occupied[rr][cc] = true
					}
				}
			}
			col += cs
		}
	}
	return grid, breakBefore
}

// Draw 从 (x, yTop) 开始绘制表格，yTop 为表格上边缘，bottomY 为页底边界。
// 空间不足且 newPage 非 nil 时，调用 newPage 获取新页与续页起始 y 并继续绘制
// （HeaderRepeat 为 true 时自动重绘表头）；newPage 为 nil 则超出边界继续绘制。
// 跨行合并块优先整体移动：放得下时不拆到两页；
// 块（或单行）超过整页可用高度时按行边界拆分绘制，单行仍超高时按文本行拆分，
// 内容连续、不溢出、不丢失（newPage 为 nil 时退化为溢出绘制）。
// 返回结束时所在页与结束 y（表格下边缘）。
func (t *Table) Draw(p *page.Page, x, yTop, w, bottomY float64, newPage func() (*page.Page, float64)) (*page.Page, float64) {
	grid, breakBefore := t.resolveGrid()
	widths := t.colWidths(w, grid)
	heights := t.rowHeights(grid, widths)
	headerRows := t.headerRowCount()
	headerH := 0.0
	if headerRows > 0 {
		for r := 0; r < headerRows; r++ {
			headerH += heights[r]
		}
	} else if t.hasHeader() {
		headerH = t.headerHeight()
	}

	// 分页布局：把内容行分配到各页（跨行块优先整体移动，超页则窗口化拆分）
	runs := []pageRun{}
	cur := pageRun{page: p, top: yTop, start: headerRows}
	y := yTop - headerH
	for i := headerRows; i < len(heights); i++ {
		h := heights[i]
		if y-h < bottomY && newPage != nil {
			// 当前页放不下 → 找安全分页边界（回溯跨行块起点）
			j := i
			for j > cur.start && !breakBefore[j] {
				j = t.breakStart(j, grid)
			}
			if j > cur.start {
				// 在行 j 前分页：j..i-1 行移至新页
				cur.end = j
				runs = append(runs, cur)
				np, ntop := newPage()
				cur = pageRun{page: np, top: ntop, start: j}
				y = ntop - headerH
				for k := j; k < i; k++ {
					y -= heights[k]
				}
			} else {
				// 不可分页块超过整页可用高度：窗口化拆分绘制
				be := cur.start + 1
				for be < len(heights) && !breakBefore[be] {
					be++
				}
				cur, y = t.splitRuns(&runs, cur, be, heights, grid, widths, headerH, bottomY, newPage)
				i = be - 1 // 循环末尾 i++，从块后第一行继续
				continue
			}
		}
		y -= h
		cur.end = i + 1
	}
	runs = append(runs, cur)

	// 逐页渲染
	for _, run := range runs {
		if run.split {
			t.renderSplitRun(run.page, x, run.top, widths, heights, grid, headerH, headerRows, run.start, run.end, run.wTop, run.wBot)
		} else {
			t.renderRun(run.page, x, run.top, w, widths, heights, grid, headerH, headerRows, run.start, run.end)
		}
	}
	last := runs[len(runs)-1]
	endY := last.top - headerH
	if last.split {
		endY -= last.wBot - last.wTop
	} else {
		for r := last.start; r < last.end; r++ {
			endY -= heights[r]
		}
	}
	return last.page, endY
}

// splitRuns 把超过整页可用高度的不可分页块 [cur.start, be) 拆分为若干窗口片段
// 追加到 runs，返回块后行继续绘制所用的 run 与当前 y。
// 窗口边界优先吸附到行边界；单行仍超页时吸附到文本行边界；都不行则硬切。
func (t *Table) splitRuns(runs *[]pageRun, cur pageRun, be int, heights []float64,
	grid [][]placedCell, widths []float64, headerH, bottomY float64,
	newPage func() (*page.Page, float64)) (pageRun, float64) {

	bs := cur.start
	// 块内累计高度（相对块起点）
	cum := make([]float64, be-bs+1)
	for r := bs; r < be; r++ {
		cum[r-bs+1] = cum[r-bs] + heights[r]
	}
	total := cum[be-bs]

	pg, ptop := cur.page, cur.top
	avail := ptop - headerH - bottomY
	wTop := 0.0
	for wTop < total {
		if avail <= 0 {
			avail = 1 // 防御：保证必然前进
		}
		limit := wTop + avail
		wBot := limit
		if wBot >= total {
			wBot = total
		} else if b := t.cleanCut(grid, widths, heights, bs, be, wTop, limit); b > wTop {
			wBot = b // 吸附到不切断任何文本行的边界
		}
		*runs = append(*runs, pageRun{page: pg, top: ptop, start: bs, end: be, split: true, wTop: wTop, wBot: wBot})
		wTop = wBot
		if wTop < total {
			pg, ptop = newPage()
			avail = ptop - headerH - bottomY
		}
	}
	// 块后行从最后一个拆分页继续
	next := pageRun{page: pg, top: ptop, start: be}
	return next, ptop - headerH - (total - wTopOfLast(*runs))
}

// wTopOfLast 返回 runs 末个（拆分）run 的窗口起点。
func wTopOfLast(runs []pageRun) float64 { return runs[len(runs)-1].wTop }

// cleanCut 在 (wTop, limit] 内寻找不切断任何单元格文本行的最大拆分点：
// 候选为行边界与所有单元格的文本行边界（行框顶部的块内坐标）；
// 候选点落在某单元格内部时，必须避开其文本行内部（在行边界上或文本区之外）。
// 找不到干净切点时返回 0（调用方退化为硬切，由渲染期裁剪兜底）。
func (t *Table) cleanCut(grid [][]placedCell, widths, heights []float64,
	bs, be int, wTop, limit float64) float64 {
	cum := make([]float64, be-bs+1)
	for r := bs; r < be; r++ {
		cum[r-bs+1] = cum[r-bs] + heights[r]
	}
	type cellSpan struct {
		ct, cb    float64 // 单元格纵向范围
		hasText   bool
		textStart float64 // 首行行框顶部
		textEnd   float64 // 末行行框底部
		leading   float64 // 行距
	}
	var cells []cellSpan
	var cands []float64
	for r := bs; r < be; r++ {
		for _, c := range grid[r] {
			ct := cum[c.row-bs]
			cb := ct
			for rr := c.row; rr < c.row+c.RowSpan; rr++ {
				cb += heights[rr]
			}
			if cb <= wTop || ct >= limit {
				continue // 与窗口无关的单元格
			}
			cs := cellSpan{ct: ct, cb: cb}
			if c.Text != "" {
				_, _, leading, lines, voff := t.cellText(c, spanWidth(widths, c.col, c.ColSpan), cb-ct, false)
				cs.hasText = true
				cs.textStart = ct + voff
				cs.leading = leading
				cs.textEnd = ct + voff + float64(len(lines))*leading
				for k := 1; k <= len(lines); k++ {
					if b := ct + voff + float64(k)*leading; b > wTop && b <= limit {
						cands = append(cands, b)
					}
				}
			}
			cells = append(cells, cs)
		}
	}
	// 行边界也是候选
	for _, b := range cum[1 : len(cum)-1] {
		if b > wTop && b <= limit {
			cands = append(cands, b)
		}
	}
	sortFloatsDesc(cands)
	const eps = 1e-4
	for _, p := range cands {
		clean := true
		for _, cs := range cells {
			if p <= cs.ct+eps || p >= cs.cb-eps {
				continue // 不在单元格内部
			}
			if !cs.hasText {
				continue // 空单元格
			}
			if p <= cs.textStart+eps || p >= cs.textEnd-eps {
				continue // 文本区之外（内边距区）
			}
			// 文本区内部：必须恰好落在某条文本行边界上
			k := (p - cs.textStart) / cs.leading
			if d := k - math.Round(k); d > eps || d < -eps {
				clean = false // 切断文本行
				break
			}
		}
		if clean {
			return p
		}
	}
	return 0
}

// sortFloatsDesc 降序插入排序（候选数量有限）。
func sortFloatsDesc(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] > a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// breakStart 返回覆盖第 j 行的跨行块的起始行。
func (t *Table) breakStart(j int, grid [][]placedCell) int {
	start := j
	for r := 0; r < j; r++ {
		for _, c := range grid[r] {
			if c.RowSpan > 1 && r < j && j < r+c.RowSpan && r < start {
				start = r
			}
		}
	}
	return start
}

// rowHeights 计算各行高度：单行单元格先定高，跨行单元格按需撑高末行。
func (t *Table) rowHeights(grid [][]placedCell, widths []float64) []float64 {
	n := len(t.Rows)
	hs := make([]float64, n)
	for r, cells := range grid {
		maxH := t.minRowHeight()
		for _, c := range cells {
			if c.RowSpan > 1 {
				continue
			}
			if h := t.cellHeight(c, spanWidth(widths, c.col, c.ColSpan)); h > maxH {
				maxH = h
			}
		}
		hs[r] = maxH
	}
	// 跨行单元格：需求高度超过所跨行高之和时，把差额补到最后一行
	for _, cells := range grid {
		for _, c := range cells {
			if c.RowSpan <= 1 {
				continue
			}
			need := t.cellHeight(c, spanWidth(widths, c.col, c.ColSpan))
			have := 0.0
			for r := c.row; r < c.row+c.RowSpan; r++ {
				have += hs[r]
			}
			if need > have {
				hs[c.row+c.RowSpan-1] += need - have
			}
		}
	}
	return hs
}

// minRowHeight 空行最小行高。
func (t *Table) minRowHeight() float64 {
	f := t.bodyFont()
	return f.LineHeight(t.fontSize()) + 2*t.padding()
}

// cellHeight 单元格内容所需高度（按给定宽度换行；字体/字号取单元格覆盖）。
func (t *Table) cellHeight(c placedCell, w float64) float64 {
	f, size, pad := t.bodyFont(), t.fontSize(), t.padding()
	if c.Font != nil {
		f = c.Font
	}
	if c.Size > 0 {
		size = c.Size
	}
	lines := len(text.Wrap(f, size, w-2*pad, c.Text))
	return float64(lines)*f.LineHeight(size) + 2*pad
}

func spanWidth(widths []float64, col, span int) float64 {
	w := 0.0
	for i := col; i < col+span && i < len(widths); i++ {
		w += widths[i]
	}
	return w
}

// pageRun 一页的绘制任务：普通模式渲染行区间 [start, end)；
// split 模式渲染拆分块 [start, end) 中落在窗口 [wTop, wBot) 内的片段
// （窗口坐标为相对 start 行起点的累计高度）。
type pageRun struct {
	page       *page.Page
	top        float64
	start, end int
	split      bool
	wTop, wBot float64
}

// cellText 布局单元格文本：返回字体、字号、行距、换行结果与
// 首行行框顶部相对单元格顶部的偏移（跨行单元格与表头行垂直居中，其余顶对齐）。
func (t *Table) cellText(c placedCell, w, ch float64, header bool) (font.Resource, float64, float64, []string, float64) {
	f, size, pad := t.bodyFont(), t.fontSize(), t.padding()
	if c.Font != nil {
		f = c.Font
	}
	if c.Size > 0 {
		size = c.Size
	}
	leading := f.LineHeight(size)
	lines := text.Wrap(f, size, w-2*pad, c.Text)
	textH := float64(len(lines)) * leading
	voff := pad
	if (c.RowSpan > 1 || header) && textH+2*pad < ch {
		voff = pad + (ch-2*pad-textH)/2
	}
	return f, size, leading, lines, voff
}

// renderRun 渲染一页：表头（表头行或列标题）+ [start, end) 内容行。
func (t *Table) renderRun(p *page.Page, x, top, w float64, widths, heights []float64,
	grid [][]placedCell, headerH float64, headerRows, start, end int) {
	y := top
	if headerRows > 0 {
		t.renderRows(p, x, y, widths, heights, grid, 0, headerRows, true)
		y -= headerH
	} else if headerH > 0 {
		t.renderHeader(p, x, y, w, widths, headerH)
		y -= headerH
	}
	t.renderRows(p, x, y, widths, heights, grid, start, end, false)
}

// renderRows 渲染 [start, end) 行；header 为 true 时应用表头样式（底色/文本色）。
func (t *Table) renderRows(p *page.Page, x, top float64, widths, heights []float64,
	grid [][]placedCell, start, end int, header bool) {
	pad := t.padding()
	y := top
	for r := start; r < end; r++ {
		rh := heights[r]
		if !header && t.RowBg != nil {
			p.Save().SetFillColor(t.RowBg)
			p.FillRect(x, y-rh, spanWidth(widths, 0, len(widths)), rh)
			p.Restore()
		}
		// 底色、边框与文本（按单元格，含合并区）
		for _, c := range grid[r] {
			cx := x + spanWidth(widths, 0, c.col)
			cw := spanWidth(widths, c.col, c.ColSpan)
			ch := 0.0
			for rr := r; rr < r+c.RowSpan; rr++ {
				ch += heights[rr]
			}
			bg := c.Bg
			if bg == nil && header {
				bg = t.headerBg()
			}
			if bg != nil {
				p.Save().SetFillColor(bg)
				p.FillRect(cx, y-ch, cw, ch)
				p.Restore()
			}
			p.Save().SetStrokeColor(t.border()).SetLineWidth(t.borderW())
			p.StrokeRect(cx, y-ch, cw, ch)
			p.Restore()
			if c.Text != "" {
				txtColor := c.Color
				if txtColor == nil {
					if header {
						txtColor = t.headerTextColor()
					} else {
						txtColor = t.textColor()
					}
				}
				f, size, leading, lines, voff := t.cellText(c, cw, ch, header)
				ty := y - voff - f.Ascent(size)
				align := t.Columns[c.col].Align
				if c.Align != nil {
					align = *c.Align
				}
				p.SetFillColor(txtColor)
				for _, line := range lines {
					xoff := text.OffsetX(f, size, cw-2*pad, line, align)
					p.DrawText(f, size, cx+pad+xoff, ty, line)
					ty -= leading
				}
			}
		}
		y -= rh
	}
}

// renderSplitRun 渲染拆分块的一个窗口片段：表头 + 块 [start, end) 中
// 落在窗口 [wTop, wBot) 内的部分。单元格背景/边框按可见片段绘制；
// 文本行按行框顶部归属窗口（每行只在一页输出），并以片段矩形裁剪防溢出。
func (t *Table) renderSplitRun(p *page.Page, x, top float64, widths, heights []float64,
	grid [][]placedCell, headerH float64, headerRows, start, end int, wTop, wBot float64) {
	y := top
	if headerRows > 0 {
		t.renderRows(p, x, y, widths, heights, grid, 0, headerRows, true)
		y -= headerH
	} else if headerH > 0 {
		t.renderHeader(p, x, y, spanWidth(widths, 0, len(widths)), widths, headerH)
		y -= headerH
	}
	pad := t.padding()

	// 块内累计高度（相对块起点）
	cum := make([]float64, end-start+1)
	for r := start; r < end; r++ {
		cum[r-start+1] = cum[r-start] + heights[r]
	}
	// pageY 把块内坐标 c 映射为页面 y（片段上边缘）
	pageY := func(c float64) float64 { return y - (c - wTop) }
	totalW := spanWidth(widths, 0, len(widths))

	// 行底色片段
	if t.RowBg != nil {
		for r := start; r < end; r++ {
			vt := maxF(cum[r-start], wTop)
			vb := minF(cum[r-start+1], wBot)
			if vb <= vt {
				continue
			}
			p.Save().SetFillColor(t.RowBg)
			p.FillRect(x, pageY(vb), totalW, vb-vt)
			p.Restore()
		}
	}

	// 单元格片段（含跨行合并区）
	for r := start; r < end; r++ {
		for _, c := range grid[r] {
			ct := cum[c.row-start]
			cb := ct
			for rr := c.row; rr < c.row+c.RowSpan; rr++ {
				cb += heights[rr]
			}
			vt := maxF(ct, wTop)
			vb := minF(cb, wBot)
			if vb <= vt {
				continue
			}
			cx := x + spanWidth(widths, 0, c.col)
			cw := spanWidth(widths, c.col, c.ColSpan)
			if c.Bg != nil {
				p.Save().SetFillColor(c.Bg)
				p.FillRect(cx, pageY(vb), cw, vb-vt)
				p.Restore()
			}
			p.Save().SetStrokeColor(t.border()).SetLineWidth(t.borderW())
			p.StrokeRect(cx, pageY(vb), cw, vb-vt)
			p.Restore()
			if c.Text == "" {
				continue
			}
			txtColor := c.Color
			if txtColor == nil {
				txtColor = t.textColor()
			}
			f, size, leading, lines, voff := t.cellText(c, cw, cb-ct, false)
			align := t.Columns[c.col].Align
			if c.Align != nil {
				align = *c.Align
			}
			p.Save()
			p.Content.Rect(cx, pageY(vb), cw, vb-vt).Clip().EndPath()
			p.SetFillColor(txtColor)
			for k, line := range lines {
				lineTop := ct + voff + float64(k)*leading
				if lineTop < wTop || lineTop >= wBot {
					continue // 该行归属其它片段页
				}
				xoff := text.OffsetX(f, size, cw-2*pad, line, align)
				p.DrawText(f, size, cx+pad+xoff, pageY(lineTop)-f.Ascent(size), line)
			}
			p.Restore()
		}
	}
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// renderHeader 渲染表头行（灰底、加粗、居中）。
func (t *Table) renderHeader(p *page.Page, x, y, w float64, widths []float64, hh float64) {
	p.Save().SetFillColor(t.headerBg())
	p.FillRect(x, y-hh, w, hh)
	p.Restore()
	p.Save().SetStrokeColor(t.border()).SetLineWidth(t.borderW())
	cx := x
	for _, cw := range widths {
		p.StrokeRect(cx, y-hh, cw, hh)
		cx += cw
	}
	p.Restore()
	f := t.headFont()
	size := t.headerSize()
	cx = x
	p.SetFillColor(t.headerTextColor())
	for i, c := range t.Columns {
		if c.Title != "" {
			tw := f.TextWidth(c.Title, size)
			tx := cx + (widths[i]-tw)/2
			p.DrawText(f, size, tx, y-t.padding()-f.Ascent(size), c.Title)
		}
		cx += widths[i]
	}
}
