// 列宽自适应：根据表头与内容测算列宽。
//
// 算法（类 CSS 的 min/preferred 双测量）：
//   - 每列记录 minW（最长单词宽 + 内边距）与 prefW（单行全文宽 + 内边距）
//   - 定宽列不变；prefW 总和不超过可用宽度时按 prefW 分配并均分剩余，
//     超过时按 prefW 比例压缩（不低于 minW）
//   - 跨列单元格的超额宽度均摊到所跨的自动列
package table

import (
	"strings"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
)

// autoColWidths 按内容自适应测算列宽。
func (t *Table) autoColWidths(total float64, grid [][]placedCell) []float64 {
	n := len(t.Columns)
	pref := make([]float64, n)
	minW := make([]float64, n)
	pad := t.padding()

	type spanExcess struct {
		col, span int
		pref      float64
	}
	var spans []spanExcess

	measure := func(f font.Resource, size float64, s string) (mn, pf float64) {
		if s == "" {
			return 0, 0
		}
		pf = f.TextWidth(s, size) + 2*pad
		for _, word := range strings.Fields(s) {
			if w := f.TextWidth(word, size) + 2*pad; w > mn {
				mn = w
			}
		}
		if mn == 0 {
			mn = pf // 无空格文本（如中文）：不可再拆
		}
		return mn, pf
	}

	// 表头参与测量
	for i, c := range t.Columns {
		if c.Title != "" {
			mn, pf := measure(t.headFont(), t.headerSize(), c.Title)
			if pf > pref[i] {
				pref[i] = pf
			}
			if mn > minW[i] {
				minW[i] = mn
			}
		}
	}

	f, size := t.bodyFont(), t.fontSize()
	for _, cells := range grid {
		for _, c := range cells {
			mn, pf := measure(f, size, c.Text)
			if c.ColSpan == 1 {
				if pf > pref[c.col] {
					pref[c.col] = pf
				}
				if mn > minW[c.col] {
					minW[c.col] = mn
				}
			} else {
				spans = append(spans, spanExcess{c.col, c.ColSpan, pf})
			}
		}
	}

	ws := make([]float64, n)
	fixed := 0.0
	var auto []int
	for i, c := range t.Columns {
		if c.Width > 0 {
			ws[i] = c.Width
			fixed += c.Width
		} else {
			auto = append(auto, i)
		}
	}
	if len(auto) == 0 {
		return ws
	}

	// 跨列单元格：超额宽度均摊到所跨的自动列
	for _, s := range spans {
		have := 0.0
		var autoInSpan []int
		for i := s.col; i < s.col+s.span && i < n; i++ {
			if ws[i] > 0 {
				have += ws[i]
			} else {
				have += pref[i]
				autoInSpan = append(autoInSpan, i)
			}
		}
		if excess := s.pref - have; excess > 0 && len(autoInSpan) > 0 {
			share := excess / float64(len(autoInSpan))
			for _, i := range autoInSpan {
				pref[i] += share
			}
		}
	}

	avail := total - fixed
	if avail < 0 {
		avail = 0
	}
	prefSum := 0.0
	for _, i := range auto {
		prefSum += pref[i]
	}

	if prefSum <= avail {
		// 按内容宽度分配，剩余均分
		leftover := avail - prefSum
		share := leftover / float64(len(auto))
		for _, i := range auto {
			ws[i] = pref[i] + share
		}
		return ws
	}
	// 超出可用宽度：按比例压缩，不低于 minW
	scale := avail / prefSum
	for _, i := range auto {
		w := pref[i] * scale
		if w < minW[i] {
			w = minW[i]
		}
		ws[i] = w
	}
	return ws
}
