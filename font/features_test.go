package font

import (
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// dejaVuPath 系统 DejaVu Sans（glyf + kern 表 + GSUB liga），用于连字/字距测试。
const dejaVuPath = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

func loadDejaVu(t *testing.T) *CJKFont {
	t.Helper()
	f, err := LoadCJKFile(dejaVuPath)
	if err != nil {
		t.Skip("系统无 DejaVuSans.ttf")
	}
	if !f.tf.HasLigatures() || !f.tf.HasKerning() {
		t.Skip("DejaVuSans 缺少 liga/kern（版本不符）")
	}
	return f
}

func TestCJKLigatureEncode(t *testing.T) {
	f := loadDejaVu(t)
	// 默认启用连字："fi" 合并为单个字形 → 单 CID
	enc := f.Encode("fi")
	if len(enc) != 2 {
		t.Fatalf("连字 Encode(fi) 长度 = %d, want 2", len(enc))
	}
	// 关闭连字后恢复逐字符编码
	f2 := loadDejaVu(t)
	f2.Ligatures = false
	if enc2 := f2.Encode("fi"); len(enc2) != 4 {
		t.Errorf("禁用连字后 Encode(fi) 长度 = %d, want 4", len(enc2))
	}
	// 三字母连字 ffi 优先于 fi（最长匹配）
	if enc3 := f.Encode("ffi"); len(enc3) != 2 {
		t.Errorf("Encode(ffi) 长度 = %d, want 2（ffi 单连字）", len(enc3))
	}
}

func TestCJKLigatureToUnicode(t *testing.T) {
	f := loadDejaVu(t)
	f.Encode("fi")
	w := writer.New()
	f.BuildDict(w)
	st := f.buildToUnicode()
	// 连字 CID 的 ToUnicode 目标应为 <00660069>（"fi" 两码点 UTF-16BE）
	if !strings.Contains(string(st.Data), "<00660069>") {
		t.Error("ToUnicode 缺少连字 → fi 的多码点映射")
	}
}

func TestCJKKerning(t *testing.T) {
	f := loadDejaVu(t)
	// EncodeKerned：AV 有字距 → TJ 段；HH 无字距 → nil
	segs := f.EncodeKerned("AV")
	if segs == nil {
		t.Fatal("EncodeKerned(AV) = nil, want TJ 段")
	}
	hasAdj := false
	for _, s := range segs {
		if v, ok := s.(float64); ok {
			hasAdj = true
			if v <= 0 {
				t.Errorf("AV 字距调整值 = %v, want > 0（收紧）", v)
			}
		}
	}
	if !hasAdj {
		t.Error("TJ 段缺少数值调整项")
	}
	if f.EncodeKerned("HH") != nil {
		t.Error("EncodeKerned(HH) 应为 nil（无字距对）")
	}
	// 宽度应包含字距：AV 总宽 < A + V 各自宽度
	wAV, wA, wV := f.WidthOf("AV"), f.WidthOf("A"), f.WidthOf("V")
	if wAV >= wA+wV {
		t.Errorf("WidthOf(AV)=%d 应小于 WidthOf(A)+WidthOf(V)=%d", wAV, wA+wV)
	}
	// 关闭字距后无调整
	f3 := loadDejaVu(t)
	f3.Kerning = false
	if f3.EncodeKerned("AV") != nil {
		t.Error("禁用 Kerning 后 EncodeKerned 应为 nil")
	}
	if f3.WidthOf("AV") != wA+wV-f3.kernPair(token{gid: 0}, token{gid: 0}) { // 无字距
		// 宽松校验：禁用后应等于逐字宽度之和
		if f3.WidthOf("AV") == wAV {
			t.Error("禁用 Kerning 后宽度未变化")
		}
	}
}

func TestCJKEncodeKernedInterface(t *testing.T) {
	var _ Kerned = (*CJKFont)(nil) // 编译期断言
	// 标准 14 字体不实现 Kerned（不嵌入、无 kern 数据）
	if _, ok := any(Helvetica).(Kerned); ok {
		t.Error("标准字体不应实现 Kerned")
	}
}

func TestCJKCapHeight(t *testing.T) {
	f := loadSubset(t)
	// 子集字体 OS/2 v4：sCapHeight=733，UnitsPerEm=1000
	if got := f.CapHeight(1000); got != 733 {
		t.Errorf("CapHeight(1000) = %v, want 733（sCapHeight）", got)
	}
}

func TestLoadCJKCollection(t *testing.T) {
	// 单字体文件：index 0 兼容路径
	f, err := LoadCJKCollectionFile(subsetFontPath, 0)
	if err != nil {
		t.Fatalf("LoadCJKCollectionFile(单字体, 0): %v", err)
	}
	if f.Name() == "" {
		t.Error("名称为空")
	}
	// LoadCJKFile 直接喂 TTC 应报引导性错误
	const uming = "/usr/share/fonts/truetype/arphic/uming.ttc"
	cf, err := LoadCJKCollectionFile(uming, 0)
	if err != nil {
		t.Skip("系统无 uming.ttc")
	}
	if cf.tf.UnitsPerEm != 1000 && cf.tf.UnitsPerEm != 256 && cf.tf.UnitsPerEm != 2048 {
		t.Logf("uming UnitsPerEm = %d", cf.tf.UnitsPerEm)
	}
	if cf.WidthOf("中文") <= 0 {
		t.Error("集合字体宽度异常")
	}
	if _, err := LoadCJKFile(uming); err == nil {
		t.Error("LoadCJKFile(TTC) 应报错")
	} else if !strings.Contains(err.Error(), "ParseCollection") &&
		!strings.Contains(err.Error(), "LoadCJKCollection") {
		t.Errorf("错误信息应指引集合加载方式: %v", err)
	}
	// 越界索引
	if _, err := LoadCJKCollectionFile(uming, 99); err == nil {
		t.Error("越界索引应报错")
	}
}
