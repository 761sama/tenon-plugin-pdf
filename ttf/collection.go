package ttf

import "fmt"

// TrueType Collection（.ttc）支持。
//
// TTC 文件结构：'ttcf' 头 + 各字体的 sfnt 目录偏移数组（相对文件起始），
// 多个字体可共享表数据。解析时只需把表目录的读取起点换成对应偏移，
// 表内偏移仍是相对文件起始的绝对偏移，因此其余解析逻辑不变。

const ttcfTag = 0x74746366 // 'ttcf'

// IsCollection 报告数据是否为 TrueType Collection（.ttc）。
func IsCollection(data []byte) bool {
	return len(data) >= 4 && u32(data, 0) == ttcfTag
}

// CollectionCount 返回 TTC 集合中包含的字体数量；非集合返回 0。
func CollectionCount(data []byte) int {
	if !IsCollection(data) || len(data) < 12 {
		return 0
	}
	return int(u32(data, 8))
}

// CollectionNames 返回集合中每个字体的 PostScript 名称（解析失败的项为空串）。
// 用于 CLI 展示等场景；只解析 name 表所需的最小结构。
func CollectionNames(data []byte) []string {
	n := CollectionCount(data)
	names := make([]string, n)
	for i := 0; i < n; i++ {
		if f, err := ParseCollection(data, i); err == nil {
			names[i] = f.PSName()
		}
	}
	return names
}

// ParseCollection 解析 TTC 集合中第 index 个字体（0 起）。
// 若数据不是集合但 index 为 0，则按普通单字体解析。
func ParseCollection(data []byte, index int) (*Font, error) {
	if !IsCollection(data) {
		if index == 0 {
			return Parse(data)
		}
		return nil, fmt.Errorf("ttf: 非字体集合，index %d 越界", index)
	}
	n := CollectionCount(data)
	if index < 0 || index >= n {
		return nil, fmt.Errorf("ttf: 集合含 %d 个字体，index %d 越界", n, index)
	}
	if len(data) < 12+4*n {
		return nil, fmt.Errorf("ttf: TTC 头过短")
	}
	off := u32(data, 12+4*index)
	return parseAt(data, int(off))
}
