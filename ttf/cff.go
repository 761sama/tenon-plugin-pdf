package ttf

import "fmt"

// CFF（Compact Font Format，OpenType 的 "CFF " 表）解析。
// 只提取子集化所需的结构：CharStrings、FDArray/FDSelect（CID 键字体）、
// 各 Private 字典与局部 Subrs 的位置；charset/Encoding 不解析——子集化时
// CID 按输出字形序重编号，charset 整体重建，Encoding 一律丢弃。

// cffFont 已解析的 CFF 字体（切片均指向原始数据，不复制）。
type cffFont struct {
	data        []byte
	header      []byte // 文件头（hdrSize 字节）
	name        []byte // 首个字体名（Name INDEX 第一项）
	topDict     []dictEntry
	strings     [][]byte // String INDEX（非标准字符串，SID 391 起）
	isCID       bool     // 含 ROS（12 30）则为 CID 键字体
	charstrings [][]byte // GID → charstring 原始字节（Type 2）
	gsubrItems  [][]byte // Global Subr INDEX 成员
	fds         []cffFD  // FDArray 成员（CID 字体）
	fdSelect    []uint8  // GID → FD 索引（CID 字体，已展开）
	privateOff  int      // 非 CID 字体：顶层 Private 位置
	privateSize int
}

// cffFD 是 FDArray 的一个成员：Font DICT 条目与其 Private 字典位置。
type cffFD struct {
	entries     []dictEntry
	privateOff  int
	privateSize int
}

// dictEntry 是 DICT 数据中的一条操作数序列 + 操作符。
// 单字节操作符 op 为 0..21；双字节操作符（转义字节 12）op 为 0x0C00|第二字节。
type dictEntry struct {
	op   uint16
	args []dictNum
}

// dictNum 是 DICT 操作数：整数或实数（实数保留十进制文本，原样重编码）。
type dictNum struct {
	isReal bool
	i      int64
	r      string
}

// 解析 CFF 表数据。
// 入参: data 为 "CFF " 表的完整字节
// 出参: 解析结果；结构非法时返回错误
func parseCFF(data []byte) (*cffFont, error) {
	if len(data) < 4 || data[0] != 1 {
		return nil, fmt.Errorf("ttf: CFF 头非法")
	}
	hdrSize := int(data[2])
	if hdrSize < 4 || hdrSize > len(data) {
		return nil, fmt.Errorf("ttf: CFF hdrSize 非法")
	}
	c := &cffFont{data: data, header: data[:hdrSize]}
	names, next, err := parseIndex(data, hdrSize)
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF Name INDEX: %w", err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("ttf: CFF 无字体名")
	}
	c.name = names[0]
	topIdx, next, err := parseIndex(data, next)
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF Top DICT INDEX: %w", err)
	}
	if len(topIdx) != 1 {
		return nil, fmt.Errorf("ttf: CFF 含 %d 个 Top DICT，仅支持单字体", len(topIdx))
	}
	c.topDict, err = parseDict(topIdx[0])
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF Top DICT: %w", err)
	}
	c.strings, next, err = parseIndex(data, next)
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF String INDEX: %w", err)
	}
	c.gsubrItems, _, err = parseIndex(data, next)
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF Global Subr INDEX: %w", err)
	}
	c.isCID = dictHas(c.topDict, opROS)
	csOff, ok := dictInt(c.topDict, opCharStrings)
	if !ok {
		return nil, fmt.Errorf("ttf: CFF 缺少 CharStrings")
	}
	c.charstrings, _, err = parseIndex(data, int(csOff))
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF CharStrings INDEX: %w", err)
	}
	if c.isCID {
		if err := c.parseCID(); err != nil {
			return nil, err
		}
	} else {
		size, off, _ := dictInt2(c.topDict, opPrivate)
		c.privateSize, c.privateOff = int(size), int(off)
	}
	return c, nil
}

// 解析 CID 键结构：FDArray（各成员 Font DICT 与 Private 位置）与 FDSelect。
func (c *cffFont) parseCID() error {
	fdArrayOff, ok := dictInt(c.topDict, opFDArray)
	if !ok {
		return fmt.Errorf("ttf: CID 字体缺少 FDArray")
	}
	fdSelOff, ok := dictInt(c.topDict, opFDSelect)
	if !ok {
		return fmt.Errorf("ttf: CID 字体缺少 FDSelect")
	}
	fdDicts, _, err := parseIndex(c.data, int(fdArrayOff))
	if err != nil {
		return fmt.Errorf("ttf: CFF FDArray INDEX: %w", err)
	}
	if len(fdDicts) == 0 {
		return fmt.Errorf("ttf: CFF FDArray 为空")
	}
	if len(fdDicts) > 256 {
		return fmt.Errorf("ttf: CFF FDArray 成员数 %d 超出支持范围", len(fdDicts))
	}
	for _, d := range fdDicts {
		ents, err := parseDict(d)
		if err != nil {
			return fmt.Errorf("ttf: CFF FDArray Font DICT: %w", err)
		}
		fd := cffFD{entries: ents}
		size, off, _ := dictInt2(ents, opPrivate)
		fd.privateSize, fd.privateOff = int(size), int(off)
		c.fds = append(c.fds, fd)
	}
	c.fdSelect, err = parseFDSelect(c.data, int(fdSelOff), len(c.charstrings))
	if err != nil {
		return fmt.Errorf("ttf: CFF FDSelect: %w", err)
	}
	return nil
}

// 解析 FD（或非 CID 顶层）Private 字典，返回局部 Subrs INDEX 成员；无 Subrs 返回 nil。
// 入参: privateOff/privateSize 为 Private 位置（size ≤ 0 表示无 Private）
func (c *cffFont) localSubrs(privateOff, privateSize int) ([][]byte, error) {
	if privateSize <= 0 {
		return nil, nil
	}
	if privateOff < 0 || privateOff+privateSize > len(c.data) {
		return nil, fmt.Errorf("ttf: CFF Private 字典越界")
	}
	ents, err := parseDict(c.data[privateOff : privateOff+privateSize])
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF Private 字典: %w", err)
	}
	subrsOff, ok := dictInt(ents, opSubrs)
	if !ok || subrsOff <= 0 {
		return nil, nil
	}
	items, _, err := parseIndex(c.data, privateOff+int(subrsOff))
	if err != nil {
		return nil, fmt.Errorf("ttf: CFF 局部 Subrs INDEX: %w", err)
	}
	return items, nil
}

// CFF DICT 操作符（本实现用到的子集）。
const (
	opCharset     = 15
	opEncoding    = 16
	opCharStrings = 17
	opPrivate     = 18
	opSubrs       = 19
	opROS         = 0x0C00 | 30
	opCIDCount    = 0x0C00 | 34
	opFDArray     = 0x0C00 | 36
	opFDSelect    = 0x0C00 | 37
)

// 查询条目 args[0] 的整数值；实数或缺失时返回默认值与 false。
func dictInt(ents []dictEntry, op uint16) (int64, bool) {
	for _, e := range ents {
		if e.op == op && len(e.args) >= 1 && !e.args[0].isReal {
			return e.args[0].i, true
		}
	}
	return 0, false
}

// 查询双操作数条目（如 Private size/offset）的两个整数值。
func dictInt2(ents []dictEntry, op uint16) (int64, int64, bool) {
	for _, e := range ents {
		if e.op == op && len(e.args) >= 2 && !e.args[0].isReal && !e.args[1].isReal {
			return e.args[0].i, e.args[1].i, true
		}
	}
	return 0, 0, false
}

// 报告条目中是否存在指定操作符。
func dictHas(ents []dictEntry, op uint16) bool {
	for _, e := range ents {
		if e.op == op {
			return true
		}
	}
	return false
}

// 解析 CFF INDEX：count + offSize + 偏移数组（1 起）+ 数据。
// 入参: data 为完整 CFF，off 为 INDEX 起始偏移
// 出参: 各成员字节切片、INDEX 结束偏移、错误
func parseIndex(data []byte, off int) ([][]byte, int, error) {
	if off+2 > len(data) {
		return nil, 0, fmt.Errorf("INDEX 越界")
	}
	count := int(u16(data, off))
	if count == 0 {
		return nil, off + 2, nil
	}
	if off+3+(count+1)*1 > len(data) {
		return nil, 0, fmt.Errorf("INDEX 越界")
	}
	offSize := int(data[off+2])
	if offSize < 1 || offSize > 4 {
		return nil, 0, fmt.Errorf("INDEX offSize %d 非法", offSize)
	}
	arrOff := off + 3
	dataStart := arrOff + (count+1)*offSize
	if dataStart > len(data) {
		return nil, 0, fmt.Errorf("INDEX 偏移数组越界")
	}
	prev := 0
	items := make([][]byte, 0, count)
	for i := 0; i <= count; i++ {
		v := 0
		for j := 0; j < offSize; j++ {
			v = v<<8 | int(data[arrOff+i*offSize+j])
		}
		if v < 1 || dataStart+v-1 > len(data) || (i > 0 && v < prev) {
			return nil, 0, fmt.Errorf("INDEX 偏移非法")
		}
		if i > 0 {
			items = append(items, data[dataStart+prev-1:dataStart+v-1])
		}
		prev = v
	}
	return items, dataStart + prev - 1, nil
}

// 返回 off 处 INDEX 的结束偏移（不物化成员，用于整块原样复制）。
func indexExtent(data []byte, off int) (int, error) {
	if off+2 > len(data) {
		return 0, fmt.Errorf("INDEX 越界")
	}
	count := int(u16(data, off))
	if count == 0 {
		return off + 2, nil
	}
	offSize := int(data[off+2])
	if offSize < 1 || offSize > 4 {
		return 0, fmt.Errorf("INDEX offSize %d 非法", offSize)
	}
	arrOff := off + 3
	dataStart := arrOff + (count+1)*offSize
	if dataStart > len(data) {
		return 0, fmt.Errorf("INDEX 偏移数组越界")
	}
	last := 0
	for j := 0; j < offSize; j++ {
		last = last<<8 | int(data[arrOff+count*offSize+j])
	}
	if last < 1 || dataStart+last-1 > len(data) {
		return 0, fmt.Errorf("INDEX 偏移非法")
	}
	return dataStart + last - 1, nil
}

// 解析 DICT 数据为条目序列（操作数栈遇操作符落为一条）。
func parseDict(b []byte) ([]dictEntry, error) {
	var ents []dictEntry
	var stack []dictNum
	i := 0
	for i < len(b) {
		b0 := b[i]
		if b0 <= 21 {
			op := uint16(b0)
			i++
			if b0 == 12 {
				if i >= len(b) {
					return nil, fmt.Errorf("DICT 转义操作符截断")
				}
				op = 0x0C00 | uint16(b[i])
				i++
			}
			ents = append(ents, dictEntry{op: op, args: stack})
			stack = nil
			continue
		}
		n, ni, err := decodeDictNum(b, i)
		if err != nil {
			return nil, err
		}
		stack = append(stack, n)
		i = ni
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("DICT 尾部有未消费的操作数")
	}
	return ents, nil
}

// 解码 i 处的一个 DICT 操作数，返回操作数与下一字节位置。
func decodeDictNum(b []byte, i int) (dictNum, int, error) {
	b0 := b[i]
	switch {
	case b0 == 28:
		if i+3 > len(b) {
			return dictNum{}, 0, fmt.Errorf("DICT int16 截断")
		}
		return dictNum{i: int64(int16(u16(b, i+1)))}, i + 3, nil
	case b0 == 29:
		if i+5 > len(b) {
			return dictNum{}, 0, fmt.Errorf("DICT int32 截断")
		}
		return dictNum{i: int64(int32(u32(b, i+1)))}, i + 5, nil
	case b0 == 30:
		return decodeDictReal(b, i)
	case b0 >= 32 && b0 <= 246:
		return dictNum{i: int64(b0) - 139}, i + 1, nil
	case b0 >= 247 && b0 <= 250:
		if i+2 > len(b) {
			return dictNum{}, 0, fmt.Errorf("DICT 整数截断")
		}
		return dictNum{i: int64(b0-247)*256 + int64(b[i+1]) + 108}, i + 2, nil
	case b0 >= 251 && b0 <= 254:
		if i+2 > len(b) {
			return dictNum{}, 0, fmt.Errorf("DICT 整数截断")
		}
		return dictNum{i: -int64(b0-251)*256 - int64(b[i+1]) - 108}, i + 2, nil
	}
	return dictNum{}, 0, fmt.Errorf("DICT 保留字节 0x%02x", b0)
}

// 解码实数操作数（前导字节 30，半字节序列以 0xF 结束），保留十进制文本。
func decodeDictReal(b []byte, i int) (dictNum, int, error) {
	j := i + 1
	var s []byte
	for {
		if j >= len(b) {
			return dictNum{}, 0, fmt.Errorf("DICT 实数截断")
		}
		for _, nib := range [2]byte{b[j] >> 4, b[j] & 0x0F} {
			switch nib {
			case 0xF:
				return dictNum{isReal: true, r: string(s)}, j + 1, nil
			case 0xA:
				s = append(s, '.')
			case 0xB:
				s = append(s, 'E')
			case 0xC:
				s = append(s, "E-"...)
			case 0xE:
				s = append(s, '-')
			case 0xD:
				return dictNum{}, 0, fmt.Errorf("DICT 实数含保留半字节")
			default:
				s = append(s, byte('0'+nib))
			}
		}
		j++
	}
}

// 编码 DICT 条目序列（操作数在前、操作符在后）。
func encodeDict(ents []dictEntry) []byte {
	var out []byte
	for _, e := range ents {
		for _, a := range e.args {
			out = encodeDictNum(out, a)
		}
		if e.op&0x0C00 != 0 {
			out = append(out, 12, byte(e.op&0xFF))
		} else {
			out = append(out, byte(e.op))
		}
	}
	return out
}

// 追加编码一个 DICT 操作数（整数取最短形式，实数按文本转半字节）。
func encodeDictNum(out []byte, n dictNum) []byte {
	if n.isReal {
		return encodeDictReal(out, n.r)
	}
	v := n.i
	switch {
	case v >= -107 && v <= 107:
		return append(out, byte(v+139))
	case v >= 108 && v <= 1131:
		v -= 108
		return append(out, byte(v/256+247), byte(v%256))
	case v >= -1131 && v <= -108:
		v = -v - 108
		return append(out, byte(v/256+251), byte(v%256))
	case v >= -32768 && v <= 32767:
		return append(out, 28, byte(int16(v)>>8), byte(int16(v)))
	default:
		return append(out, 29, byte(int32(v)>>24), byte(int32(v)>>16), byte(int32(v)>>8), byte(int32(v)))
	}
}

// 追加编码实数操作数：前导 30，字符转半字节，0xF 收尾（奇数个半字节补 0xF）。
func encodeDictReal(out []byte, s string) []byte {
	out = append(out, 30)
	pending := -1
	flush := func(nib byte) {
		if pending < 0 {
			pending = int(nib)
			return
		}
		out = append(out, byte(pending)<<4|nib)
		pending = -1
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= '0' && ch <= '9':
			flush(ch - '0')
		case ch == '.':
			flush(0xA)
		case ch == 'E' || ch == 'e':
			if i+1 < len(s) && s[i+1] == '-' {
				flush(0xC)
				i++
			} else {
				flush(0xB)
			}
		case ch == '-':
			flush(0xE)
		case ch == '+': // E+ 形式的正号直接丢弃
		default:
			// 非法字符不应出现（实数文本来自 decodeDictReal）；忽略以保证健壮
		}
	}
	flush(0xF)
	if pending >= 0 {
		out = append(out, byte(pending)<<4|0xF)
	}
	return out
}

// 构造 CFF INDEX（偏移按最大数据长度选 1..4 字节宽）。
func buildIndex(items [][]byte) []byte {
	if len(items) == 0 {
		return []byte{0, 0}
	}
	total := 0
	for _, it := range items {
		total += len(it)
	}
	offSize := 1
	for v := total + 1; v > 0xFF; v >>= 8 {
		offSize++
	}
	out := make([]byte, 0, 3+(len(items)+1)*offSize+total)
	out = append(out, byte(len(items)>>8), byte(len(items)), byte(offSize))
	acc := 1
	writeOff := func(v int) {
		for j := offSize - 1; j >= 0; j-- {
			out = append(out, byte(v>>(8*j)))
		}
	}
	writeOff(acc)
	for _, it := range items {
		acc += len(it)
		writeOff(acc)
	}
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

// 解析 FDSelect 为逐字形 FD 索引数组（支持 format 0/3/4）。
// 入参: data 为完整 CFF，off 为 FDSelect 偏移，nGlyphs 为字形总数
// 出参: 长度 nGlyphs 的 FD 索引数组；格式非法时返回错误
func parseFDSelect(data []byte, off, nGlyphs int) ([]uint8, error) {
	if off >= len(data) {
		return nil, fmt.Errorf("FDSelect 越界")
	}
	fds := make([]uint8, nGlyphs)
	switch data[off] {
	case 0:
		if off+1+nGlyphs > len(data) {
			return nil, fmt.Errorf("FDSelect format 0 越界")
		}
		copy(fds, data[off+1:off+1+nGlyphs])
	case 3, 4:
		is4 := data[off] == 4
		nRanges := int(u16(data, off+1))
		if nRanges == 0 {
			return nil, fmt.Errorf("FDSelect 区间为空")
		}
		p := off + 3
		type rng struct{ first, fd int }
		ranges := make([]rng, 0, nRanges)
		prevFirst := -1
		for r := 0; r < nRanges; r++ {
			var first, fd int
			if is4 {
				if p+6 > len(data) {
					return nil, fmt.Errorf("FDSelect format 4 越界")
				}
				first, fd = int(u32(data, p)), int(u16(data, p+4))
				p += 6
			} else {
				if p+3 > len(data) {
					return nil, fmt.Errorf("FDSelect format 3 越界")
				}
				first, fd = int(u16(data, p)), int(data[p+2])
				p += 3
			}
			if first <= prevFirst || first >= nGlyphs {
				return nil, fmt.Errorf("FDSelect 区间首字形非法")
			}
			if fd > 255 {
				return nil, fmt.Errorf("FDSelect FD 索引 %d 超出支持范围", fd)
			}
			prevFirst = first
			ranges = append(ranges, rng{first, fd})
		}
		var sentinel int
		if is4 {
			if p+4 > len(data) {
				return nil, fmt.Errorf("FDSelect format 4 缺哨兵")
			}
			sentinel = int(u32(data, p))
		} else {
			if p+2 > len(data) {
				return nil, fmt.Errorf("FDSelect format 3 缺哨兵")
			}
			sentinel = int(u16(data, p))
		}
		if sentinel != nGlyphs {
			return nil, fmt.Errorf("FDSelect 哨兵 %d 与字形数 %d 不符", sentinel, nGlyphs)
		}
		if ranges[0].first != 0 {
			return nil, fmt.Errorf("FDSelect 首个区间未从字形 0 开始")
		}
		for r, rg := range ranges {
			end := nGlyphs
			if r+1 < len(ranges) {
				end = ranges[r+1].first
			}
			for g := rg.first; g < end; g++ {
				fds[g] = byte(rg.fd)
			}
		}
	default:
		return nil, fmt.Errorf("FDSelect format %d 不支持", data[off])
	}
	return fds, nil
}
