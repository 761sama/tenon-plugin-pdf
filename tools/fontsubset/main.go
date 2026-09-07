// fontsubset 是开发工具：从完整 TrueType 字体生成仅含指定字符集的子集字体。
//
// 用法：
//
//	go run ./tools/fontsubset <完整字体.ttf> <字符集.txt> <输出.ttf>
//
// 字符集文件为 UTF-8 文本，其中出现的每个字符都会被纳入子集；
// ASCII 可打印字符（0x20–0x7E）自动包含。用于生成 cmd/tenon-pdf/assets 下
// 随仓库提交的演示用子集字体。
package main

import (
	"fmt"
	"os"

	"gopkg.761sama.com/tenon-plugin-pdf/ttf"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "用法: fontsubset <完整字体.ttf> <字符集.txt> <输出.ttf>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取字体:", err)
		os.Exit(1)
	}
	f, err := ttf.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析字体:", err)
		os.Exit(1)
	}
	charset, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取字符集:", err)
		os.Exit(1)
	}

	seen := map[rune]bool{}
	glyphs := []ttf.GlyphMapping{{Rune: 0, GID: 0}}
	add := func(r rune) {
		if seen[r] {
			return
		}
		seen[r] = true
		gid := f.GlyphIndex(r)
		if gid == 0 {
			fmt.Fprintf(os.Stderr, "警告: %q (U+%04X) 无字形\n", r, r)
			return
		}
		glyphs = append(glyphs, ttf.GlyphMapping{Rune: r, GID: gid})
	}
	for r := rune(0x20); r <= 0x7e; r++ {
		add(r)
	}
	for _, r := range string(charset) {
		add(r)
	}

	out, err := f.Subset(glyphs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "子集化:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[3], out, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "写入:", err)
		os.Exit(1)
	}
	fmt.Printf("子集完成：%d 字形，%d → %d 字节\n", len(glyphs), len(data), len(out))
}
