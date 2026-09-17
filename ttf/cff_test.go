package ttf

import (
	"os"
	"testing"
)

// 测试用 CFF 字体经环境变量指定（均为 CID 键 CFF 轮廓的思源宋体）：
//   TENONPDF_TEST_HAN_OTF — SourceHanSerifSC-Regular.otf 完整路径
//   TENONPDF_TEST_HAN_OTC — SourceHanSerif-Regular.ttc（OTC 集合）完整路径
// 未设置时相关测试自动跳过。

// 返回环境变量 TENONPDF_TEST_HAN_OTF 指定的 OTF 路径；未设置时跳过测试
func hanOTFPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("TENONPDF_TEST_HAN_OTF")
	if p == "" {
		t.Skip("未设置 TENONPDF_TEST_HAN_OTF（思源宋体 OTF 路径）")
	}
	return p
}

// 返回环境变量 TENONPDF_TEST_HAN_OTC 指定的 OTC 集合路径；未设置时跳过测试
func hanOTCPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("TENONPDF_TEST_HAN_OTC")
	if p == "" {
		t.Skip("未设置 TENONPDF_TEST_HAN_OTC（思源宋体 OTC 路径）")
	}
	return p
}

// 加载思源宋体 OTF；资源不可用时跳过测试
func loadHanOTF(t *testing.T) *Font {
	t.Helper()
	data, err := os.ReadFile(hanOTFPath(t))
	if err != nil {
		t.Skip("思源宋体 OTF 不可用")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("解析 OTF: %v", err)
	}
	return f
}

// 测试 OTF（CFF 轮廓）解析：格式标记、度量、cmap 与 PostScript 名称
func TestParseOTF(t *testing.T) {
	f := loadHanOTF(t)
	if !f.IsCFF() {
		t.Error("OTF 应标记为 CFF 轮廓")
	}
	if f.UnitsPerEm != 1000 {
		t.Errorf("UnitsPerEm = %d, want 1000", f.UnitsPerEm)
	}
	if f.Ascent <= 0 || f.Descent >= 0 {
		t.Errorf("度量异常: Ascent=%d Descent=%d", f.Ascent, f.Descent)
	}
	if gid := f.GlyphIndex('中'); gid == 0 {
		t.Error("GlyphIndex(中) = 0")
	} else if f.Advance(gid) != 1000 {
		t.Errorf("Advance(中) = %d, want 1000", f.Advance(gid))
	}
	if f.PSName() != "SourceHanSerifSC-Regular" {
		t.Errorf("PSName = %q", f.PSName())
	}
}

// 测试 CFF 子集化：输出可被再解析，CID 重编号、CharStrings/FDSelect/FDArray 收敛
func TestSubsetCFF(t *testing.T) {
	f := loadHanOTF(t)
	text := "中文测试 Hello 采购订单"
	seen := map[uint16]bool{0: true}
	glyphs := []GlyphMapping{{Rune: 0, GID: 0}}
	for _, r := range text {
		gid := f.GlyphIndex(r)
		if !seen[gid] {
			seen[gid] = true
			glyphs = append(glyphs, GlyphMapping{Rune: r, GID: gid})
		}
	}
	sub, err := f.Subset(glyphs)
	if err != nil {
		t.Fatalf("CFF 子集化: %v", err)
	}
	// 子集应远小于原字体（原 CFF 约 22MB）
	if len(sub) > 100*1024 {
		t.Errorf("子集大小 = %d 字节，超过 100KB", len(sub))
	}
	c, err := parseCFF(sub)
	if err != nil {
		t.Fatalf("再解析子集 CFF: %v", err)
	}
	if !c.isCID {
		t.Error("子集应为 CID 键字体")
	}
	n := len(glyphs)
	if len(c.charstrings) != n {
		t.Errorf("CharStrings 数 = %d, want %d", len(c.charstrings), n)
	}
	if cidCount, ok := dictInt(c.topDict, opCIDCount); !ok || int(cidCount) != n {
		t.Errorf("CIDCount = %d, want %d", cidCount, n)
	}
	if len(c.fdSelect) != n {
		t.Errorf("FDSelect 长度 = %d, want %d", len(c.fdSelect), n)
	}
	for gid, fd := range c.fdSelect {
		if int(fd) >= len(c.fds) {
			t.Errorf("FDSelect[%d] = %d 越界（FDArray %d 个）", gid, fd, len(c.fds))
		}
	}
	// 子程序闭包：新 CharStrings 应可执行式解析（局部/全局 Subrs 引用完整）
	locals := make([][][]byte, len(c.fds))
	for i, fd := range c.fds {
		locals[i], err = c.localSubrs(fd.privateOff, fd.privateSize)
		if err != nil {
			t.Fatalf("子集局部 Subrs: %v", err)
		}
	}
	for gid := range c.charstrings {
		p := newCSParser(c.gsubrItems, locals[c.fdSelect[gid]])
		if _, err := p.ParseGlyph(c.charstrings[gid]); err != nil {
			t.Errorf("子集字形 %d 执行式解析: %v", gid, err)
		}
	}
}

// 测试 OTC 集合（CFF 版 TTC）：成员枚举、按索引解析与子集化
func TestParseOTC(t *testing.T) {
	data, err := os.ReadFile(hanOTCPath(t))
	if err != nil {
		t.Skip("思源宋体 OTC 不可用")
	}
	n := CollectionCount(data)
	if n < 2 {
		t.Fatalf("OTC 成员数 = %d", n)
	}
	names := CollectionNames(data)
	if names[2] != "SourceHanSerifSC-Regular" {
		t.Errorf("OTC[2] = %q, want SourceHanSerifSC-Regular", names[2])
	}
	f, err := ParseCollection(data, 2)
	if err != nil {
		t.Fatalf("解析 OTC[2]: %v", err)
	}
	if !f.IsCFF() {
		t.Error("OTC 成员应标记为 CFF 轮廓")
	}
	gid := f.GlyphIndex('中')
	sub, err := f.Subset([]GlyphMapping{{Rune: 0, GID: 0}, {Rune: '中', GID: gid}})
	if err != nil {
		t.Fatalf("OTC 成员子集化: %v", err)
	}
	c, err := parseCFF(sub)
	if err != nil {
		t.Fatalf("再解析 OTC 子集: %v", err)
	}
	if len(c.charstrings) != 2 {
		t.Errorf("CharStrings 数 = %d, want 2", len(c.charstrings))
	}
}

// 测试非 CID 键 CFF（普通拉丁 OTF 布局）的子集化：
// 合成最小非 CID CFF 字体，验证转换为 CID 键结构（ROS/单 FD/FDSelect）
func TestSubsetCFFNonCID(t *testing.T) {
	fontBytes := buildTestNonCIDFont(t)
	f, err := Parse(fontBytes)
	if err != nil {
		t.Fatalf("解析合成字体: %v", err)
	}
	if !f.IsCFF() {
		t.Fatal("合成字体应为 CFF 轮廓")
	}
	if gid := f.GlyphIndex('A'); gid != 1 {
		t.Fatalf("GlyphIndex(A) = %d, want 1", gid)
	}
	sub, err := f.Subset([]GlyphMapping{{Rune: 0, GID: 0}, {Rune: 'A', GID: 1}, {Rune: 'B', GID: 2}})
	if err != nil {
		t.Fatalf("非 CID CFF 子集化: %v", err)
	}
	c, err := parseCFF(sub)
	if err != nil {
		t.Fatalf("再解析子集: %v", err)
	}
	if !c.isCID {
		t.Error("非 CID 源应转换为 CID 键（补 ROS）")
	}
	if len(c.fds) != 1 {
		t.Errorf("FDArray 数 = %d, want 1（合成单 FD）", len(c.fds))
	}
	if len(c.charstrings) != 3 {
		t.Fatalf("CharStrings 数 = %d, want 3", len(c.charstrings))
	}
	for gid, fd := range c.fdSelect {
		if fd != 0 {
			t.Errorf("FDSelect[%d] = %d, want 0", gid, fd)
		}
	}
	// 字形 1（A）轮廓：rmoveto + rlineto 的方框
	locals, err := c.localSubrs(c.fds[0].privateOff, c.fds[0].privateSize)
	if err != nil {
		t.Fatal(err)
	}
	p := newCSParser(c.gsubrItems, locals)
	instrs, err := p.ParseGlyph(c.charstrings[1])
	if err != nil {
		t.Fatalf("子集字形 1 解析: %v", err)
	}
	var haveMove, haveLine bool
	for _, in := range instrs {
		if in.op == 21 && len(in.args) == 2 && in.args[0].i == 50 {
			haveMove = true
		}
		if in.op == 5 && len(in.args) == 6 {
			haveLine = true
		}
	}
	if !haveMove || !haveLine {
		t.Errorf("字形 1 轮廓指令异常: move=%v line=%v", haveMove, haveLine)
	}
	// 字形 2（B）经局部子程序绘制：闭包应纳入该子程序
	p2 := newCSParser(c.gsubrItems, locals)
	if _, err := p2.ParseGlyph(c.charstrings[2]); err != nil {
		t.Fatalf("子集字形 2 解析: %v", err)
	}
	if len(p2.UsedL) != 1 {
		t.Errorf("字形 2 引用的局部子程序数 = %d, want 1", len(p2.UsedL))
	}
}

// 构造最小非 CID 键 CFF 字体（3 字形：.notdef/A/B，B 经局部子程序绘制），
// 返回完整 OTF 文件字节。
func buildTestNonCIDFont(t *testing.T) []byte {
	t.Helper()
	// charstring：方框轮廓（50,0 起 100×100）
	box := []byte{189, 140, 21, 239, 140, 140, 239, 39, 140, 5} // rmoveto + rlineto
	csItems := [][]byte{
		{14},                                    // .notdef：endchar
		append(append([]byte(nil), box...), 14), // A：方框 + endchar
		{32, 10, 14},                            // B：callsubr 0（-107+107）+ endchar
	}
	csIdx := buildIndex(csItems)
	charset := []byte{0, 0, 34, 0, 35}                                         // format 0：GID1→SID 34(A)，GID2→SID 35(B)
	localSubr := buildIndex([][]byte{append(append([]byte(nil), box...), 11)}) // subr0：方框 + return
	privDict := []byte{141, 19}                                                // Subrs 偏移 2（紧随字典）
	// CFF 布局：header + Name + TopDICT + String + GSubr + charset + CharStrings + Private + LocalSubrs
	nameIdx := buildIndex([][]byte{[]byte("TestNonCID")})
	strIdx := buildIndex(nil)
	gsubrIdx := buildIndex(nil)
	header := []byte{1, 0, 4, 4}
	// Top DICT 偏移需迭代（操作数编码长度随偏移变化）
	var topIdx []byte
	charsetOff, csOff, privOff := 0, 0, 0
	for i := 0; i < 5; i++ {
		topIdx = buildIndex([][]byte{encodeDict([]dictEntry{
			{op: 5, args: []dictNum{{i: 0}, {i: 0}, {i: 500}, {i: 700}}}, // FontBBox
			{op: opCharset, args: []dictNum{{i: int64(charsetOff)}}},
			{op: opCharStrings, args: []dictNum{{i: int64(csOff)}}},
			{op: opPrivate, args: []dictNum{{i: int64(len(privDict))}, {i: int64(privOff)}}},
		})})
		base := len(header) + len(nameIdx) + len(topIdx) + len(strIdx) + len(gsubrIdx)
		nCharset, nCS, nPriv := base, base+len(charset), base+len(charset)+len(csIdx)
		if nCharset == charsetOff && nCS == csOff && nPriv == privOff {
			break
		}
		charsetOff, csOff, privOff = nCharset, nCS, nPriv
	}
	cff := append([]byte(nil), header...)
	cff = append(cff, nameIdx...)
	cff = append(cff, topIdx...)
	cff = append(cff, strIdx...)
	cff = append(cff, gsubrIdx...)
	cff = append(cff, charset...)
	cff = append(cff, csIdx...)
	cff = append(cff, privDict...)
	cff = append(cff, localSubr...)
	// sfnt 各表
	head := make([]byte, 54)
	putU16(head, 18, 1000) // unitsPerEm
	hhea := make([]byte, 36)
	putU16(hhea, 4, 800)
	putU16(hhea, 6, uint16(65336)) // -200
	putU16(hhea, 34, 3)            // numberOfHMetrics
	maxp := make([]byte, 6)
	putU32(maxp, 0, 0x00005000) // version 0.5（CFF）
	putU16(maxp, 4, 3)
	hmtx := make([]byte, 12)
	putU16(hmtx, 0, 500)
	putU16(hmtx, 4, 600)
	putU16(hmtx, 8, 700)
	cmap, err := buildCmap([]GlyphMapping{{Rune: 'A', GID: 1}, {Rune: 'B', GID: 2}},
		map[uint16]int{1: 1, 2: 2})
	if err != nil {
		t.Fatal(err)
	}
	fontBytes, err := assemble(map[string][]byte{
		"head": head, "hhea": hhea, "maxp": maxp, "hmtx": hmtx,
		"cmap": cmap, "CFF ": cff,
	})
	if err != nil {
		t.Fatal(err)
	}
	copy(fontBytes, "OTTO") // assemble 固定写 0x00010000，改回 CFF 标记（校验和不被解析侧使用）
	return fontBytes
}
