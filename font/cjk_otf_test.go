package font

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// 测试用 CFF 字体经环境变量指定（均为 CID 键 CFF 轮廓的思源宋体）：
//   TENONPDF_TEST_HAN_OTF — SourceHanSerifSC-Regular.otf 完整路径
//   TENONPDF_TEST_HAN_OTC — SourceHanSerif-Regular.ttc（OTC 集合）完整路径
// 未设置时相关测试自动跳过。

// 加载思源宋体 OTF；资源不可用时跳过测试
func loadHanOTF(t *testing.T) *CJKFont {
	t.Helper()
	p := os.Getenv("TENONPDF_TEST_HAN_OTF")
	if p == "" {
		t.Skip("未设置 TENONPDF_TEST_HAN_OTF（思源宋体 OTF 路径）")
	}
	f, err := LoadCJKFile(p)
	if err != nil {
		t.Skip("思源宋体 OTF 不可用")
	}
	return f
}

// 测试 OTF 字体的 Type0 字典：CIDFontType0 + FontFile3（/CIDFontType0C），
// 且无 CIDFontType2 特有的 /CIDToGIDMap 与 /FontFile2
func TestCJKOTFBuildDict(t *testing.T) {
	f := loadHanOTF(t)
	f.Encode("思源宋体测试")
	w := writer.New()
	d := f.BuildDict(w)
	w.Add(d)
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf, object.Ref{Num: 1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"/Subtype /Type0", "/Encoding /Identity-H",
		"/Subtype /CIDFontType0", // 注意不能误判 /CIDFontType0C：前缀匹配不含 C 结尾
		"/FontFile3", "/CIDFontType0C", "/FlateDecode",
		"/FontDescriptor", "/ToUnicode",
		"<601D>", // 思 U+601D → ToUnicode
		"+SourceHanSerifSC-Regular",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("序列化缺 %q", want)
		}
	}
	for _, unwanted := range []string{"/CIDToGIDMap", "/FontFile2", "/CIDFontType2"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("CFF 嵌入不应出现 %q", unwanted)
		}
	}
}

// 测试 OTF 字体的宽度与度量（hmtx/cmap 路径与 glyf 字体一致）
func TestCJKOTFWidth(t *testing.T) {
	f := loadHanOTF(t)
	if w := f.WidthOf("中"); w != 1000 {
		t.Errorf("WidthOf(中) = %d, want 1000", w)
	}
	if f.TextWidth("中文", 10) != 20 {
		t.Errorf("TextWidth(中文,10) = %v, want 20", f.TextWidth("中文", 10))
	}
	if f.Ascent(10) <= 0 || f.Descent(10) >= 0 || f.LineHeight(10) <= 0 {
		t.Error("度量异常")
	}
}

// 测试 OTC 集合（CFF 版 TTC）加载：按索引取成员、集合喂 LoadCJKFile 报引导性错误
func TestCJKOTCCollection(t *testing.T) {
	otcPath := os.Getenv("TENONPDF_TEST_HAN_OTC")
	if otcPath == "" {
		t.Skip("未设置 TENONPDF_TEST_HAN_OTC（思源宋体 OTC 路径）")
	}
	f, err := LoadCJKCollectionFile(otcPath, 2) // 成员 2 = SourceHanSerifSC-Regular
	if err != nil {
		t.Skip("思源宋体 OTC 不可用")
	}
	if !strings.Contains(f.Name(), "SourceHanSerifSC") {
		t.Errorf("名称 = %q", f.Name())
	}
	if f.WidthOf("中文") <= 0 {
		t.Error("集合字体宽度异常")
	}
	if _, err := LoadCJKFile(otcPath); err == nil {
		t.Error("LoadCJKFile(OTC) 应报错")
	} else if !strings.Contains(err.Error(), "LoadCJKCollection") {
		t.Errorf("错误信息应指引集合加载方式: %v", err)
	}
	if _, err := LoadCJKCollectionFile(otcPath, 99); err == nil {
		t.Error("越界索引应报错")
	}
}
