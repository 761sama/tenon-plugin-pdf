package ttf

import "fmt"

// CFF 子集化重建：输出只含指定字形的独立 CFF（供 PDF 以 CIDFontType0C 嵌入）。
//
// 输出统一重建为 CID 键字体：字形按 glyphs 顺序重排，GID i 的 CID 重编号为 i
// （与 PDF 侧 /CIDToGIDMap 无关，CIDFontType0 直接以 CFF charset 反查）。
// 局部/全局 Subrs 做调用闭包分析，仅保留被引用的子程序并重编号
// （charstring 中的 callsubr/callgsubr 操作数同步改写）；
// charset/FDSelect/FDArray 按新字形序重建，未用到的 FD 丢弃。
// 非 CID 源字体转换为单 FD 的 CID 键结构（ROS=Adobe-Identity-0）。

// stdStrings 标准字符串数量：SID 0..390 为标准字符串，391 起索引 String INDEX。
const stdStrings = 391

// 按给定顺序重建 CFF：输出字形 i 对应 glyphs[i]（glyphs[0] 应为 .notdef）。
// 出参为合法的独立 CFF 数据（单字体、CID 键）。
func (f *Font) subsetCFF(glyphs []GlyphMapping) ([]byte, error) {
	raw, err := f.cffTable()
	if err != nil {
		return nil, err
	}
	c, err := parseCFF(raw)
	if err != nil {
		return nil, err
	}
	// 1. 字形去重定序（CFF 无复合字形，无需组件闭包）
	seen := make(map[uint16]bool, len(glyphs))
	order := make([]uint16, 0, len(glyphs))
	for _, g := range glyphs {
		if seen[g.GID] {
			continue
		}
		if int(g.GID) >= len(c.charstrings) {
			return nil, fmt.Errorf("ttf: 字形 ID %d 超出 CFF 字形数 %d", g.GID, len(c.charstrings))
		}
		seen[g.GID] = true
		order = append(order, g.GID)
	}
	n := len(order)
	// 2. FD 归并：CID 源按 FDSelect 收敛用到的 FD；非 CID 源合成单 FD
	fdOfNewGID := make([]int, n)
	fdRemap := map[int]int{} // 旧 FD → 新 FD
	var srcFDs []cffFD       // 新 FD 序对应的来源 FD
	if c.isCID {
		for i, oldGid := range order {
			oldFD := int(c.fdSelect[oldGid])
			if oldFD >= len(c.fds) {
				return nil, fmt.Errorf("ttf: FDSelect 引用的 FD %d 越界", oldFD)
			}
			nf, ok := fdRemap[oldFD]
			if !ok {
				nf = len(srcFDs)
				fdRemap[oldFD] = nf
				srcFDs = append(srcFDs, c.fds[oldFD])
			}
			fdOfNewGID[i] = nf
		}
	} else {
		srcFDs = []cffFD{{privateOff: c.privateOff, privateSize: c.privateSize}}
	}
	// 3. 子程序闭包：执行式解析子集字形 charstring，沿途收集局部/全局 Subrs 引用
	locals := make([][][]byte, len(srcFDs))
	for i, fd := range srcFDs {
		locals[i], err = c.localSubrs(fd.privateOff, fd.privateSize)
		if err != nil {
			return nil, err
		}
	}
	glyphProgs := make([][]csInstr, n)
	usedG := map[int]bool{}
	usedL := make([]map[int]bool, len(srcFDs))
	gProg := map[int][]csInstr{}
	lProgs := make([]map[int][]csInstr, len(srcFDs))
	for i := range srcFDs {
		usedL[i] = map[int]bool{}
		lProgs[i] = map[int][]csInstr{}
	}
	for i, oldGid := range order {
		fd := fdOfNewGID[i]
		p := newCSParser(c.gsubrItems, locals[fd])
		instrs, err := p.ParseGlyph(c.charstrings[oldGid])
		if err != nil {
			return nil, fmt.Errorf("ttf: 字形 %d charstring: %w", oldGid, err)
		}
		glyphProgs[i] = instrs
		for idx := range p.UsedG {
			usedG[idx] = true
		}
		for idx := range p.UsedL {
			usedL[fd][idx] = true
		}
		for idx, prog := range p.GProg {
			gProg[idx] = prog
		}
		for idx, prog := range p.LProg {
			lProgs[fd][idx] = prog
		}
	}
	globalMap := sortKeys(usedG)
	localMaps := make([]map[int]int, len(srcFDs))
	for i := range srcFDs {
		localMaps[i] = sortKeys(usedL[i])
	}
	remapOf := func(fd int) *subrRemap {
		return &subrRemap{
			locals:        localMaps[fd],
			globals:       globalMap,
			oldLocalBias:  subrBias(len(locals[fd])),
			oldGlobalBias: subrBias(len(c.gsubrItems)),
			newLocalBias:  subrBias(len(localMaps[fd])),
			newGlobalBias: subrBias(len(globalMap)),
		}
	}
	// 4. 重编码 charstrings 与 Subrs（调用操作数按新编号改写）
	csItems := make([][]byte, n)
	for i := range order {
		csItems[i], err = encodeCharstring(glyphProgs[i], remapOf(fdOfNewGID[i]))
		if err != nil {
			return nil, err
		}
	}
	localItems := make([][][]byte, len(srcFDs))
	for i := range srcFDs {
		items := make([][]byte, len(localMaps[i]))
		for old, ni := range localMaps[i] {
			items[ni], err = encodeCharstring(lProgs[i][old], remapOf(i))
			if err != nil {
				return nil, fmt.Errorf("ttf: FD %d 局部子程序 %d: %w", i, old, err)
			}
		}
		localItems[i] = items
	}
	gsubrItems := make([][]byte, len(globalMap))
	{
		// 全局子程序无局部语境：locals 置空，出现 callsubr（非法字体）会报错
		r := &subrRemap{
			locals: map[int]int{}, globals: globalMap,
			oldGlobalBias: subrBias(len(c.gsubrItems)),
			newGlobalBias: subrBias(len(globalMap)),
		}
		for old, ni := range globalMap {
			gsubrItems[ni], err = encodeCharstring(gProg[old], r)
			if err != nil {
				return nil, fmt.Errorf("ttf: 全局子程序 %d: %w", old, err)
			}
		}
	}
	gsubrIdx := buildIndex(gsubrItems)
	// 5. 各新 FD 的 Private 数据块（字典重编码 + 重建的局部 Subrs 紧随）
	// privDictLens 记录各块中字典部分长度（FD 字典的 Private size 只含字典本身，
	// 不含 Subrs INDEX——与 Adobe 原字体布局一致，查看器按 size 截取字典）
	privBlobs := make([][]byte, len(srcFDs))
	privDictLens := make([]int, len(srcFDs))
	for i, fd := range srcFDs {
		privBlobs[i], privDictLens[i], err = buildPrivateBlob(c.data, fd.privateOff, fd.privateSize, buildIndex(localItems[i]))
		if err != nil {
			return nil, err
		}
	}
	// 6. String INDEX：原样保留（ROS/FontName 的 SID 不失效），需要时追加
	newStrings := append([][]byte(nil), c.strings...)
	addString := func(s []byte) int {
		newStrings = append(newStrings, s)
		return stdStrings + len(newStrings) - 1
	}
	// 7. FDArray 条目骨架：剔除 Private（布局阶段按新偏移补回）
	fdEnts := make([][]dictEntry, len(srcFDs))
	for i, fd := range srcFDs {
		for _, e := range fd.entries {
			if e.op != opPrivate {
				fdEnts[i] = append(fdEnts[i], e)
			}
		}
	}
	if !c.isCID { // 合成 FD 需 FontName 条目
		sid := addString(c.name)
		fdEnts[0] = append([]dictEntry{{op: 0x0C00 | 38, args: []dictNum{{i: int64(sid)}}}}, fdEnts[0]...)
	}
	// 8. Top DICT 条目骨架：剔除按布局重建的条目
	var topBase []dictEntry
	drop := map[uint16]bool{
		opCharset: true, opEncoding: true, opCharStrings: true, opPrivate: true,
		opCIDCount: true, opFDArray: true, opFDSelect: true,
	}
	for _, e := range c.topDict {
		if !drop[e.op] {
			topBase = append(topBase, e)
		}
	}
	if !c.isCID { // 非 CID 源补 ROS，转为 CID 键
		topBase = append(topBase, dictEntry{op: opROS, args: []dictNum{
			{i: int64(addString([]byte("Adobe")))},
			{i: int64(addString([]byte("Identity")))},
			{},
		}})
	}
	// 9. 固定内容：Name/String/CharStrings INDEX、charset、FDSelect
	nameIdx := buildIndex([][]byte{c.name})
	strIdx := buildIndex(newStrings)
	csIdx := buildIndex(csItems)
	charsetBytes := buildCFFCharset(n)
	fdSelBytes := buildFDSelect(fdOfNewGID)
	// 10. 布局迭代：偏移影响操作数编码长度，迭代至稳定
	var offs cffLayout
	for iter := 0; iter < 10; iter++ {
		topIdx := buildIndex([][]byte{encodeDict(offs.topEntries(topBase, n))})
		fdIdx := buildIndex(offs.fdDictBytes(fdEnts, privBlobs, privDictLens))
		next := cffLayout{}
		next.charset = len(c.header) + len(nameIdx) + len(topIdx) + len(strIdx) + len(gsubrIdx)
		next.fdSelect = next.charset + len(charsetBytes)
		next.charStrings = next.fdSelect + len(fdSelBytes)
		next.fdArray = next.charStrings + len(csIdx)
		priv := next.fdArray + len(fdIdx)
		next.privates = make([]int, len(privBlobs))
		for i, b := range privBlobs {
			next.privates[i] = priv
			priv += len(b)
		}
		if next.equal(offs) {
			offs = next
			break
		}
		offs = next
		if iter == 9 {
			return nil, fmt.Errorf("ttf: CFF 子集布局迭代未收敛")
		}
	}
	// 11. 组装输出
	topIdx := buildIndex([][]byte{encodeDict(offs.topEntries(topBase, n))})
	fdIdx := buildIndex(offs.fdDictBytes(fdEnts, privBlobs, privDictLens))
	out := make([]byte, 0, offs.privates[len(offs.privates)-1]+len(privBlobs[len(privBlobs)-1]))
	out = append(out, c.header...)
	out = append(out, nameIdx...)
	out = append(out, topIdx...)
	out = append(out, strIdx...)
	out = append(out, gsubrIdx...)
	out = append(out, charsetBytes...)
	out = append(out, fdSelBytes...)
	out = append(out, csIdx...)
	out = append(out, fdIdx...)
	for _, b := range privBlobs {
		out = append(out, b...)
	}
	return out, nil
}

// cffLayout 记录输出 CFF 各区块的偏移（charstrings/fdArray/privates 由迭代确定）。
type cffLayout struct {
	charset     int
	fdSelect    int
	charStrings int
	fdArray     int
	privates    []int
}

// 比较两组布局是否一致（privates 为 nil 视为未初始化）。
func (l cffLayout) equal(o cffLayout) bool {
	if l.charset != o.charset || l.fdSelect != o.fdSelect ||
		l.charStrings != o.charStrings || l.fdArray != o.fdArray ||
		len(l.privates) != len(o.privates) {
		return false
	}
	for i := range l.privates {
		if l.privates[i] != o.privates[i] {
			return false
		}
	}
	return true
}

// 生成 Top DICT 条目：骨架 + 本布局的 charset/CharStrings/FDArray/FDSelect/CIDCount。
func (l cffLayout) topEntries(base []dictEntry, nGlyphs int) []dictEntry {
	ents := append([]dictEntry(nil), base...)
	num := func(v int) dictNum { return dictNum{i: int64(v)} }
	ents = append(ents,
		dictEntry{op: opCharset, args: []dictNum{num(l.charset)}},
		dictEntry{op: opCharStrings, args: []dictNum{num(l.charStrings)}},
		dictEntry{op: opCIDCount, args: []dictNum{num(nGlyphs)}},
		dictEntry{op: opFDArray, args: []dictNum{num(l.fdArray)}},
		dictEntry{op: opFDSelect, args: []dictNum{num(l.fdSelect)}},
	)
	return ents
}

// 生成各 FD 的 Font DICT 字节：骨架 + 本布局的 Private（size offset）。
// size 取字典部分长度（不含紧随的 Subrs INDEX）。
func (l cffLayout) fdDictBytes(fdEnts [][]dictEntry, privBlobs [][]byte, privDictLens []int) [][]byte {
	out := make([][]byte, len(fdEnts))
	for i, ents := range fdEnts {
		e := append([]dictEntry(nil), ents...)
		if len(privBlobs[i]) > 0 {
			privOff := 0 // 首轮迭代尚无布局，以 0 占位（编码长度随后续迭代收敛）
			if i < len(l.privates) {
				privOff = l.privates[i]
			}
			e = append(e, dictEntry{op: opPrivate, args: []dictNum{
				{i: int64(privDictLens[i])},
				{i: int64(privOff)},
			}})
		}
		out[i] = encodeDict(e)
	}
	return out
}

// 生成 charset：CID 与 GID 一致（连续 1..n-1），单区间即可表达。
// 入参: nGlyphs 为输出字形总数（含 .notdef）
// 出参: format 1/2 的 charset 数据
func buildCFFCharset(nGlyphs int) []byte {
	if nGlyphs <= 1 {
		return []byte{1} // format 1，零区间（.notdef 无 charset 条目）
	}
	if nGlyphs-2 <= 255 {
		return []byte{1, 0, 1, byte(nGlyphs - 2)} // format 1：first=1，nLeft=n-2
	}
	return []byte{2, 0, 1, byte((nGlyphs - 2) >> 8), byte(nGlyphs - 2)} // format 2
}

// 生成 FDSelect format 3：新字形序上 FD 相同的连续段合并为区间。
func buildFDSelect(fdOfGID []int) []byte {
	n := len(fdOfGID)
	out := make([]byte, 0, 8)
	out = append(out, 3, 0, 0) // format 3，nRanges 占位
	nRanges := 0
	for i := 0; i < n; {
		j := i
		for j+1 < n && fdOfGID[j+1] == fdOfGID[i] {
			j++
		}
		out = append(out, byte(i>>8), byte(i), byte(fdOfGID[i]))
		nRanges++
		i = j + 1
	}
	out[1], out[2] = byte(nRanges>>8), byte(nRanges)
	out = append(out, byte(n>>8), byte(n)) // 哨兵
	return out
}

// 构造 Private 数据块：字典剔除 Subrs 条目后重编码，重建的局部 Subrs INDEX
// 紧随字典放置，Subrs 操作数改写为新相对偏移（迭代至编码长度稳定）。
// 不能原样复制字典 + 垫齐原偏移：部分字体（如思源宋体）各 FD 字典集中存放、
// Subrs 远置，原偏移可达数 MB，垫零会等比例膨胀。
// 入参: data 为完整 CFF；off/size 为 Private 字典位置（size ≤ 0 表示无 Private）；
// newSubrs 为重建的局部 Subrs INDEX（原 Private 无 Subrs 时忽略）
// 出参: 可直接嵌入输出 CFF 的数据块（空表示无 Private）、字典部分长度、错误
func buildPrivateBlob(data []byte, off, size int, newSubrs []byte) ([]byte, int, error) {
	if size <= 0 {
		return nil, 0, nil
	}
	if off < 0 || off+size > len(data) {
		return nil, 0, fmt.Errorf("ttf: CFF Private 字典越界")
	}
	window := data[off : off+size]
	ents, err := parseDict(window)
	if err != nil {
		return nil, 0, fmt.Errorf("ttf: CFF Private 字典: %w", err)
	}
	subrsOff, hasSubrs := dictInt(ents, opSubrs)
	if !hasSubrs || subrsOff <= 0 {
		blob := append([]byte(nil), window...)
		return blob, len(blob), nil
	}
	if int(subrsOff) < size { // size 含 Subrs 的布局：字典按 Subrs 起点截断后重解析
		ents, err = parseDict(data[off : off+int(subrsOff)])
		if err != nil {
			return nil, 0, fmt.Errorf("ttf: CFF Private 字典: %w", err)
		}
	}
	var base []dictEntry
	for _, e := range ents {
		if e.op != opSubrs {
			base = append(base, e)
		}
	}
	dict := encodeDict(base)
	for iter := 0; iter < 5; iter++ {
		e := append(append([]dictEntry(nil), base...),
			dictEntry{op: opSubrs, args: []dictNum{{i: int64(len(dict))}}})
		next := encodeDict(e)
		if len(next) == len(dict) {
			dict = next
			break
		}
		dict = next
		if iter == 4 {
			return nil, 0, fmt.Errorf("ttf: CFF Private 字典重编码未收敛")
		}
	}
	blob := make([]byte, 0, len(dict)+len(newSubrs))
	blob = append(blob, dict...)
	blob = append(blob, newSubrs...)
	return blob, len(dict), nil
}
