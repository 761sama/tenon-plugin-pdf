package font

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// subsetFontPath 随仓库提交的思源黑体演示子集字体。
const subsetFontPath = "../cmd/tenon-pdf/assets/NotoSansSC-Subset.ttf"

func loadSubset(t *testing.T) *CJKFont {
	t.Helper()
	f, err := LoadCJKFile(subsetFontPath)
	if err != nil {
		t.Skip("子集字体资源不可用")
	}
	f.SetName("NotoSansSC")
	return f
}

func TestCJKEncode(t *testing.T) {
	f := loadSubset(t)
	enc := f.Encode("中文AB")
	if len(enc) != 8 { // 4 字符 × 2 字节
		t.Fatalf("Encode len = %d, want 8", len(enc))
	}
	// CID 从 1 开始连续分配
	if enc[0] != 0 || enc[1] != 1 {
		t.Errorf("首个 CID = %d, want 1", uint16(enc[0])<<8|uint16(enc[1]))
	}
	// 相同文本两次编码结果一致（CID 复用）
	if enc2 := f.Encode("中文"); !bytes.Equal(enc[:4], enc2) {
		t.Error("相同文本两次编码不一致")
	}
	// 缺字形字符 → CID 0（.notdef）
	bad := f.Encode("🀄") // 麻将牌，子集字体中不存在
	if bad[0] != 0 || bad[1] != 0 {
		t.Errorf("缺失字符 CID = %d, want 0", uint16(bad[0])<<8|uint16(bad[1]))
	}
}

func TestCJKWidth(t *testing.T) {
	f := loadSubset(t)
	// 中文字形通常全宽（1000/1000 em）
	if w := f.WidthOf("中"); w < 900 || w > 1000 {
		t.Errorf("中 width = %d", w)
	}
	if f.TextWidth("中文", 10) != float64(f.WidthOf("中文"))*10/1000 {
		t.Error("TextWidth scaling wrong")
	}
	if f.Ascent(10) <= 0 || f.Descent(10) >= 0 || f.LineHeight(10) <= 0 {
		t.Error("metrics broken")
	}
}

func TestCJKBuildDict(t *testing.T) {
	f := loadSubset(t)
	f.Encode("采购订单 test 0123")
	w := writer.New()
	d := f.BuildDict(w)
	s := string(d.Encode(nil))
	for _, want := range []string{
		"/Subtype /Type0", "/Encoding /Identity-H",
		"+NotoSansSC", // 子集前缀
		"/DescendantFonts", "/ToUnicode",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Type0 dict missing %q", want)
		}
	}

	// 序列化全部间接对象，验证关键结构
	w.Add(d)
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf, object.Ref{Num: 1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"/Subtype /CIDFontType2", "/CIDToGIDMap /Identity",
		"/FontFile2", "/Length1", "/FlateDecode",
		"/FontDescriptor", "/Flags 4",
		"/W [1 [", // 紧凑宽度数组（CID 连续）
		"beginbfchar", "begincmap",
		"<91C7>", // 采 U+91C7 → ToUnicode
		"<8D2D>", // 购 U+8D2D
	} {
		if !strings.Contains(out, want) {
			t.Errorf("serialized font missing %q", want)
		}
	}

	// 同一 writer 上重复调用返回缓存
	if f.BuildDict(w) != d {
		t.Error("BuildDict should be cached per writer")
	}
}

func TestCJKResourceInterface(t *testing.T) {
	// 编译期与运行期接口断言
	var _ Resource = (*CJKFont)(nil)
	var _ Resource = Helvetica
	var f Resource = loadSubset(t)
	if f.TextWidth("测试", 12) <= 0 {
		t.Error("interface dispatch broken")
	}
}
