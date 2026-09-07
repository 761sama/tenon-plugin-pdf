package ttf

import (
	"os"
	"testing"
)

// 测试字体路径（完整版思源黑体/Noto Sans SC 变量字体，未提交到仓库）。
const fullFontPath = "/tmp/opencode/NotoSansSC-Regular.ttf"

func loadFull(t *testing.T) *Font {
	t.Helper()
	data, err := os.ReadFile(fullFontPath)
	if err != nil {
		t.Skip("完整字体不可用，跳过")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParse(t *testing.T) {
	f := loadFull(t)
	if f.UnitsPerEm != 1000 {
		t.Errorf("UnitsPerEm = %d", f.UnitsPerEm)
	}
	if f.NumGlyphs < 20000 {
		t.Errorf("NumGlyphs = %d", f.NumGlyphs)
	}
	if f.Ascent <= 0 || f.Descent >= 0 {
		t.Errorf("Ascent/Descent = %d/%d", f.Ascent, f.Descent)
	}
	if f.PSName() == "SubsetFont" {
		t.Error("PSName not parsed")
	}
	t.Logf("PSName=%s glyphs=%d", f.PSName(), f.NumGlyphs)
}

func TestGlyphIndex(t *testing.T) {
	f := loadFull(t)
	for _, r := range "中文ABCabc012" {
		if g := f.GlyphIndex(r); g == 0 {
			t.Errorf("glyph for %q missing", r)
		}
	}
	if g := f.GlyphIndex(0x10FFFF); g != 0 {
		t.Errorf("unexpected glyph for U+10FFFF: %d", g)
	}
}

func TestSubset(t *testing.T) {
	f := loadFull(t)
	text := "中文采购订单 0123 ABCabc"
	glyphs := []GlyphMapping{{Rune: 0, GID: 0}}
	seen := map[rune]bool{}
	for _, r := range text {
		if seen[r] {
			continue
		}
		seen[r] = true
		gid := f.GlyphIndex(r)
		if gid == 0 {
			t.Fatalf("glyph for %q missing", r)
		}
		glyphs = append(glyphs, GlyphMapping{Rune: r, GID: gid})
	}

	sub, err := f.Subset(glyphs)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("subset size: %d bytes (full %d)", len(sub), len(f.data))
	if len(sub) > len(f.data)/10 {
		t.Errorf("subset too large: %d", len(sub))
	}

	// 子集应可再解析，且 cmap 映射到新 gid
	sf, err := Parse(sub)
	if err != nil {
		t.Fatalf("re-parse subset: %v", err)
	}
	if sf.NumGlyphs != len(glyphs) {
		t.Errorf("NumGlyphs = %d, want %d", sf.NumGlyphs, len(glyphs))
	}
	for i, g := range glyphs {
		if g.Rune == 0 {
			continue
		}
		if got := sf.GlyphIndex(g.Rune); int(got) != i {
			t.Errorf("GlyphIndex(%q) = %d, want %d", g.Rune, got, i)
		}
	}
	// 步进宽度应与原字体一致
	for i, g := range glyphs {
		if sf.advances[i] != f.advances[g.GID] {
			t.Errorf("advance[%d] = %d, want %d", i, sf.advances[i], f.advances[g.GID])
		}
	}
}

func TestSubsetCompositeClosure(t *testing.T) {
	f := loadFull(t)
	// "é"（U+00E9）通常是 e +  acute 的复合字形
	gid := f.GlyphIndex('é')
	if gid == 0 {
		t.Skip("é 无字形")
	}
	sub, err := f.Subset([]GlyphMapping{{0, 0}, {'é', gid}})
	if err != nil {
		t.Fatal(err)
	}
	sf, err := Parse(sub)
	if err != nil {
		t.Fatal(err)
	}
	// 若原字形是复合的，组件应被纳入（NumGlyphs > 2）
	d := f.glyphData(gid)
	if len(d) >= 10 && int16(d[1])|int16(d[0]) < 0 { // numberOfContours < 0
		if sf.NumGlyphs < 3 {
			t.Errorf("composite closure failed: NumGlyphs = %d", sf.NumGlyphs)
		}
	}
	if sf.GlyphIndex('é') != 1 {
		t.Errorf("GlyphIndex(é) = %d", sf.GlyphIndex('é'))
	}
}
