package font

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/ttf"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// CJKFont 嵌入子集化 TrueType 字体的 CID 字体（Type0 / Identity-H / CIDFontType2），
// 用于中文等超出 WinAnsi 范围的文本。实现 Resource 与 Kerned 接口。
//
// 工作方式：文本绘制时按 rune 序列做 GSUB 连字替换（可选）后动态分配 CID
// （从 1 起），序列化时（BuildDict）将实际用到的字形子集化嵌入，
// 并生成 ToUnicode CMap 保证文本可提取。源字体带 kern 表时自动应用字距
// （EncodeKerned 输出 TJ 调整数组）。
type CJKFont struct {
	tf   *ttf.Font
	name string

	// Ligatures 为 true 时（默认）应用源字体 GSUB 的 liga/rlig 连字替换。
	Ligatures bool
	// Kerning 为 true 时（默认）应用源字体 kern 表的字距调整。
	Kerning bool

	cids    map[string]uint16 // token 的 Unicode 文本 → CID（从 1 起，0 为 .notdef）
	unis    []string          // CID-1 → Unicode 文本（连字为多码点）
	gids    []uint16          // CID-1 → 原字体字形 ID
	widths  []int32           // CID-1 → 宽度（1/1000 em）
	missing map[rune]bool     // 无字形字符（只警告一次）

	builtFor *writer.Writer
	built    *object.Dict
}

// LoadCJK 从 TrueType 字体文件（glyf 轮廓，如 Noto Sans SC / 思源黑体 TTF 版）加载 CJK 字体。
// 数据为 TTC 集合时报错，请改用 LoadCJKCollection 指定成员索引。
func LoadCJK(r io.Reader) (*CJKFont, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if ttf.IsCollection(data) {
		return nil, fmt.Errorf("font: 数据是 TTC 集合（含 %d 个字体），请用 LoadCJKCollection(r, index)",
			ttf.CollectionCount(data))
	}
	tf, err := ttf.Parse(data)
	if err != nil {
		return nil, err
	}
	return newCJK(tf), nil
}

// LoadCJKFile 从文件加载 CJK 字体。
func LoadCJKFile(path string) (*CJKFont, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadCJK(f)
}

// LoadCJKCollection 从 TTC 集合加载第 index 个成员字体（0 起），如 simsun.ttc。
func LoadCJKCollection(r io.Reader, index int) (*CJKFont, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	tf, err := ttf.ParseCollection(data, index)
	if err != nil {
		return nil, err
	}
	return newCJK(tf), nil
}

// LoadCJKCollectionFile 从 TTC 集合文件加载第 index 个成员字体。
func LoadCJKCollectionFile(path string, index int) (*CJKFont, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadCJKCollection(f, index)
}

func newCJK(tf *ttf.Font) *CJKFont {
	return &CJKFont{
		tf:        tf,
		name:      sanitizePSName(tf.PSName()),
		Ligatures: true,
		Kerning:   true,
		cids:      map[string]uint16{},
		missing:   map[rune]bool{},
	}
}

// Name 返回字体的 PostScript 名称。
func (f *CJKFont) Name() string { return f.name }

// SetName 覆盖字体的 PostScript 名称（用于 /BaseFont 等）。
func (f *CJKFont) SetName(name string) { f.name = sanitizePSName(name) }

// token 排版后的一个输出单元：一个字形及其对应的 Unicode 文本。
// 普通字符 uni 为单 rune；连字为全部组件 rune（保证 ToUnicode 可反查原文）。
type token struct {
	gid uint16
	uni string
}

// tokenize 将文本转换为字形 token 序列：逐 rune 查 cmap，
// 随后（可选）做 GSUB 连字贪心最长匹配。
func (f *CJKFont) tokenize(s string) []token {
	runes := []rune(s)
	toks := make([]token, 0, len(runes))
	for _, r := range runes {
		gid := f.tf.GlyphIndex(r)
		if gid == 0 && !f.missing[r] && r != ' ' && r != '\t' {
			f.missing[r] = true
			fmt.Printf("font: 警告：字符 %q (U+%04X) 在 %s 中无字形，将显示为 .notdef\n", r, r, f.name)
		}
		toks = append(toks, token{gid: gid, uni: string(r)})
	}
	if !f.Ligatures || !f.tf.HasLigatures() {
		return toks
	}
	ligs := f.tf.Ligatures()
	out := make([]token, 0, len(toks))
	for i := 0; i < len(toks); {
		matched := false
		if toks[i].gid != 0 {
			for _, lig := range ligs[toks[i].gid] { // 已按组件数降序
				if i+len(lig.Components) > len(toks)-1 {
					continue // 组件数超出剩余 token
				}
				ok := true
				for j, cg := range lig.Components {
					if toks[i+1+j].gid != cg || cg == 0 {
						ok = false
						break
					}
				}
				if ok {
					var sb strings.Builder
					for k := 0; k <= len(lig.Components); k++ {
						sb.WriteString(toks[i+k].uni)
					}
					out = append(out, token{gid: lig.Glyph, uni: sb.String()})
					i += len(lig.Components) + 1
					matched = true
					break
				}
			}
		}
		if !matched {
			out = append(out, toks[i])
			i++
		}
	}
	return out
}

// cidOfToken 返回 token 的 CID；首次出现时分配。无字形返回 0（.notdef）。
func (f *CJKFont) cidOfToken(t token) uint16 {
	if t.gid == 0 {
		return 0
	}
	if cid, ok := f.cids[t.uni]; ok {
		return cid
	}
	cid := uint16(len(f.unis) + 1)
	f.cids[t.uni] = cid
	f.unis = append(f.unis, t.uni)
	f.gids = append(f.gids, t.gid)
	f.widths = append(f.widths, int32(f.tf.Advance(t.gid))*1000/int32(f.tf.UnitsPerEm))
	return cid
}

// kernPair 返回相邻两 token 的字距修正（1/1000 em，通常 ≤ 0）。
func (f *CJKFont) kernPair(a, b token) int {
	if !f.Kerning || a.gid == 0 || b.gid == 0 {
		return 0
	}
	return f.tf.Kern(a.gid, b.gid) * 1000 / f.tf.UnitsPerEm
}

// Encode 将文本编码为 2 字节大端 CID 序列（连字已替换为单字形）。
func (f *CJKFont) Encode(s string) []byte {
	toks := f.tokenize(s)
	out := make([]byte, 0, len(toks)*2)
	for _, t := range toks {
		cid := f.cidOfToken(t)
		out = append(out, byte(cid>>8), byte(cid))
	}
	return out
}

// EncodeKerned 实现 Kerned：编码为 TJ 数组段，字距非零处插入调整值。
// 全部字距为零时返回 nil（调用方回退 Tj）。
func (f *CJKFont) EncodeKerned(s string) []any {
	toks := f.tokenize(s)
	if len(toks) < 2 {
		return nil
	}
	kerns := make([]int, len(toks)-1)
	anyKern := false
	for i := range kerns {
		kerns[i] = f.kernPair(toks[i], toks[i+1])
		if kerns[i] != 0 {
			anyKern = true
		}
	}
	if !anyKern {
		return nil
	}
	// TJ 调整值 n 使后一字形移动 -n/1000 em；字距 k 要求移动 +k，
	// 故 n = -k（kern 通常为负 → n 为正 → 收紧）。
	segs := make([]any, 0, 2*len(toks))
	var pending []byte
	flush := func() {
		if len(pending) > 0 {
			segs = append(segs, pending)
			pending = nil
		}
	}
	for i, t := range toks {
		cid := f.cidOfToken(t)
		pending = append(pending, byte(cid>>8), byte(cid))
		if i < len(kerns) && kerns[i] != 0 {
			flush()
			segs = append(segs, float64(-kerns[i]))
		}
	}
	flush()
	return segs
}

// WidthOf 返回文本在 1/1000 em 单位下的总宽度（含连字替换与字距调整）。
func (f *CJKFont) WidthOf(s string) int {
	toks := f.tokenize(s)
	w := 0
	for i, t := range toks {
		if cid := f.cidOfToken(t); cid > 0 {
			w += int(f.widths[cid-1])
		}
		if i+1 < len(toks) {
			w += f.kernPair(t, toks[i+1])
		}
	}
	return w
}

// TextWidth 返回文本在给定字号下的宽度（磅）。
func (f *CJKFont) TextWidth(s string, size float64) float64 {
	return float64(f.WidthOf(s)) * size / 1000
}

func (f *CJKFont) scale(v int, size float64) float64 {
	return float64(v) * size / float64(f.tf.UnitsPerEm)
}

// Ascent 返回指定字号下的上升部高度。
func (f *CJKFont) Ascent(size float64) float64 { return f.scale(f.tf.Ascent, size) }

// Descent 返回指定字号下的下降部高度（负值）。
func (f *CJKFont) Descent(size float64) float64 { return f.scale(f.tf.Descent, size) }

// CapHeight 返回指定字号下的大写字母高度（OS/2 sCapHeight；无数据时取上升部的 70% 估算）。
func (f *CJKFont) CapHeight(size float64) float64 {
	if f.tf.CapHeight > 0 {
		return f.scale(f.tf.CapHeight, size)
	}
	return f.scale(f.tf.Ascent, size) * 0.7
}

// LineHeight 返回指定字号下的建议行高。
func (f *CJKFont) LineHeight(size float64) float64 {
	return f.scale(f.tf.Ascent-f.tf.Descent+f.tf.LineGap, size)
}

// BuildDict 实现 Resource：子集化嵌入字体文件，构建 Type0 字体字典。
// 同一文档写入器上重复调用返回缓存结果。
func (f *CJKFont) BuildDict(w *writer.Writer) *object.Dict {
	if f.builtFor == w && f.built != nil {
		return f.built
	}

	// 1. 子集化：新字形顺序 = CID 顺序（故 /CIDToGIDMap 可用 /Identity）
	// 连字字形无单码点对应，cmap 不映射（Rune=0）；ToUnicode 仍保留原文
	glyphs := make([]ttf.GlyphMapping, 0, len(f.unis)+1)
	glyphs = append(glyphs, ttf.GlyphMapping{Rune: 0, GID: 0})
	for i, uni := range f.unis {
		var r rune
		if rs := []rune(uni); len(rs) == 1 {
			r = rs[0]
		}
		glyphs = append(glyphs, ttf.GlyphMapping{Rune: r, GID: f.gids[i]})
	}
	subset, err := f.tf.Subset(glyphs)
	if err != nil {
		// 子集化失败时退化为不嵌入（查看器可能无法显示）
		subset = nil
	}

	// 2. 字体文件流（Flate 压缩 + Length1）
	var fontFileRef object.Object
	if subset != nil {
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		zw.Write(subset)
		zw.Close()
		st := object.NewStream(buf.Bytes())
		st.Dict.Set("Filter", object.Name("FlateDecode"))
		st.Dict.Set("Length1", object.Int(len(subset)))
		fontFileRef = w.Add(st)
	}

	// 子集字体按规范加 6 大写字母前缀
	subsetName := subsetTag(f.unis) + "+" + f.name

	// 3. FontDescriptor
	scale := func(v int) int { return v * 1000 / f.tf.UnitsPerEm }
	desc := object.NewDict()
	desc.Set("Type", object.Name("FontDescriptor"))
	desc.Set("FontName", object.Name(subsetName))
	desc.Set("Flags", object.Int(4)) // Symbolic
	desc.Set("FontBBox", object.Rect(float64(scale(int(f.tf.XMin))), float64(scale(int(f.tf.YMin))),
		float64(scale(int(f.tf.XMax))), float64(scale(int(f.tf.YMax)))))
	desc.Set("ItalicAngle", object.Int(0))
	desc.Set("Ascent", object.Int(scale(f.tf.Ascent)))
	desc.Set("Descent", object.Int(scale(f.tf.Descent)))
	if f.tf.CapHeight > 0 {
		desc.Set("CapHeight", object.Int(scale(f.tf.CapHeight)))
	} else {
		desc.Set("CapHeight", object.Int(scale(f.tf.Ascent)*7/10))
	}
	desc.Set("StemV", object.Int(80))
	if fontFileRef != nil {
		desc.Set("FontFile2", fontFileRef)
	}
	descRef := w.Add(desc)

	// 4. CIDFont（/W 宽度数组：CID 连续 1..n，用紧凑形式）
	var cidFont *object.Dict
	{
		wArr := object.Array{}
		if len(f.widths) > 0 {
			ws := make(object.Array, len(f.widths))
			for i, wd := range f.widths {
				ws[i] = object.Int(wd)
			}
			wArr = object.Array{object.Int(1), ws}
		}
		cidFont = object.NewDict()
		cidFont.Set("Type", object.Name("Font"))
		cidFont.Set("Subtype", object.Name("CIDFontType2"))
		cidFont.Set("BaseFont", object.Name(subsetName))
		cidFont.Set("CIDSystemInfo", object.NewDict().
			Set("Registry", object.Str("Adobe")).
			Set("Ordering", object.Str("Identity")).
			Set("Supplement", object.Int(0)))
		cidFont.Set("FontDescriptor", descRef)
		cidFont.Set("DW", object.Int(1000))
		if len(wArr) > 0 {
			cidFont.Set("W", wArr)
		}
		cidFont.Set("CIDToGIDMap", object.Name("Identity"))
	}

	// 5. ToUnicode CMap
	toUnicodeRef := w.Add(f.buildToUnicode())

	d := object.NewDict()
	d.Set("Type", object.Name("Font"))
	d.Set("Subtype", object.Name("Type0"))
	d.Set("BaseFont", object.Name(subsetName))
	d.Set("Encoding", object.Name("Identity-H"))
	d.Set("DescendantFonts", object.Array{cidFont})
	d.Set("ToUnicode", toUnicodeRef)

	f.builtFor = w
	f.built = d
	return d
}

// buildToUnicode 生成 ToUnicode CMap 流：CID → Unicode（UTF-16BE）。
// 连字 CID 映射为多码点序列（如 "fi"），保证文本提取还原原文。
func (f *CJKFont) buildToUnicode() *object.Stream {
	var sb strings.Builder
	sb.WriteString(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
`)
	// bfchar 每段最多 100 条
	for i := 0; i < len(f.unis); i += 100 {
		j := i + 100
		if j > len(f.unis) {
			j = len(f.unis)
		}
		fmt.Fprintf(&sb, "%d beginbfchar\n", j-i)
		for k := i; k < j; k++ {
			fmt.Fprintf(&sb, "<%04X> <", k+1)
			for _, u := range utf16.Encode([]rune(f.unis[k])) {
				fmt.Fprintf(&sb, "%04X", u)
			}
			sb.WriteString(">\n")
		}
		sb.WriteString("endbfchar\n")
	}
	sb.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return object.NewStream([]byte(sb.String()))
}

// sanitizePSName 清洗 PostScript 名称中的非法字符。
func sanitizePSName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "SubsetFont"
	}
	return b.String()
}

// subsetTag 依据已用字符集生成确定性的 6 大写字梅子集前缀。
func subsetTag(unis []string) string {
	h := uint32(5381)
	for _, u := range unis {
		for _, r := range u {
			h = h*33 + uint32(r)
		}
	}
	tag := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		tag[i] = byte('A' + h%26)
		h /= 26
	}
	return string(tag)
}
