package ttf

import "sort"

// GSUB 连字替换解析（仅 LigatureSubst，lookup type 4 format 1）。
//
// 取舍：不实现完整的 OpenType  shaping 管线（脚本/语言系统选择、
// ccmp 重排、GSUB 其他 lookup 类型），只收集 'liga' 与 'rlig' 特性引用的
// 连字规则。拉丁文常见连字（fi/fl/ffi/ffl 等）由此覆盖；复杂文字
// （阿拉伯/印度语系）的正确 shaping 超出本库范围。
//
// LigatureSubst format 1 结构：
//   coverage（首字形集合）→ ligatureSets[i]（以该字形开头的连字表）→
//   Ligature{ligGlyph, compCount, component[compCount-1]}（组件为第 2..n 个字形）

// Ligature 一条连字规则：首字形 + Components 依次出现时替换为 Glyph。
type Ligature struct {
	Components []uint16 // 首个字形之后的组件字形 ID（长度为总组件数-1）
	Glyph      uint16   // 连字字形 ID
}

// parseGSUB 解析 GSUB 表，收集连字规则到 f.ligatures。
// 无 GSUB 或无连字规则时 f.ligatures 为 nil。
func (f *Font) parseGSUB() {
	d, err := f.table("GSUB")
	if err != nil || len(d) < 10 {
		return
	}
	featureOff := int(u16(d, 6))
	lookupOff := int(u16(d, 8))
	if featureOff+2 > len(d) || lookupOff+2 > len(d) {
		return
	}

	// 1. FeatureList：找到所有 tag 为 liga/rlig 的特性的 lookup 索引
	var lookupIdx []int
	feat := d[featureOff:]
	count := int(u16(feat, 0))
	for i := 0; i < count; i++ {
		ro := 2 + i*6
		if ro+6 > len(feat) {
			break
		}
		tag := string(feat[ro : ro+4])
		if tag != "liga" && tag != "rlig" {
			continue
		}
		fo := int(u16(feat, ro+4))
		if fo+4 > len(feat) {
			continue
		}
		n := int(u16(feat, fo+2))
		for j := 0; j < n; j++ {
			if fo+4+2*j+2 <= len(feat) {
				lookupIdx = append(lookupIdx, int(u16(feat, fo+4+2*j)))
			}
		}
	}
	if len(lookupIdx) == 0 {
		return
	}

	// 2. LookupList：逐个解析 type 4 lookup
	lookups := d[lookupOff:]
	nLookups := int(u16(lookups, 0))
	for _, idx := range lookupIdx {
		if idx >= nLookups || 2+2*idx+2 > len(lookups) {
			continue
		}
		lo := int(u16(lookups, 2+2*idx))
		f.parseLigatureLookup(lookups, lo)
	}

	// 3. 每组按组件数降序，贪心最长匹配时先试长连字
	for _, ls := range f.ligatures {
		sort.Slice(ls, func(a, b int) bool { return len(ls[a].Components) > len(ls[b].Components) })
	}
}

// parseLigatureLookup 解析单个 lookup（lookups 为 LookupList 起始切片，lo 为表内偏移）。
func (f *Font) parseLigatureLookup(lookups []byte, lo int) {
	if lo+6 > len(lookups) || u16(lookups, lo) != 4 { // 仅 LigatureSubst
		return
	}
	nSub := int(u16(lookups, lo+4))
	for i := 0; i < nSub; i++ {
		if lo+6+2*i+2 > len(lookups) {
			return
		}
		sub := lo + int(u16(lookups, lo+6+2*i))
		f.parseLigatureSubtable(lookups, sub)
	}
}

// parseLigatureSubtable 解析 LigatureSubst format 1 子表。
func (f *Font) parseLigatureSubtable(lookups []byte, sub int) {
	if sub+6 > len(lookups) || u16(lookups, sub) != 1 {
		return
	}
	covOff := int(u16(lookups, sub+2))
	nSets := int(u16(lookups, sub+4))
	cov := coverageGlyphs(lookups, sub+covOff)
	for i := 0; i < nSets && i < len(cov); i++ {
		if sub+6+2*i+2 > len(lookups) {
			return
		}
		setOff := sub + int(u16(lookups, sub+6+2*i))
		f.parseLigatureSet(lookups, setOff, cov[i])
	}
}

// parseLigatureSet 解析以 first 字形开头的连字集合。
func (f *Font) parseLigatureSet(lookups []byte, set int, first uint16) {
	if set+2 > len(lookups) {
		return
	}
	n := int(u16(lookups, set))
	for i := 0; i < n; i++ {
		if set+2+2*i+2 > len(lookups) {
			return
		}
		lo := set + int(u16(lookups, set+2+2*i))
		if lo+4 > len(lookups) {
			return
		}
		ligGlyph := u16(lookups, lo)
		compCount := int(u16(lookups, lo+2))
		if compCount < 2 || lo+2+2*compCount > len(lookups) {
			continue
		}
		comps := make([]uint16, compCount-1)
		for j := range comps {
			comps[j] = u16(lookups, lo+4+2*j)
		}
		if f.ligatures == nil {
			f.ligatures = map[uint16][]Ligature{}
		}
		f.ligatures[first] = append(f.ligatures[first], Ligature{Components: comps, Glyph: ligGlyph})
	}
}

// coverageGlyphs 展开 coverage 表（format 1 列表 / format 2 区间）为字形 ID 切片。
func coverageGlyphs(d []byte, off int) []uint16 {
	if off+4 > len(d) {
		return nil
	}
	switch u16(d, off) {
	case 1:
		n := int(u16(d, off+2))
		out := make([]uint16, 0, n)
		for i := 0; i < n && off+4+2*i+2 <= len(d); i++ {
			out = append(out, u16(d, off+4+2*i))
		}
		return out
	case 2:
		n := int(u16(d, off+2))
		var out []uint16
		for i := 0; i < n && off+4+6*i+6 <= len(d); i++ {
			start := u16(d, off+4+6*i)
			end := u16(d, off+4+6*i+2)
			for g := start; g <= end; g++ {
				out = append(out, g)
				if g == 0xFFFF {
					break
				}
			}
		}
		return out
	}
	return nil
}

// Ligatures 返回连字规则表（首字形 ID → 候选连字，按组件数降序）。
// 无连字规则时返回 nil。
func (f *Font) Ligatures() map[uint16][]Ligature { return f.ligatures }

// HasLigatures 报告字体是否带可用的连字规则。
func (f *Font) HasLigatures() bool { return len(f.ligatures) > 0 }
