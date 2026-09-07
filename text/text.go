// Package text 提供文本排版：按宽度换行、对齐与段落布局计算。
// 只依赖 font 包的度量，不依赖内容流，保持单一职责。
package text

import (
	"strings"

	"gopkg.761sama.com/tenon-plugin-pdf/font"
)

// Alignment 水平对齐方式。
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
	AlignJustify // 两端对齐（通过调整单词间距实现）
)

// Wrap 按最大宽度对文本贪心换行，长单词按字符拆分。保留显式换行符。
func Wrap(f font.Resource, size, maxWidth float64, s string) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		lines = append(lines, wrapLine(f, size, maxWidth, para)...)
	}
	return lines
}

func wrapLine(f font.Resource, size, maxWidth float64, s string) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	space := f.TextWidth(" ", size)
	var lines []string
	cur := words[0]
	curW := f.TextWidth(cur, size)
	// 首个单词超宽则硬拆
	for curW > maxWidth && len(cur) > 1 {
		head, rest := splitWord(f, size, maxWidth, cur)
		lines = append(lines, head)
		cur = rest
		curW = f.TextWidth(cur, size)
	}
	for _, w := range words[1:] {
		ww := f.TextWidth(w, size)
		for ww > maxWidth && len(w) > 1 { // 长单词硬拆
			head, rest := splitWord(f, size, maxWidth, w)
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
				curW = 0
			}
			lines = append(lines, head)
			w = rest
			ww = f.TextWidth(w, size)
		}
		if cur == "" {
			cur, curW = w, ww
			continue
		}
		if curW+space+ww <= maxWidth {
			cur += " " + w
			curW += space + ww
		} else {
			lines = append(lines, cur)
			cur, curW = w, ww
		}
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

// splitWord 将单词拆成"恰好不超过 maxWidth 的前缀"与剩余部分。
func splitWord(f font.Resource, size, maxWidth float64, word string) (head, rest string) {
	w := 0.0
	for i, r := range word {
		rw := float64(f.WidthOf(string(r))) * size / 1000
		if w+rw > maxWidth && i > 0 {
			return word[:i], word[i:]
		}
		w += rw
	}
	return word, ""
}

// OffsetX 计算一行文本在指定宽度与对齐方式下的水平起始偏移。
func OffsetX(f font.Resource, size, width float64, line string, align Alignment) float64 {
	lw := f.TextWidth(line, size)
	switch align {
	case AlignCenter:
		return (width - lw) / 2
	case AlignRight:
		return width - lw
	default:
		return 0
	}
}

// JustifyWordSpace 计算两端对齐所需的额外单词间距。
// 仅一行且不含空格时返回 0；末行不应调用。
func JustifyWordSpace(f font.Resource, size, width float64, line string) float64 {
	spaces := strings.Count(line, " ")
	if spaces == 0 {
		return 0
	}
	return (width - f.TextWidth(line, size)) / float64(spaces)
}
