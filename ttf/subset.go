package ttf

import (
	"fmt"
	"sort"
)

// GlyphMapping 子集中的一个字形：原字体字形 ID 与其 Unicode 码点（用于重建 cmap）。
// Rune 为 0 表示该字形无 cmap 映射（如 .notdef）。
type GlyphMapping struct {
	Rune rune
	GID  uint16
}

// Subset 按给定顺序重建字体：输出字体中字形 i 对应 glyphs[i]。
// glyphs[0] 应为 .notdef（GID 0）。复合字形的组件会自动纳入（追加到末尾）。
// 输出为合法的可独立使用的 TrueType 文件。
func (f *Font) Subset(glyphs []GlyphMapping) ([]byte, error) {
	if len(glyphs) == 0 || glyphs[0].GID != 0 {
		return nil, fmt.Errorf("ttf: 子集首个字形必须是 .notdef（GID 0）")
	}

	// 1. 组件闭包
	newGidOf := make(map[uint16]int, len(glyphs))
	order := make([]uint16, 0, len(glyphs)+16)
	for _, g := range glyphs {
		if _, ok := newGidOf[g.GID]; !ok {
			newGidOf[g.GID] = len(order)
			order = append(order, g.GID)
		}
	}
	for i := 0; i < len(order); i++ {
		d := f.glyphData(order[i])
		if len(d) < 10 || int16(u16(d, 0)) >= 0 {
			continue
		}
		comps, err := compositeComponents(d)
		if err != nil {
			return nil, err
		}
		for _, c := range comps {
			if _, ok := newGidOf[c.GID]; !ok {
				newGidOf[c.GID] = len(order)
				order = append(order, c.GID)
			}
		}
	}

	// 2. 重建 glyf + loca（long 格式，2 字节对齐；复合组件 ID 重映射）
	var glyf []byte
	loca := make([]uint32, len(order)+1)
	for i, oldGid := range order {
		d := f.glyphData(oldGid)
		if len(d) > 0 && int16(u16(d, 0)) < 0 {
			comps, err := compositeComponents(d)
			if err != nil {
				return nil, err
			}
			d = append([]byte(nil), d...)
			for _, c := range comps {
				putU16(d, c.Offset, uint16(newGidOf[c.GID]))
			}
		}
		loca[i] = uint32(len(glyf))
		glyf = append(glyf, d...)
		if len(glyf)%2 != 0 {
			glyf = append(glyf, 0)
		}
	}
	loca[len(order)] = uint32(len(glyf))

	// 3. 重建 hmtx（全部完整记录）
	hmtx := make([]byte, len(order)*4)
	for i, oldGid := range order {
		putU16(hmtx, i*4, f.advances[oldGid])
		putU16(hmtx, i*4+2, uint16(f.lsbs[oldGid]))
	}

	// 4. 重建 cmap（format 4；存在非 BMP 码点时附加 format 12）
	cmap, err := buildCmap(glyphs, newGidOf)
	if err != nil {
		return nil, err
	}

	// 5. 其余表：复制或修补
	patch := func(tag string, mutate func([]byte)) ([]byte, bool) {
		raw, err := f.table(tag)
		if err != nil {
			return nil, false
		}
		d := append([]byte(nil), raw...)
		if mutate != nil {
			mutate(d)
		}
		return d, true
	}

	tables := map[string][]byte{}
	tables["glyf"] = glyf
	{
		locaB := make([]byte, len(loca)*4)
		for i, v := range loca {
			putU32(locaB, i*4, v)
		}
		tables["loca"] = locaB
	}
	tables["hmtx"] = hmtx
	tables["cmap"] = cmap
	if d, ok := patch("head", func(d []byte) {
		putU32(d, 8, 0)  // checkSumAdjustment 置 0
		putU16(d, 50, 1) // indexToLocFormat = long
	}); ok {
		tables["head"] = d
	}
	if d, ok := patch("hhea", func(d []byte) {
		putU16(d, 34, uint16(len(order))) // numberOfHMetrics
	}); ok {
		tables["hhea"] = d
	}
	if d, ok := patch("maxp", func(d []byte) {
		putU16(d, 4, uint16(len(order))) // numGlyphs
	}); ok {
		tables["maxp"] = d
	}
	if d, ok := patch("OS/2", nil); ok {
		tables["OS/2"] = d
	}
	if d, ok := patch("name", nil); ok {
		tables["name"] = d
	}
	if d, ok := patch("post", func(d []byte) {
		// 转为 format 3.0（不携带字形名）
		putU32(d, 0, 0x00030000)
	}); ok {
		if len(d) > 32 {
			d = d[:32]
		}
		tables["post"] = d
	}
	if d, ok := patch("gasp", nil); ok {
		tables["gasp"] = d
	}

	return assemble(tables)
}

// buildCmap 生成 cmap 表：BMP 码点走 format 4 子表；存在非 BMP 码点
// （>0xFFFF，如扩展汉字/emoji）时附加 format 12 子表，
// 使子集字体作为独立 TTF 使用时不丢失非 BMP 映射。
func buildCmap(glyphs []GlyphMapping, newGidOf map[uint16]int) ([]byte, error) {
	hasSupplementary := false
	for _, g := range glyphs {
		if g.Rune > 0xFFFF {
			hasSupplementary = true
			break
		}
	}

	sub4, err := buildCmap4Sub(glyphs, newGidOf)
	if err != nil {
		return nil, err
	}
	type record struct{ pid, eid uint16 }
	records := []record{{3, 1}, {0, 4}}
	subs := [][]byte{sub4}
	if hasSupplementary {
		sub12 := buildCmap12Sub(glyphs, newGidOf)
		records = append(records, record{3, 10}, record{0, 6})
		subs = append(subs, sub12)
	}

	// cmap 头 + 编码记录 + 子表（format 4 被两条记录共享）
	n := len(records)
	headerLen := 4 + n*8
	out := make([]byte, headerLen)
	putU16(out, 0, 0)
	putU16(out, 2, uint16(n))
	// 计算子表偏移：format 4 在前，format 12（如有）在后
	off4 := headerLen
	recSub := []int{0, 0} // records[i] → subs 索引
	if hasSupplementary {
		recSub = append(recSub, 1, 1)
	}
	for i, r := range records {
		putU16(out, 4+i*8, r.pid)
		putU16(out, 6+i*8, r.eid)
		off := off4
		if recSub[i] == 1 {
			off = off4 + len(subs[0])
		}
		putU32(out, 8+i*8, uint32(off))
	}
	for _, s := range subs {
		out = append(out, s...)
	}
	return out, nil
}

// buildCmap12Sub 生成 format 12 子表：连续码点且新 gid 连续的合并为一组。
func buildCmap12Sub(glyphs []GlyphMapping, newGidOf map[uint16]int) []byte {
	type mapping struct {
		cp  rune
		gid uint32
	}
	var ms []mapping
	for _, g := range glyphs {
		if g.Rune <= 0 {
			continue
		}
		ms = append(ms, mapping{g.Rune, uint32(newGidOf[g.GID])})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].cp < ms[j].cp })

	type group struct{ start, end, startGID uint32 }
	var groups []group
	for i := 0; i < len(ms); {
		j := i
		for j+1 < len(ms) && ms[j+1].cp == ms[j].cp+1 && ms[j+1].gid == ms[j].gid+1 {
			j++
		}
		groups = append(groups, group{uint32(ms[i].cp), uint32(ms[j].cp), ms[i].gid})
		i = j + 1
	}

	sub := make([]byte, 16+12*len(groups))
	putU16(sub, 0, 12)
	putU32(sub, 4, uint32(len(sub)))
	putU32(sub, 12, uint32(len(groups)))
	for i, g := range groups {
		putU32(sub, 16+12*i, g.start)
		putU32(sub, 16+12*i+4, g.end)
		putU32(sub, 16+12*i+8, g.startGID)
	}
	return sub
}

// buildCmap4Sub 生成 format 4 子表（连续码点分段 + glyphIdArray）。
func buildCmap4Sub(glyphs []GlyphMapping, newGidOf map[uint16]int) ([]byte, error) {
	type mapping struct {
		cp  uint16
		gid uint16
	}
	var ms []mapping
	for _, g := range glyphs {
		if g.Rune <= 0 || g.Rune > 0xFFFF {
			continue
		}
		ms = append(ms, mapping{uint16(g.Rune), uint16(newGidOf[g.GID])})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].cp < ms[j].cp })

	// 连续码点分组为段，段内用 glyphIdArray
	type seg struct{ start, end, glyphStart uint16 }
	var segs []seg
	var glyphIDs []uint16
	for i := 0; i < len(ms); {
		j := i
		for j+1 < len(ms) && ms[j+1].cp == ms[j].cp+1 {
			j++
		}
		segs = append(segs, seg{ms[i].cp, ms[j].cp, uint16(len(glyphIDs))})
		for k := i; k <= j; k++ {
			glyphIDs = append(glyphIDs, ms[k].gid)
		}
		i = j + 1
	}
	// 哨兵段 0xFFFF → gid 0（idDelta = 1）
	segs = append(segs, seg{0xFFFF, 0xFFFF, 0})
	useDelta := len(segs) - 1 // 最后一段用 idDelta

	segCount := len(segs)
	segCountX2 := uint16(segCount * 2)
	searchRange, entrySelector := uint16(2), uint16(0)
	for searchRange*2 <= uint16(segCount) {
		searchRange *= 2
		entrySelector++
	}
	rangeShift := segCountX2 - searchRange

	subLen := 16 + 8*segCount + 2*len(glyphIDs)
	sub := make([]byte, subLen)
	putU16(sub, 0, 4)
	putU16(sub, 2, uint16(subLen))
	putU16(sub, 6, segCountX2)
	putU16(sub, 8, searchRange)
	putU16(sub, 10, entrySelector)
	putU16(sub, 12, rangeShift)

	endOff := 14
	startOff := endOff + 2*segCount + 2
	deltaOff := startOff + 2*segCount
	roOff := deltaOff + 2*segCount
	glyphOff := roOff + 2*segCount

	for i, s := range segs {
		putU16(sub, endOff+2*i, s.end)
		putU16(sub, startOff+2*i, s.start)
		if i == useDelta {
			putU16(sub, deltaOff+2*i, 1) // (0xFFFF+1)%65536 = 0
			putU16(sub, roOff+2*i, 0)
		} else {
			putU16(sub, deltaOff+2*i, 0)
			// idRangeOffset：从 &idRangeOffset[i] 到 glyphIdArray[s.glyphStart] 的字节距离
			ro := (glyphOff + 2*int(s.glyphStart)) - (roOff + 2*i)
			putU16(sub, roOff+2*i, uint16(ro))
		}
	}
	for i, g := range glyphIDs {
		putU16(sub, glyphOff+2*i, g)
	}
	return sub, nil
}

// assemble 组装完整字体文件，计算表校验和与 head.checkSumAdjustment。
func assemble(tables map[string][]byte) ([]byte, error) {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	numTables := len(tags)
	searchRange, entrySelector := 16, 0
	for searchRange*2 <= numTables*16 {
		searchRange *= 2
		entrySelector++
	}
	rangeShift := numTables*16 - searchRange

	headerLen := 12 + numTables*16
	out := make([]byte, headerLen)
	putU32(out, 0, 0x00010000)
	putU16(out, 4, uint16(numTables))
	putU16(out, 6, uint16(searchRange))
	putU16(out, 8, uint16(entrySelector))
	putU16(out, 10, uint16(rangeShift))

	headOffset := 0
	for i, tag := range tags {
		data := tables[tag]
		// 4 字节对齐
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		off := len(out)
		rec := 12 + i*16
		copy(out[rec:], tag)
		putU32(out, rec+4, tableChecksum(data))
		putU32(out, rec+8, uint32(off))
		putU32(out, rec+12, uint32(len(data)))
		out = append(out, data...)
		if tag == "head" {
			headOffset = off
		}
	}
	for len(out)%4 != 0 {
		out = append(out, 0)
	}

	// checkSumAdjustment = 0xB1B0AFBA − 全文件校验和
	adj := 0xB1B0AFBA - tableChecksum(out)
	putU32(out, headOffset+8, adj)
	return out, nil
}

// tableChecksum 计算表（或整个文件）的校验和（大端 uint32 累加，尾部补零到 4 字节）。
func tableChecksum(data []byte) uint32 {
	var sum uint32
	for i := 0; i < len(data); i += 4 {
		var v uint32
		for j := 0; j < 4; j++ {
			v <<= 8
			if i+j < len(data) {
				v |= uint32(data[i+j])
			}
		}
		sum += v
	}
	return sum
}
