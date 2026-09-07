package jsongen

import (
	"fmt"
	"unicode/utf8"

	pdf "gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/table"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// engine 流式排版引擎：维护页游标，按内容块顺序排版并自动分页。
type engine struct {
	doc     *pdf.Document
	fonts   *FontRegistry
	styles  map[string]styleSpec
	size    page.Size
	margins [4]float64 // top, right, bottom, left

	p     *page.Page
	y     float64 // 下一内容的上边缘
	atTop bool    // 当前页尚无内容（用于跳过段前距）

	// pendingAfter 前一段落的段后距：与下一段的段前距折叠（取较大者，
	// 与 CSS margin collapsing 一致）；spacer/table 到达时消耗。
	pendingAfter float64
}

func (e *engine) left() float64     { return e.margins[3] }
func (e *engine) bottom() float64   { return e.margins[2] }
func (e *engine) contentW() float64 { return e.size.W - e.margins[3] - e.margins[1] }

func (e *engine) addPage() {
	e.p = e.doc.AddPage(e.size)
	e.y = e.size.H - e.margins[0]
	e.atTop = true
	e.pendingAfter = 0
}

// ---------------------------------------------------------------------------
// 样式解析

// style 解析后的文本样式。
type style struct {
	font        font.Resource
	size        float64
	colr        color.Color
	align       text.Alignment
	lineHeight  float64
	indent      float64 // 首行缩进（×字号宽度）
	spaceBefore float64
	spaceAfter  float64
	underline   bool
	strike      bool
}

// resolveStyle 解析样式：默认值 ← 命名样式 ← 内联覆盖。
func (e *engine) resolveStyle(name string, ov *styleOverride) (style, error) {
	st := style{size: 12, align: text.AlignLeft, lineHeight: 1.5}
	fontID := ""
	if name != "" {
		ns, ok := e.styles[name]
		if !ok {
			return st, fmt.Errorf("样式 %q 未定义", name)
		}
		fontID = ns.Font
		st.size = or(ns.Size, st.size)
		st.lineHeight = or(ns.LineHeight, st.lineHeight)
		st.indent = ns.Indent
		st.spaceBefore = ns.SpaceBefore
		st.spaceAfter = ns.SpaceAfter
		st.underline = ns.Underline
		st.strike = ns.StrikeThrough
		var err error
		if st.colr, err = parseColor(ns.Color); err != nil {
			return st, err
		}
		if st.align, err = parseAlign(ns.Align, st.align); err != nil {
			return st, err
		}
	}
	if ov != nil {
		if ov.Font != nil {
			fontID = *ov.Font
		}
		if ov.Size != nil {
			st.size = *ov.Size
		}
		if ov.LineHeight != nil {
			st.lineHeight = *ov.LineHeight
		}
		if ov.Indent != nil {
			st.indent = *ov.Indent
		}
		if ov.SpaceBefore != nil {
			st.spaceBefore = *ov.SpaceBefore
		}
		if ov.SpaceAfter != nil {
			st.spaceAfter = *ov.SpaceAfter
		}
		if ov.Underline != nil {
			st.underline = *ov.Underline
		}
		if ov.StrikeThrough != nil {
			st.strike = *ov.StrikeThrough
		}
		if ov.Color != nil {
			c, err := parseColor(*ov.Color)
			if err != nil {
				return st, err
			}
			st.colr = c
		}
		if ov.Align != nil {
			a, err := parseAlign(*ov.Align, st.align)
			if err != nil {
				return st, err
			}
			st.align = a
		}
	}
	if fontID == "" {
		return st, fmt.Errorf("样式 %q 未指定字体", name)
	}
	f, err := e.fonts.lookup(fontID)
	if err != nil {
		return st, err
	}
	st.font = f
	if st.colr == nil {
		st.colr = color.Black
	}
	return st, nil
}

func or(v, def float64) float64 {
	if v != 0 {
		return v
	}
	return def
}

// ---------------------------------------------------------------------------
// 段落排版

// token 最小排版单元：单字符（CJK 等可断行）、拉丁词（不可断，超宽硬拆）、
// 空格（可断行）或显式换行符。
type token struct {
	text    string
	st      style
	width   float64
	space   bool
	newline bool
}

func (t token) word() bool { return !t.space && !t.newline }

// tokenize 切分文本为 token 序列。拉丁字母/数字/西文标点（U+2E80 以下）
// 聚合为词，其余字符（CJK 等）逐字成 token，与 text 包“中文按字断行”一致。
func tokenize(s string, st style) []token {
	var toks []token
	word := []rune{}
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		t := string(word)
		toks = append(toks, token{text: t, st: st, width: st.font.TextWidth(t, st.size)})
		word = word[:0]
	}
	for _, r := range s {
		switch {
		case r == '\n':
			flushWord()
			toks = append(toks, token{newline: true, st: st})
		case r == ' ':
			flushWord()
			toks = append(toks, token{text: " ", st: st, width: st.font.TextWidth(" ", st.size), space: true})
		case r < 0x2E80:
			word = append(word, r)
		default:
			flushWord()
			t := string(r)
			toks = append(toks, token{text: t, st: st, width: st.font.TextWidth(t, st.size)})
		}
	}
	flushWord()
	return toks
}

// line 一行排版结果。
type line struct {
	toks  []token
	width float64
	hard  bool // 由显式换行结束（两端对齐时应视作末行）
}

// wrapper 贪心换行器。
type wrapper struct {
	maxWidth float64 // 内容区宽度
	indent   float64 // 首行缩进（pt）
	n        int     // 当前行号
	cur      []token
	curW     float64
	lines    []line
}

func (w *wrapper) avail() float64 {
	if w.n == 0 {
		return w.maxWidth - w.indent
	}
	return w.maxWidth
}

func (w *wrapper) flush(hard bool) {
	// 去掉行尾空格
	for len(w.cur) > 0 && w.cur[len(w.cur)-1].space {
		w.curW -= w.cur[len(w.cur)-1].width
		w.cur = w.cur[:len(w.cur)-1]
	}
	w.lines = append(w.lines, line{toks: w.cur, width: w.curW, hard: hard})
	w.cur, w.curW = nil, 0
	w.n++
}

func (w *wrapper) push(tk token) {
	switch {
	case tk.newline:
		w.flush(true)
	case tk.space:
		if len(w.cur) == 0 {
			return // 丢弃行首空格
		}
		if w.curW+tk.width > w.avail() {
			w.flush(false) // 断行处空格丢弃
			return
		}
		w.cur = append(w.cur, tk)
		w.curW += tk.width
	default:
		for {
			if w.curW+tk.width <= w.avail() {
				w.cur = append(w.cur, tk)
				w.curW += tk.width
				return
			}
			if len(w.cur) > 0 {
				w.flush(false)
				continue
			}
			// 单 token 超宽：多字符词按 rune 硬拆
			if utf8.RuneCountInString(tk.text) > 1 {
				head, rest := splitToken(tk, w.avail())
				w.cur = append(w.cur, head)
				w.curW += head.width
				w.flush(false)
				tk = rest
				continue
			}
			// 单字符也超宽：溢出绘制
			w.cur = append(w.cur, tk)
			w.flush(false)
			return
		}
	}
}

// splitToken 切出恰好不超过 maxW 的 rune 前缀。
func splitToken(tk token, maxW float64) (head, rest token) {
	w := 0.0
	for i, r := range tk.text {
		rw := tk.st.font.TextWidth(string(r), tk.st.size)
		if w+rw > maxW && i > 0 {
			return token{text: tk.text[:i], st: tk.st, width: w},
				token{text: tk.text[i:], st: tk.st, width: tk.width - w}
		}
		w += rw
	}
	return tk, token{st: tk.st}
}

// wrapTokens 将 token 序列贪心换行为行序列。
func wrapTokens(toks []token, maxWidth, firstIndent float64) []line {
	w := &wrapper{maxWidth: maxWidth, indent: firstIndent}
	for _, tk := range toks {
		w.push(tk)
	}
	if len(w.cur) > 0 || len(w.lines) == 0 {
		w.flush(false)
	}
	return w.lines
}

// paragraph 排版段落块。
func (e *engine) paragraph(b *blockSpec) error {
	lines, st, err := e.prepareParagraph(b, e.contentW())
	if err != nil {
		return err
	}
	if lines == nil {
		return nil // 空段落
	}
	if !e.atTop {
		e.y -= maxFloat(e.pendingAfter, st.spaceBefore)
	}
	e.pendingAfter = 0
	// 孤行标题控制：单行且段前距较大（标题性质）的段落，
	// 若本页放不下「标题 + 一行正文」则整段推到下一页。
	if len(lines) == 1 && st.spaceBefore >= 8 {
		lineH := maxSize(lines[0], st) * st.lineHeight
		if e.y-lineH-textHeight(lines[0], st)*2 < e.bottom() {
			e.addPage()
		}
	}
	for i, ln := range lines {
		lineH := maxSize(ln, st) * st.lineHeight
		// 分页判定用行内文字实际高度（ascent+descent）而非整行距：
		// 与浏览器一致，文字底部越过页底边界才换页。
		if e.y-textHeight(ln, st) < e.bottom() {
			e.addPage()
		}
		last := i == len(lines)-1
		e.drawLine(ln, st, e.left(), e.contentW(), i == 0, !last && !ln.hard)
		e.y -= lineH
	}
	e.pendingAfter = st.spaceAfter
	e.atTop = false
	return nil
}

// prepareParagraph 解析样式并换行；空段落返回 nil 行。
func (e *engine) prepareParagraph(b *blockSpec, width float64) ([]line, style, error) {
	st, err := e.resolveStyle(b.Style, &b.styleOverride)
	if err != nil {
		return nil, st, err
	}
	runs := b.Runs
	if len(runs) == 0 {
		if b.Text == "" {
			return nil, st, nil // 空段落
		}
		runs = []runSpec{{Text: b.Text}}
	}
	var toks []token
	for i := range runs {
		r := &runs[i]
		rst := st
		if r.Style != "" || r.styleOverride != (styleOverride{}) {
			if rst, err = e.resolveStyle(first(r.Style, b.Style), &r.styleOverride); err != nil {
				return nil, st, err
			}
		}
		toks = append(toks, tokenize(sanitizeText(r.Text), rst)...)
	}
	return wrapTokens(toks, width, st.indent*st.size), st, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func maxSize(ln line, st style) float64 {
	m := st.size
	for _, tk := range ln.toks {
		if tk.st.size > m {
			m = tk.st.size
		}
	}
	return m
}

// textHeight 行内文字实际高度（最大 ascent + |descent|）。
func textHeight(ln line, st style) float64 {
	h := st.font.Ascent(st.size) - st.font.Descent(st.size)
	for _, tk := range ln.toks {
		if th := tk.st.font.Ascent(tk.st.size) - tk.st.font.Descent(tk.st.size); th > h {
			h = th
		}
	}
	return h
}

// drawLine 绘制一行：对齐 / 两端对齐（空格拉伸），按相同样式合并段绘制。
// baseX/width 为可用区左缘与宽度（columns 块内为栏宽）。
func (e *engine) drawLine(ln line, st style, baseX, width float64, firstLine, justify bool) {
	indent := 0.0
	if firstLine {
		indent = st.indent * st.size
	}
	x0 := baseX + indent
	avail := width - indent

	xoff, extraSpace := 0.0, 0.0
	nSpaces := 0
	for _, tk := range ln.toks {
		if tk.space {
			nSpaces++
		}
	}
	switch st.align {
	case text.AlignCenter:
		xoff = (avail - ln.width) / 2
	case text.AlignRight:
		xoff = avail - ln.width
	case text.AlignJustify:
		if justify && nSpaces > 0 {
			extraSpace = (avail - ln.width) / float64(nSpaces)
		}
	}

	maxAsc := st.font.Ascent(st.size)
	for _, tk := range ln.toks {
		if a := tk.st.font.Ascent(tk.st.size); a > maxAsc {
			maxAsc = a
		}
	}
	baseline := e.y - maxAsc

	x := x0 + xoff
	for i := 0; i < len(ln.toks); {
		// 合并相同样式的连续 token
		j := i
		seg := ""
		segW := 0.0
		segSpaces := 0
		for j < len(ln.toks) && sameStyle(ln.toks[i], ln.toks[j]) {
			seg += ln.toks[j].text
			segW += ln.toks[j].width
			if ln.toks[j].space {
				segSpaces++
			}
			j++
		}
		tk := ln.toks[i]
		e.p.SetFillColor(tk.st.colr)
		e.p.DrawText(tk.st.font, tk.st.size, x, baseline, seg)
		if tk.st.underline {
			e.p.Underline(tk.st.font, tk.st.size, x, baseline, seg)
		}
		if tk.st.strike {
			e.p.StrikeThrough(tk.st.font, tk.st.size, x, baseline, seg)
		}
		x += segW + extraSpace*float64(segSpaces)
		i = j
	}
}

func sameStyle(a, b token) bool {
	return a.st.font == b.st.font && a.st.size == b.st.size &&
		a.st.colr == b.st.colr && a.st.underline == b.st.underline &&
		a.st.strike == b.st.strike && a.space == b.space
}

// ---------------------------------------------------------------------------
// 其他内容块

func (e *engine) spacer(h float64) {
	if h <= 0 || e.atTop {
		return // 页首的间隔被折叠掉
	}
	if e.y-e.pendingAfter-h < e.bottom() {
		e.addPage()
		return
	}
	e.y -= e.pendingAfter + h // 段后距并入间隔
	e.pendingAfter = 0
}

// columns 多栏并排布局：各栏独立排版（仅支持 paragraph/spacer 子块），
// 块整体高度取最高栏；整栏放不下当前页时整体移至新页（栏内不再分页）。
func (e *engine) columns(b *blockSpec) error {
	n := len(b.Children)
	if n == 0 {
		return fmt.Errorf("columns 块缺少 children")
	}
	gap := b.Gap
	availW := e.contentW() - gap*float64(n-1)
	widths := make([]float64, n)
	fixed, auto := 0.0, 0
	for i := range widths {
		if i < len(b.Widths) && b.Widths[i] > 0 {
			widths[i] = b.Widths[i]
			fixed += widths[i]
		} else {
			auto++
		}
	}
	if auto > 0 {
		share := (availW - fixed) / float64(auto)
		if share < 0 {
			share = 0
		}
		for i := range widths {
			if widths[i] == 0 {
				widths[i] = share
			}
		}
	}

	type colBlock struct {
		lines     []line
		st        style
		gapBefore float64
		spacer    float64
	}
	layouts := make([][]colBlock, n)
	heights := make([]float64, n)
	for ci, col := range b.Children {
		pending := e.pendingAfter // 栏首块与块前段后距折叠
		for bi := range col {
			blk := &col[bi]
			switch blk.Type {
			case "paragraph":
				lines, st, err := e.prepareParagraph(blk, widths[ci])
				if err != nil {
					return err
				}
				if lines == nil {
					continue
				}
				gb := maxFloat(pending, st.spaceBefore)
				pending = st.spaceAfter
				h := gb
				for _, ln := range lines {
					h += maxSize(ln, st) * st.lineHeight
				}
				layouts[ci] = append(layouts[ci], colBlock{lines: lines, st: st, gapBefore: gb})
				heights[ci] += h
			case "spacer":
				layouts[ci] = append(layouts[ci], colBlock{spacer: blk.Height + pending})
				heights[ci] += blk.Height + pending
				pending = 0
			default:
				return fmt.Errorf("columns 内不支持块类型 %q（仅 paragraph/spacer）", blk.Type)
			}
		}
	}
	totalH := 0.0
	for _, h := range heights {
		if h > totalH {
			totalH = h
		}
	}
	if totalH == 0 {
		return nil
	}

	e.pendingAfter = 0
	if e.atTop {
		// 页首：栏首块的 gapBefore 已在上方折叠中计算
	} else if e.y-totalH < e.bottom() {
		e.addPage()
	}
	y0 := e.y
	for ci, blocks := range layouts {
		baseX := e.left()
		for k := 0; k < ci; k++ {
			baseX += widths[k] + gap
		}
		e.y = y0
		for _, blk := range blocks {
			if blk.spacer > 0 {
				e.y -= blk.spacer
				continue
			}
			e.y -= blk.gapBefore
			for i, ln := range blk.lines {
				lineH := maxSize(ln, blk.st) * blk.st.lineHeight
				last := i == len(blk.lines)-1
				e.drawLine(ln, blk.st, baseX, widths[ci], i == 0, !last && !ln.hard)
				e.y -= lineH
			}
		}
	}
	e.y = y0 - totalH
	e.atTop = false
	return nil
}

func (e *engine) table(b *blockSpec) error {
	if len(b.Columns) == 0 {
		return fmt.Errorf("表格缺少列定义")
	}
	st, err := e.resolveStyle(b.Style, &b.styleOverride)
	if err != nil {
		return err
	}

	cols := make([]table.Column, len(b.Columns))
	for i, c := range b.Columns {
		a, err := parseAlign(c.Align, text.AlignLeft)
		if err != nil {
			return err
		}
		cols[i] = table.Column{Width: c.Width, Align: a}
	}
	tbl := table.New(cols...)
	tbl.Font = st.font
	tbl.FontSize = st.size
	tbl.TextColor = st.colr
	tbl.HeaderFont = st.font // 表头默认同字体（不加粗）
	tbl.HeaderSize = st.size
	if b.Padding > 0 {
		tbl.Padding = b.Padding
	}
	if b.Border.Width > 0 {
		tbl.BorderW = b.Border.Width
	}
	if c, err := parseColor(b.Border.Color); err != nil {
		return err
	} else if c != nil {
		tbl.Border = c
	}
	if c, err := parseColor(b.Header.Background); err != nil {
		return err
	} else if c != nil {
		tbl.HeaderBg = c
	}
	headerAlign, err := parseAlign(b.Header.Align, text.AlignCenter)
	if err != nil {
		return err
	}
	var headerColor color.Color
	if b.Header.Style != "" {
		hst, err := e.resolveStyle(b.Header.Style, nil)
		if err != nil {
			return err
		}
		headerColor = hst.colr
		tbl.HeaderColor = hst.colr
	}

	headerRows := b.HeaderRows
	for ri, row := range b.Rows {
		cells := make([]table.Cell, 0, len(row))
		for _, cs := range row {
			cell := table.C(sanitizeText(cs.Text))
			if cs.ColSpan > 0 {
				cell.ColSpan = cs.ColSpan
			}
			if cs.RowSpan > 0 {
				cell.RowSpan = cs.RowSpan
			}
			if cs.Style != "" {
				cst, err := e.resolveStyle(cs.Style, nil)
				if err != nil {
					return err
				}
				cell.Color = cst.colr
				cell.Font = cst.font // 与表格默认相同亦无妨
				cell.Size = cst.size
			}
			if c, err := parseColor(cs.Color); err != nil {
				return err
			} else if c != nil {
				cell.Color = c
			}
			if c, err := parseColor(cs.Background); err != nil {
				return err
			} else if c != nil {
				cell.Bg = c
			}
			if cs.Align != "" {
				a, err := parseAlign(cs.Align, text.AlignLeft)
				if err != nil {
					return err
				}
				cell.Align = &a
			}
			if ri < headerRows {
				if cell.Align == nil {
					a := headerAlign
					cell.Align = &a
				}
				if cell.Color == nil && headerColor != nil {
					cell.Color = headerColor
				}
			}
			cells = append(cells, cell)
		}
		tbl.AddRowCells(cells...)
	}
	tbl.HeaderRows = headerRows

	if !e.atTop {
		e.y -= e.pendingAfter // 表格消耗前段段后距
	}
	e.pendingAfter = 0
	last, endY := tbl.Draw(e.p, e.left(), e.y, e.contentW(), e.bottom(),
		func() (*page.Page, float64) {
			e.addPage()
			return e.p, e.y
		})
	e.p = last
	e.y = endY
	e.atTop = false
	return nil
}
