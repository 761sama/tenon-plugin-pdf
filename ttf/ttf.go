// Package ttf 实现 TrueType 字体文件（glyf 轮廓）的解析与子集化重建。
// 支持独立 TTF 与 TTC 集合（ParseCollection）。
//
// 子集化输出仅包含指定字形的新字体文件：重建 glyf/loca/hmtx/maxp/cmap/head
// 等表，丢弃 GSUB/GPOS/GDEF/变体等与本用途无关的表。
// 解析侧额外提供 kern 表（字距）与 GSUB 连字规则（liga/rlig），
// 供上层排版使用；这些表不进子集产物。
package ttf

import (
	"fmt"
)

// Font 已解析的 TrueType 字体。
type Font struct {
	data   []byte
	tables map[string][2]uint32 // tag → [offset, length]

	UnitsPerEm             int
	Ascent                 int
	Descent                int
	LineGap                int
	CapHeight              int // OS/2 sCapHeight（v2+），无数据时为 0
	NumGlyphs              int
	numberOfHMetrics       int
	advances               []uint16 // 每个字形的步进宽度
	lsbs                   []int16
	loca                   []uint32
	XMin, YMin, XMax, YMax int16

	cmap4  []byte // format 4 子表（可空）
	cmap12 []byte // format 12 子表（可空）

	kernPairs map[uint32]int16       // kern 字距对（left<<16|right → 修正值，可空）
	ligatures map[uint16][]Ligature  // GSUB 连字规则（首字形 → 候选，可空）

	psName string
}

// Parse 解析 TrueType 字体文件。
// 数据为 TTC 集合时报错并提示使用 ParseCollection。
func Parse(data []byte) (*Font, error) {
	if IsCollection(data) {
		return nil, fmt.Errorf("ttf: 数据是 TTC 集合（含 %d 个字体），请用 ParseCollection(data, index)",
			CollectionCount(data))
	}
	return parseAt(data, 0)
}

// parseAt 从 sfnt 目录起始偏移 off 解析单个字体（TTC 成员或独立字体）。
func parseAt(data []byte, off int) (*Font, error) {
	if off < 0 || off+12 > len(data) {
		return nil, fmt.Errorf("ttf: 文件过短")
	}
	sfnt := u32(data, off)
	if sfnt != 0x00010000 && sfnt != 0x74727565 { // 1.0 或 'true'
		return nil, fmt.Errorf("ttf: 不支持的字体格式 0x%08x（仅支持 glyf 轮廓的 TrueType）", sfnt)
	}
	numTables := int(u16(data, off+4))
	f := &Font{data: data, tables: make(map[string][2]uint32, numTables)}
	for i := 0; i < numTables; i++ {
		o := off + 12 + i*16
		if o+16 > len(data) {
			return nil, fmt.Errorf("ttf: 表目录越界")
		}
		tag := string(data[o : o+4])
		toff := u32(data, o+8)
		length := u32(data, o+12)
		if uint64(toff)+uint64(length) > uint64(len(data)) {
			return nil, fmt.Errorf("ttf: 表 %s 越界", tag)
		}
		f.tables[tag] = [2]uint32{toff, length}
	}
	if err := f.parseHead(); err != nil {
		return nil, err
	}
	if err := f.parseHhea(); err != nil {
		return nil, err
	}
	if err := f.parseMaxp(); err != nil {
		return nil, err
	}
	if err := f.parseHmtx(); err != nil {
		return nil, err
	}
	if err := f.parseLoca(); err != nil {
		return nil, err
	}
	if err := f.parseCmap(); err != nil {
		return nil, err
	}
	f.parseOS2()
	f.parseKern()
	f.parseGSUB()
	f.parseName()
	return f, nil
}

func (f *Font) table(tag string) ([]byte, error) {
	t, ok := f.tables[tag]
	if !ok {
		return nil, fmt.Errorf("ttf: 缺少 %s 表", tag)
	}
	return f.data[t[0] : t[0]+t[1]], nil
}

func (f *Font) parseHead() error {
	d, err := f.table("head")
	if err != nil {
		return err
	}
	if len(d) < 54 {
		return fmt.Errorf("ttf: head 表过短")
	}
	f.UnitsPerEm = int(u16(d, 18))
	f.XMin = int16(u16(d, 36))
	f.YMin = int16(u16(d, 38))
	f.XMax = int16(u16(d, 40))
	f.YMax = int16(u16(d, 42))
	return nil
}

func (f *Font) parseHhea() error {
	d, err := f.table("hhea")
	if err != nil {
		return err
	}
	f.Ascent = int(int16(u16(d, 4)))
	f.Descent = int(int16(u16(d, 6)))
	f.LineGap = int(int16(u16(d, 8)))
	f.numberOfHMetrics = int(u16(d, 34))
	return nil
}

func (f *Font) parseMaxp() error {
	d, err := f.table("maxp")
	if err != nil {
		return err
	}
	f.NumGlyphs = int(u16(d, 4))
	return nil
}

func (f *Font) parseHmtx() error {
	d, err := f.table("hmtx")
	if err != nil {
		return err
	}
	if len(d) < f.numberOfHMetrics*4 {
		return fmt.Errorf("ttf: hmtx 表过短")
	}
	f.advances = make([]uint16, f.NumGlyphs)
	f.lsbs = make([]int16, f.NumGlyphs)
	for i := 0; i < f.numberOfHMetrics && i < f.NumGlyphs; i++ {
		f.advances[i] = u16(d, i*4)
		f.lsbs[i] = int16(u16(d, i*4+2))
	}
	// 等宽尾部：步进沿用最后一个，lsb 从附加数组读取
	for i := f.numberOfHMetrics; i < f.NumGlyphs; i++ {
		f.advances[i] = f.advances[f.numberOfHMetrics-1]
		o := f.numberOfHMetrics*4 + (i-f.numberOfHMetrics)*2
		if o+2 <= len(d) {
			f.lsbs[i] = int16(u16(d, o))
		}
	}
	return nil
}

func (f *Font) parseLoca() error {
	d, err := f.table("loca")
	if err != nil {
		return err
	}
	head, _ := f.table("head")
	long := int16(u16(head, 50)) == 1
	f.loca = make([]uint32, f.NumGlyphs+1)
	for i := 0; i <= f.NumGlyphs; i++ {
		if long {
			f.loca[i] = u32(d, i*4)
		} else {
			f.loca[i] = uint32(u16(d, i*2)) * 2
		}
	}
	return nil
}

func (f *Font) parseCmap() error {
	d, err := f.table("cmap")
	if err != nil {
		return err
	}
	n := int(u16(d, 2))
	type cand struct {
		pid, eid, fmt int
		data          []byte
	}
	var cands []cand
	for i := 0; i < n; i++ {
		pid := int(u16(d, 4+8*i))
		eid := int(u16(d, 6+8*i))
		off := int(u32(d, 8+8*i))
		if off+2 > len(d) {
			continue
		}
		fm := int(u16(d, off))
		var sub []byte
		switch fm {
		case 4:
			l := int(u16(d, off+2))
			if off+l <= len(d) {
				sub = d[off : off+l]
			}
		case 12:
			l := int(u32(d, off+4))
			if off+l <= len(d) {
				sub = d[off : off+l]
			}
		}
		if sub != nil {
			cands = append(cands, cand{pid, eid, fm, sub})
		}
	}
	// 优先 (3,10) format12，其次任意 format12，再 (3,1)/(0,3) format4
	score := func(c cand) int {
		s := 0
		if c.fmt == 12 {
			s += 100
		}
		if c.pid == 3 {
			s += 10
		}
		if c.eid == 10 || c.eid == 1 {
			s += 5
		}
		if c.pid == 0 {
			s += 3
		}
		return s
	}
	for _, c := range cands {
		if c.fmt == 12 && (f.cmap12 == nil || score(c) > 0) {
			if f.cmap12 == nil {
				f.cmap12 = c.data
			}
		}
	}
	best := -1
	for i, c := range cands {
		if c.fmt == 4 && (best < 0 || score(c) > score(cands[best])) {
			best = i
		}
	}
	if best >= 0 {
		f.cmap4 = cands[best].data
	}
	if f.cmap4 == nil && f.cmap12 == nil {
		return fmt.Errorf("ttf: 无可用 cmap 子表")
	}
	return nil
}

// parseOS2 读取 OS/2 表的 sCapHeight（version ≥ 2，偏移 88）。
// 缺失时保持 0，调用方自行回退估算。
func (f *Font) parseOS2() {
	d, err := f.table("OS/2")
	if err != nil || len(d) < 90 || int(u16(d, 0)) < 2 {
		return
	}
	f.CapHeight = int(int16(u16(d, 88)))
}

// parseName 读取 PostScript 名称（name id 6）。
func (f *Font) parseName() {
	d, err := f.table("name")
	if err != nil || len(d) < 6 {
		return
	}
	count := int(u16(d, 2))
	strOff := int(u16(d, 4))
	bestScore := -1
	for i := 0; i < count; i++ {
		o := 6 + i*12
		if o+12 > len(d) {
			break
		}
		pid := int(u16(d, o))
		eid := int(u16(d, o+2))
		nid := int(u16(d, o+6))
		if nid != 6 {
			continue
		}
		l := int(u16(d, o+8))
		so := strOff + int(u16(d, o+10))
		if so+l > len(d) {
			continue
		}
		raw := d[so : so+l]
		score := 1
		var name string
		if pid == 3 || (pid == 0) {
			// UTF-16BE
			runes := make([]rune, 0, l/2)
			for j := 0; j+1 < len(raw); j += 2 {
				runes = append(runes, rune(u16(raw, j)))
			}
			name = string(runes)
			score = 2
			if eid == 1 {
				score = 3
			}
		} else if pid == 1 {
			name = string(raw)
		}
		if score > bestScore && name != "" {
			bestScore = score
			f.psName = name
		}
	}
}

// PSName 返回字体的 PostScript 名称。
func (f *Font) PSName() string {
	if f.psName != "" {
		return f.psName
	}
	return "SubsetFont"
}

// Advance 返回字形的步进宽度（字体单位）。
func (f *Font) Advance(gid uint16) uint16 {
	if int(gid) >= len(f.advances) {
		return 0
	}
	return f.advances[gid]
}

// GlyphIndex 查找 Unicode 码点对应的字形 ID（0 表示缺失）。
func (f *Font) GlyphIndex(r rune) uint16 {
	if r > 0xFFFF {
		if f.cmap12 != nil {
			return lookup12(f.cmap12, uint32(r))
		}
		return 0
	}
	if f.cmap4 != nil {
		if g := lookup4(f.cmap4, uint16(r)); g != 0 {
			return g
		}
	}
	if f.cmap12 != nil {
		return lookup12(f.cmap12, uint32(r))
	}
	return 0
}

func lookup4(d []byte, c uint16) uint16 {
	segCount := int(u16(d, 6)) / 2
	endOff := 14
	startOff := endOff + 2*segCount + 2
	deltaOff := startOff + 2*segCount
	roOff := deltaOff + 2*segCount
	for s := 0; s < segCount; s++ {
		end := u16(d, endOff+2*s)
		if c > end {
			continue
		}
		start := u16(d, startOff+2*s)
		if c < start {
			return 0
		}
		delta := int16(u16(d, deltaOff+2*s))
		ro := u16(d, roOff+2*s)
		if ro == 0 {
			return uint16(int32(c) + int32(delta))
		}
		gi := roOff + 2*s + int(ro) + 2*int(c-start)
		if gi+2 > len(d) {
			return 0
		}
		gid := u16(d, gi)
		if gid == 0 {
			return 0
		}
		return uint16(int32(gid) + int32(delta))
	}
	return 0
}

func lookup12(d []byte, c uint32) uint16 {
	n := int(u32(d, 12))
	lo, hi := 0, n-1
	for lo <= hi {
		mid := (lo + hi) / 2
		o := 16 + mid*12
		start := u32(d, o)
		end := u32(d, o+4)
		if c < start {
			hi = mid - 1
		} else if c > end {
			lo = mid + 1
		} else {
			return uint16(u32(d, o+8) + (c - start))
		}
	}
	return 0
}

// glyphData 返回字形的原始 glyf 数据（可能为空字形）。
func (f *Font) glyphData(gid uint16) []byte {
	if int(gid) >= f.NumGlyphs {
		return nil
	}
	glyf, err := f.table("glyf")
	if err != nil {
		return nil
	}
	start, end := f.loca[gid], f.loca[gid+1]
	if start >= end || end > uint32(len(glyf)) {
		return nil
	}
	return glyf[start:end]
}

// compositeComponents 解析复合字形的组件字形 ID 列表及其在数据中的偏移。
func compositeComponents(d []byte) (comps []compositePart, err error) {
	if len(d) < 10 {
		return nil, fmt.Errorf("ttf: 复合字形数据过短")
	}
	if int16(u16(d, 0)) >= 0 {
		return nil, fmt.Errorf("ttf: 非复合字形")
	}
	o := 10
	for {
		if o+4 > len(d) {
			return nil, fmt.Errorf("ttf: 复合字形组件越界")
		}
		flags := u16(d, o)
		gid := u16(d, o+2)
		comps = append(comps, compositePart{Offset: o + 2, GID: gid, Flags: flags})
		o += 4
		if flags&0x0001 != 0 { // ARG_1_AND_2_ARE_WORDS
			o += 4
		} else {
			o += 2
		}
		if flags&0x0008 != 0 { // WE_HAVE_A_SCALE
			o += 2
		} else if flags&0x0040 != 0 { // WE_HAVE_AN_X_AND_Y_SCALE
			o += 4
		} else if flags&0x0080 != 0 { // WE_HAVE_A_TWO_BY_TWO
			o += 8
		}
		if flags&0x0020 == 0 { // !MORE_COMPONENTS
			break
		}
	}
	return comps, nil
}

type compositePart struct {
	Offset int // glyphIndex 字段在字形数据中的偏移
	GID    uint16
	Flags  uint16
}
