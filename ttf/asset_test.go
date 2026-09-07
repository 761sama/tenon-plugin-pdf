package ttf

import (
	"os"
	"testing"
)

// assetPath 随仓库提交的思源黑体演示子集字体（本身也是合法 TTF）。
const assetPath = "../cmd/tenon-pdf/assets/NotoSansSC-Subset.ttf"

// TestSubsetAssetRoundTrip 对已子集化的字体再次解析与子集化。
func TestSubsetAssetRoundTrip(t *testing.T) {
	data, err := os.ReadFile(assetPath)
	if err != nil {
		t.Skip("子集字体资源不可用")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("解析子集字体: %v", err)
	}
	gid := f.GlyphIndex('中')
	if gid == 0 {
		t.Fatal("子集字体缺少“中”")
	}
	sub, err := f.Subset([]GlyphMapping{{Rune: 0, GID: 0}, {Rune: '中', GID: gid}})
	if err != nil {
		t.Fatal(err)
	}
	sf, err := Parse(sub)
	if err != nil {
		t.Fatalf("再解析: %v", err)
	}
	if sf.GlyphIndex('中') != 1 {
		t.Errorf("GlyphIndex(中) = %d, want 1", sf.GlyphIndex('中'))
	}
	if sf.NumGlyphs < 2 {
		t.Errorf("NumGlyphs = %d", sf.NumGlyphs)
	}
	t.Logf("二次子集: %d → %d 字节", len(data), len(sub))
}
