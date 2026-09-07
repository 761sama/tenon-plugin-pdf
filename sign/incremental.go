package sign

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 多重签名（增量更新，ISO 32000-1 §7.5.6 / §12.8）：
// 第一个签名字段由 pdf.Document.SetSignature 在生成期写入；
// 后续每个签名通过 AppendSignatureField 追加一个增量修订段
// （新签名字段部件 + 重定义的 AcroForm/Page 对象 + 新 xref 子段 + 新 trailer），
// 再用 Sign 回填。每个签名的 ByteRange 覆盖其签署时刻的文件内容，
// 前序签名因此不被后序签名破坏——这是 Acrobat 会签的标准机制。

// AppendSignatureField 在 doc（必须已含至少一个签名字段）末尾追加一个
// 增量修订段，内含新的签名字段占位符。返回追加后的完整文档，
// 对其调用 Sign 即可完成该字段的签名。
func AppendSignatureField(doc []byte, f *Field) ([]byte, error) {
	if f == nil {
		f = &Field{}
	}
	// 1. 上一个 xref 位置与 trailer
	prevXref, err := lastStartxref(doc)
	if err != nil {
		return nil, err
	}
	trailer, err := lastTrailer(doc, prevXref)
	if err != nil {
		return nil, err
	}
	rootNum := trailer.root
	if rootNum == 0 {
		return nil, fmt.Errorf("sign: trailer 缺少 /Root")
	}

	// 2. Catalog → AcroForm → Fields
	catalog, err := objectBody(doc, rootNum)
	if err != nil {
		return nil, err
	}
	acroNum := refValue(catalog, "AcroForm")
	if acroNum == 0 {
		return nil, fmt.Errorf("sign: 基础文档无 AcroForm（首个签名请用 pdf.Document.SetSignature）")
	}
	acro, err := objectBody(doc, acroNum)
	if err != nil {
		return nil, err
	}
	fields := arrayBody(acro, "Fields")
	if fields == "" {
		return nil, fmt.Errorf("sign: AcroForm 缺少 /Fields")
	}

	// 3. 页树 → 目标页对象号
	pagesNum := refValue(catalog, "Pages")
	if pagesNum == 0 {
		return nil, fmt.Errorf("sign: Catalog 缺少 /Pages")
	}
	pages, err := objectBody(doc, pagesNum)
	if err != nil {
		return nil, err
	}
	kidNums := arrayRefs(arrayBody(pages, "Kids"))
	if len(kidNums) == 0 {
		return nil, fmt.Errorf("sign: 页树为空")
	}
	pageIdx := f.PageIndex
	if pageIdx < 0 || pageIdx >= len(kidNums) {
		pageIdx = 0
	}
	pageNum := kidNums[pageIdx]
	pageBody, err := objectBody(doc, pageNum)
	if err != nil {
		return nil, err
	}

	// 4. 新对象：签名字段部件（含定长占位符）
	fieldNum := trailer.size // 下一个可用对象号
	// 可见签名：追加外观流对象（/AP /N），内联 Helvetica 字体字典保持 sign 包零依赖
	apNum := 0
	if f.Visible() {
		apNum = fieldNum + 1
	}
	var fb bytes.Buffer
	fmt.Fprintf(&fb, "%d 0 obj\n", fieldNum)
	fb.WriteString("<< /Type /Annot /Subtype /Widget /FT /Sig")
	name := f.Name
	if name == "" {
		name = fmt.Sprintf("Signature%d", len(arrayRefs(fields))+1)
	}
	fb.WriteString(" /T " + pdfTextStr(name))
	fmt.Fprintf(&fb, " /Rect [%.4g %.4g %.4g %.4g]", f.Rect[0], f.Rect[1], f.Rect[2], f.Rect[3])
	if f.Visible() {
		fb.WriteString(" /F 4") // Print（可见）
		fmt.Fprintf(&fb, " /AP << /N %d 0 R >>", apNum)
	} else {
		fb.WriteString(" /F 2") // Hidden
	}
	fmt.Fprintf(&fb, " /P %d 0 R", pageNum)
	fb.WriteString(" /V << /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached")
	fb.WriteString(" /ByteRange " + ByteRangePlaceholder)
	fb.WriteString(" /Contents " + ContentsPlaceholder())
	fmt.Fprintf(&fb, " /M (D:%s)", time.Now().Format("20060102150405Z07'00'"))
	if f.Reason != "" {
		fb.WriteString(" /Reason " + pdfTextStr(f.Reason))
	}
	if f.Location != "" {
		fb.WriteString(" /Location " + pdfTextStr(f.Location))
	}
	if f.Contact != "" {
		fb.WriteString(" /ContactInfo " + pdfTextStr(f.Contact))
	}
	fb.WriteString(" >> >>\nendobj\n")

	// 4.5 可见签名的外观流对象（Form XObject，内联 Helvetica 资源）
	var apb bytes.Buffer
	if apNum > 0 {
		apContent := appearanceContent(f)
		fmt.Fprintf(&apb, "%d 0 obj\n", apNum)
		fmt.Fprintf(&apb, "<< /Type /XObject /Subtype /Form /FormType 1 /BBox [0 0 %.4g %.4g] "+
			"/Resources << /Font << /Helv << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> "+
			"/Length %d >>\nstream\n",
			f.Rect[2]-f.Rect[0], f.Rect[3]-f.Rect[1], len(apContent))
		apb.WriteString(apContent)
		apb.WriteString("\nendstream\nendobj\n")
	}

	// 5. 重定义 AcroForm（Fields 追加新字段引用）
	newAcro := insertArrayRef(acro, "Fields", fieldNum)
	var ab bytes.Buffer
	fmt.Fprintf(&ab, "%d 0 obj\n%s\nendobj\n", acroNum, newAcro)

	// 6. 重定义页面（Annots 追加部件引用）
	newPage := upsertAnnotsRef(pageBody, fieldNum)
	var pb2 bytes.Buffer
	fmt.Fprintf(&pb2, "%d 0 obj\n%s\nendobj\n", pageNum, newPage)

	// 7. 装配修订段：对象体 + xref 子段 + trailer
	out := append([]byte(nil), doc...)
	base := len(out)
	var body bytes.Buffer
	offsets := map[int]int{}
	items := []struct {
		num int
		buf *bytes.Buffer
	}{{acroNum, &ab}, {pageNum, &pb2}, {fieldNum, &fb}}
	if apNum > 0 {
		items = append(items, struct {
			num int
			buf *bytes.Buffer
		}{apNum, &apb})
	}
	maxNum := fieldNum
	if apNum > 0 {
		maxNum = apNum
	}
	for _, item := range items {
		offsets[item.num] = base + body.Len()
		body.Write(item.buf.Bytes())
	}
	out = append(out, body.Bytes()...)

	xrefPos := len(out)
	var xref bytes.Buffer
	xref.WriteString("xref\n")
	// 子段按对象号排序合并连续区间
	nums := []int{acroNum, pageNum, fieldNum}
	if apNum > 0 {
		nums = append(nums, apNum)
	}
	sortInts(nums)
	i := 0
	for i < len(nums) {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		fmt.Fprintf(&xref, "%d %d\n", nums[i], j-i+1)
		for k := i; k <= j; k++ {
			fmt.Fprintf(&xref, "%010d 00000 n \n", offsets[nums[k]])
		}
		i = j + 1
	}
	out = append(out, xref.Bytes()...)

	// trailer：沿用 Root/Info/ID/Encrypt，/Prev 指向上一个 xref
	var tr bytes.Buffer
	tr.WriteString("trailer\n<<")
	fmt.Fprintf(&tr, " /Size %d", maxNum+1)
	fmt.Fprintf(&tr, " /Root %d 0 R", rootNum)
	if trailer.info > 0 {
		fmt.Fprintf(&tr, " /Info %d 0 R", trailer.info)
	}
	if trailer.encrypt > 0 {
		fmt.Fprintf(&tr, " /Encrypt %d 0 R", trailer.encrypt)
	}
	if trailer.id != "" {
		tr.WriteString(" /ID " + trailer.id)
	}
	fmt.Fprintf(&tr, " /Prev %d", prevXref)
	tr.WriteString(" >>\n")
	out = append(out, tr.Bytes()...)
	out = append(out, []byte("startxref\n"+strconv.Itoa(xrefPos)+"\n%%EOF\n")...)
	return out, nil
}

// --- 增量更新所需的最小字节级解析 ---

// trailerInfo 从 trailer 提取的字段。
type trailerInfo struct {
	size    int
	root    int
	info    int
	encrypt int
	id      string // 完整 /ID 数组文本（含方括号）
}

// lastStartxref 定位最后一个 startxref 偏移。
func lastStartxref(doc []byte) (int, error) {
	i := bytes.LastIndex(doc, []byte("startxref"))
	if i < 0 {
		return 0, fmt.Errorf("sign: 找不到 startxref")
	}
	m := regexp.MustCompile(`startxref\s+(\d+)`).FindSubmatch(doc[i:])
	if m == nil {
		return 0, fmt.Errorf("sign: startxref 解析失败")
	}
	return strconv.Atoi(string(m[1]))
}

// lastTrailer 解析 prevXref 之后的 trailer 字典（仅提取增量更新所需键）。
func lastTrailer(doc []byte, prevXref int) (*trailerInfo, error) {
	ti := bytes.Index(doc[prevXref:], []byte("trailer"))
	if ti < 0 {
		return nil, fmt.Errorf("sign: 找不到 trailer")
	}
	td := doc[prevXref+ti:]
	end := bytes.Index(td, []byte(">>"))
	if end < 0 {
		return nil, fmt.Errorf("sign: trailer 字典不完整")
	}
	body := string(td[:end+2])
	t := &trailerInfo{id: arrayText(body, "ID")}
	t.size = intValue(body, "Size")
	t.root = refValueStr(body, "Root")
	t.info = refValueStr(body, "Info")
	t.encrypt = refValueStr(body, "Encrypt")
	if t.size == 0 {
		return nil, fmt.Errorf("sign: trailer 缺少 /Size")
	}
	return t, nil
}

var objRe = func(num int) *regexp.Regexp {
	return regexp.MustCompile(`(?s)(?:^|\n)` + strconv.Itoa(num) + ` 0 obj\s*\n(.*?)\nendobj`)
}

// objectBody 提取对象 N 的本体字节（多个修订段重定义时取最后一个）。
func objectBody(doc []byte, num int) (string, error) {
	matches := objRe(num).FindAllSubmatch(doc, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("sign: 对象 %d 不存在", num)
	}
	return string(matches[len(matches)-1][1]), nil
}

// refValue 从对象本体提取 /Key N 0 R 的对象号。
func refValue(body, key string) int { return refValueStr(body, key) }

func refValueStr(body, key string) int {
	re := regexp.MustCompile(`/` + key + `\s+(\d+)\s+\d+\s+R`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// intValue 提取 /Key N 整数。
func intValue(body, key string) int {
	re := regexp.MustCompile(`/` + key + `\s+(\d+)`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// arrayBody 提取 /Key [ ... ] 的方括号内容。
func arrayBody(body, key string) string {
	i := strings.Index(body, "/"+key)
	if i < 0 {
		return ""
	}
	lb := strings.Index(body[i:], "[")
	rb := strings.Index(body[i:], "]")
	if lb < 0 || rb < 0 || rb < lb {
		return ""
	}
	return body[i+lb+1 : i+rb]
}

// arrayText 提取 /Key [...] 的完整数组文本（含方括号）。
func arrayText(body, key string) string {
	i := strings.Index(body, "/"+key)
	if i < 0 {
		return ""
	}
	lb := strings.Index(body[i:], "[")
	rb := strings.Index(body[i:], "]")
	if lb < 0 || rb < 0 || rb < lb {
		return ""
	}
	return body[i+lb : i+rb+1]
}

// arrayRefs 解析数组文本中的全部 N 0 R 引用。
func arrayRefs(arr string) []int {
	re := regexp.MustCompile(`(\d+)\s+\d+\s+R`)
	var out []int
	for _, m := range re.FindAllStringSubmatch(arr, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

// insertArrayRef 在对象本体的 /Key [ ... ] 数组末尾插入新的引用。
func insertArrayRef(body, key string, num int) string {
	i := strings.Index(body, "/"+key)
	if i < 0 {
		return body
	}
	rel := strings.Index(body[i:], "]")
	if rel < 0 {
		return body
	}
	pos := i + rel
	return body[:pos] + fmt.Sprintf(" %d 0 R", num) + body[pos:]
}

// upsertAnnotsRef 向页面对象的 /Annots 数组追加引用（无则插入新键）。
func upsertAnnotsRef(pageBody string, num int) string {
	if strings.Contains(pageBody, "/Annots") {
		return insertArrayRef(pageBody, "Annots", num)
	}
	// 页面字典是扁平最外层字典，在末尾 ">>" 前插入
	trimmed := strings.TrimRight(pageBody, " \n")
	if !strings.HasSuffix(trimmed, ">>") {
		return pageBody
	}
	return trimmed[:len(trimmed)-2] + fmt.Sprintf(" /Annots [%d 0 R] >>", num)
}

// pdfTextStr 生成 PDF 文本字符串（纯 ASCII 字面量，否则 UTF-16BE 十六进制）。
func pdfTextStr(s string) string {
	ascii := true
	for _, r := range s {
		if r > 0x7e {
			ascii = false
			break
		}
	}
	if ascii {
		r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
		return "(" + r.Replace(s) + ")"
	}
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, r := range s {
		fmt.Fprintf(&b, "%04X", r)
	}
	b.WriteString(">")
	return b.String()
}

// appearanceContent 生成可见签名外观流内容（PDF 操作符，Helvetica 文本）。
// 非 ASCII 字符替换为 '?'（Helvetica 无法表示；sign 包保持零依赖不嵌入 CJK 字体）。
func appearanceContent(f *Field) string {
	width := f.Rect[2] - f.Rect[0]
	height := f.Rect[3] - f.Rect[1]

	signer := f.SignerName
	if signer == "" {
		signer = f.Name
	}
	var lines []string
	if signer != "" {
		lines = append(lines, "Digitally signed by: "+asciiSafe(signer))
	}
	lines = append(lines, "Date: "+time.Now().Format("2006-01-02 15:04:05Z07:00"))
	if f.Reason != "" {
		lines = append(lines, "Reason: "+asciiSafe(f.Reason))
	}
	if f.Location != "" {
		lines = append(lines, "Location: "+asciiSafe(f.Location))
	}

	size := (height - 8) / float64(len(lines)) * 0.8
	if size > 10 {
		size = 10
	}
	if size < 6 {
		size = 6
	}
	leading := height / float64(len(lines))

	var c bytes.Buffer
	fmt.Fprintf(&c, "q 0.85 0.9 0.98 rg 0 0 %.4g %.4g re f Q\n", width, height)
	fmt.Fprintf(&c, "q 0.2 0.4 0.8 RG 1 w 0.5 0.5 %.4g %.4g re S Q\n", width-1, height-1)
	c.WriteString("BT\n")
	fmt.Fprintf(&c, "/Helv %.4g Tf %.4g TL\n", size, leading)
	fmt.Fprintf(&c, "1 %.4g Td\n", height-leading*0.72)
	for _, line := range lines {
		fmt.Fprintf(&c, "4 0 Td (%s) Tj\n", escapeASCII(line))
		fmt.Fprintf(&c, "-4 -%.4g Td\n", leading)
	}
	c.WriteString("ET\n")
	return c.String()
}

// asciiSafe 将字符串限制在 Helvetica 可绘制的 ASCII 子集。
func asciiSafe(s string) string {
	b := []byte(s)
	out := b[:0]
	for _, c := range b {
		if c >= 0x20 && c <= 0x7e {
			out = append(out, c)
		} else {
			out = append(out, '?')
		}
	}
	return string(out)
}

// escapeASCII 转义字面量字符串中的定界符。
func escapeASCII(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', ')', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
