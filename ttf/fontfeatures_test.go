package ttf

import (
	"os"
	"testing"
)

// makeTTC 用若干独立 TTF 字节流合成最小 TTC 集合（各字体独立排布，不共享表）。
// 注意：TTC 中表目录的偏移是相对整个文件起始的绝对偏移，
// 因此每个字体的表记录偏移须加上其基础位置。
func makeTTC(fonts ...[]byte) []byte {
	n := len(fonts)
	headerLen := 12 + 4*n
	out := make([]byte, headerLen)
	putU32(out, 0, ttcfTag)
	putU32(out, 4, 0x00010000)
	putU32(out, 8, uint32(n))
	for i, fd := range fonts {
		base := len(out)
		putU32(out, 12+4*i, uint32(base))
		fd = append([]byte(nil), fd...)
		numTables := int(u16(fd, 4))
		for j := 0; j < numTables; j++ {
			rec := 12 + j*16
			putU32(fd, rec+8, u32(fd, rec+8)+uint32(base))
		}
		out = append(out, fd...)
	}
	return out
}

func TestSyntheticCollection(t *testing.T) {
	data, err := os.ReadFile(assetPath)
	if err != nil {
		t.Skip("子集字体资源不可用")
	}
	if IsCollection(data) {
		t.Fatal("单字体被误判为集合")
	}
	ttc := makeTTC(data, data)
	if !IsCollection(ttc) || CollectionCount(ttc) != 2 {
		t.Fatalf("TTC 识别失败: IsCollection=%v count=%d", IsCollection(ttc), CollectionCount(ttc))
	}
	// Parse 对集合应报明确错误
	if _, err := Parse(ttc); err == nil {
		t.Fatal("Parse 应拒绝 TTC 集合")
	}
	for i := 0; i < 2; i++ {
		f, err := ParseCollection(ttc, i)
		if err != nil {
			t.Fatalf("ParseCollection(%d): %v", i, err)
		}
		if f.GlyphIndex('中') == 0 {
			t.Errorf("集合字体 %d 缺少“中”", i)
		}
		if f.UnitsPerEm != 1000 {
			t.Errorf("集合字体 %d UnitsPerEm = %d", i, f.UnitsPerEm)
		}
	}
	// 越界索引
	if _, err := ParseCollection(ttc, 2); err == nil {
		t.Error("越界索引应报错")
	}
	// 单字体 + index 0 走兼容路径
	if _, err := ParseCollection(data, 0); err != nil {
		t.Errorf("单字体 ParseCollection(data, 0): %v", err)
	}
	// 名称列表
	names := CollectionNames(ttc)
	if len(names) != 2 || names[0] == "" {
		t.Errorf("CollectionNames = %v", names)
	}
}

// TestRealCollection 用系统 AR PL UMing 集合（存在时）做真实验证。
func TestRealCollection(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/arphic/uming.ttc")
	if err != nil {
		t.Skip("系统无 uming.ttc")
	}
	n := CollectionCount(data)
	if n < 2 {
		t.Fatalf("uming.ttc 字体数 = %d", n)
	}
	t.Logf("uming.ttc: %d 个字体 %v", n, CollectionNames(data))
	f, err := ParseCollection(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "中文测试" {
		if f.GlyphIndex(r) == 0 {
			t.Errorf("uming.ttc[0] 缺少 %q", r)
		}
	}
}

// TestCFFCollectionRejected CFF 轮廓的集合成员应报“不支持的格式”。
func TestCFFCollectionRejected(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc")
	if err != nil {
		t.Skip("系统无 NotoSerifCJK ttc")
	}
	if !IsCollection(data) {
		t.Fatal("未识别为集合")
	}
	if _, err := ParseCollection(data, 0); err == nil {
		t.Fatal("CFF 集合成员应被拒绝")
	} else {
		t.Logf("预期报错: %v", err)
	}
}

func TestCapHeight(t *testing.T) {
	// 仓库自带子集字体 OS/2 v4，sCapHeight=733
	data, err := os.ReadFile(assetPath)
	if err != nil {
		t.Skip("子集字体资源不可用")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.CapHeight != 733 {
		t.Errorf("CapHeight = %d, want 733", f.CapHeight)
	}
}

func TestKernTable(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skip("系统无 DejaVuSans.ttf")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasKerning() {
		t.Fatal("DejaVuSans 应有 kern 表")
	}
	// "AV" 是几乎所有带 kern 的字体都收紧的对
	k := f.Kern(f.GlyphIndex('A'), f.GlyphIndex('V'))
	if k >= 0 {
		t.Errorf("Kern(A,V) = %d, want < 0", k)
	}
	// 无调整的对
	if k := f.Kern(f.GlyphIndex('H'), f.GlyphIndex('H')); k != 0 {
		t.Errorf("Kern(H,H) = %d, want 0", k)
	}
	t.Logf("Kern(A,V) = %d 字体单位", k)
}

func TestGSUBLigatures(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skip("系统无 DejaVuSans.ttf")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasLigatures() {
		t.Fatal("DejaVuSans 应有连字规则")
	}
	fGID := f.GlyphIndex('f')
	ligs := f.Ligatures()[fGID]
	if len(ligs) == 0 {
		t.Fatal("缺少以 f 开头的连字")
	}
	// 组件按长度降序（ffi/ffl 在 fi 之前）
	for i := 0; i+1 < len(ligs); i++ {
		if len(ligs[i].Components) < len(ligs[i+1].Components) {
			t.Error("连字未按组件数降序")
		}
	}
	// 应存在 f+i → 连字
	iGID := f.GlyphIndex('i')
	found := false
	for _, l := range ligs {
		if len(l.Components) == 1 && l.Components[0] == iGID {
			found = true
			if l.Glyph == 0 || l.Glyph == fGID {
				t.Errorf("连字字形非法: %d", l.Glyph)
			}
		}
	}
	if !found {
		t.Error("缺少 fi 连字")
	}
}

// TestSubsetCmap12 非 BMP 码点应写入 format 12 子表并可反查。
func TestSubsetCmap12(t *testing.T) {
	f := loadFull(t)
	// 从字体自身的 cmap12 里找一个非 BMP 码点
	var target rune
	if f.cmap12 != nil {
		d := f.cmap12
		n := int(u32(d, 12))
		for i := 0; i < n; i++ {
			start := u32(d, 16+i*12)
			if start > 0xFFFF {
				target = rune(start)
				break
			}
		}
	}
	if target == 0 {
		t.Skip("字体不含非 BMP 字形")
	}
	glyphs := []GlyphMapping{{0, 0}, {'中', f.GlyphIndex('中')}, {target, f.GlyphIndex(target)}}
	sub, err := f.Subset(glyphs)
	if err != nil {
		t.Fatal(err)
	}
	sf, err := Parse(sub)
	if err != nil {
		t.Fatal(err)
	}
	if sf.cmap12 == nil {
		t.Fatal("子集缺少 format 12 cmap 子表")
	}
	if got := sf.GlyphIndex(target); got != 2 {
		t.Errorf("GlyphIndex(U+%X) = %d, want 2", target, got)
	}
	if got := sf.GlyphIndex('中'); got != 1 {
		t.Errorf("GlyphIndex(中) = %d, want 1", got)
	}
}
