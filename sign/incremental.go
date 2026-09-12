package sign

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 多重签名与第三方文档签署（增量更新，ISO 32000-1 §7.5.6 / §12.8）：
// 第一个签名字段可由 pdf.Document.SetSignature 在生成期写入；
// 也可以由 AppendSignatureField 在任意既有 PDF（含第三方工具生成、
// 无 AcroForm 的文档）末尾追加一个增量修订段（新签名字段部件 +
// 重定义的 AcroForm/Catalog/Page 对象 + 新 xref 子段 + 新 trailer），
// 再用 Sign 回填。每个签名的 ByteRange 覆盖其签署时刻的文件内容，
// 前序签名因此不被后序签名破坏——这是 Acrobat 会签的标准机制。
//
// 输入布局支持（最小字节级解析的边界，诚实声明）：
//   - 经典交叉引用表与 xref 流（PDF 1.5+）布局均可，两者可在修订链中混排
//     （含 /XRefStm 混合段、多段 /Prev 链）；
//   - 压缩在对象流（ObjStm）中的对象可寻址读取（FlateDecode 解码）；
//   - 追加的修订段一律写经典 xref 表 + trailer（规范允许混排）；
//   - 未加密文档（追加对象无法持文件密钥加密）。

// AppendSignatureField 在 doc 末尾追加一个增量修订段，内含新的签名字段
// 占位符；doc 可以是本库已签名文档（会签），也可以是任意第三方既有 PDF
// （无 AcroForm 时自动新建）。返回追加后的完整文档，
// 对其调用 Sign 即可完成该字段的签名。
func AppendSignatureField(doc []byte, f *Field) ([]byte, error) {
	if f == nil {
		f = &Field{}
	}
	// 1. 沿 xref 链建立对象索引（经典表 / xref 流 / 混合段均可）
	idx, prevXref, err := buildIndex(doc)
	if err != nil {
		return nil, err
	}
	trailer := idx.trailer
	if trailer.encrypt > 0 {
		return nil, fmt.Errorf("sign: 加密文档不支持追加签名字段（增量修订无法加密新对象）")
	}
	rootNum := trailer.root
	if rootNum == 0 {
		return nil, fmt.Errorf("sign: trailer 缺少 /Root")
	}

	// 2. Catalog → AcroForm（引用 / 内联字典 / 缺失三种形态）
	catalog, err := idx.body(doc, rootNum)
	if err != nil {
		return nil, err
	}
	acroNum := refValue(catalog, "AcroForm")
	acro := ""
	existFields := ""
	if acroNum != 0 {
		if acro, err = idx.body(doc, acroNum); err != nil {
			return nil, err
		}
		existFields = arrayBody(acro, "Fields")
	} else if strings.Contains(catalog, "/AcroForm") {
		existFields = arrayBody(catalog, "Fields")
	}

	// 3. 页树 → 目标页对象号（支持嵌套页树）
	pagesNum := refValue(catalog, "Pages")
	if pagesNum == 0 {
		return nil, fmt.Errorf("sign: Catalog 缺少 /Pages")
	}
	pageIdx := f.PageIndex
	if pageIdx < 0 {
		pageIdx = 0
	}
	pageNum, err := idx.findPage(doc, pagesNum, pageIdx)
	if err != nil {
		return nil, err
	}
	pageBody, err := idx.body(doc, pageNum)
	if err != nil {
		return nil, err
	}

	// 4. 新对象号分配：签名字段部件 + 签名值字典（/V 须为间接引用，
	// 部分验证器如 pyhanko 不接受内联 /V）+ 外观流 + 新建 AcroForm
	next := trailer.size
	fieldNum := next
	next++
	valNum := next
	next++
	apNum := 0
	if f.Visible() {
		apNum = next
		next++
	}
	newAcroNum := 0
	if acroNum == 0 && !strings.Contains(catalog, "/AcroForm") {
		newAcroNum = next
		next++
	}

	var fb bytes.Buffer
	fmt.Fprintf(&fb, "%d 0 obj\n", fieldNum)
	fb.WriteString("<< /Type /Annot /Subtype /Widget /FT /Sig")
	name := f.Name
	if name == "" {
		name = fmt.Sprintf("Signature%d", len(arrayRefs(existFields))+1)
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
	fmt.Fprintf(&fb, " /V %d 0 R", valNum)
	fb.WriteString(" >>\nendobj\n")

	// 4.1 签名值字典（独立间接对象，含定长占位符）
	var vb bytes.Buffer
	fmt.Fprintf(&vb, "%d 0 obj\n", valNum)
	vb.WriteString("<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached")
	vb.WriteString(" /ByteRange " + ByteRangePlaceholder)
	vb.WriteString(" /Contents " + ContentsPlaceholder())
	fmt.Fprintf(&vb, " /M (D:%s)", time.Now().Format("20060102150405Z07'00'"))
	if f.Reason != "" {
		vb.WriteString(" /Reason " + pdfTextStr(f.Reason))
	}
	if f.Location != "" {
		vb.WriteString(" /Location " + pdfTextStr(f.Location))
	}
	if f.Contact != "" {
		vb.WriteString(" /ContactInfo " + pdfTextStr(f.Contact))
	}
	vb.WriteString(" >>\nendobj\n")

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

	// 5. AcroForm 更新：引用形态重定义 AcroForm 对象；内联形态重定义 Catalog；
	// 缺失形态新建 AcroForm 对象并重定义 Catalog 挂接
	type objBuf struct {
		num int
		buf *bytes.Buffer
	}
	var newObjs []objBuf
	if newAcroNum > 0 {
		var nab bytes.Buffer
		fmt.Fprintf(&nab, "%d 0 obj\n<< /Fields [%d 0 R] /SigFlags 3 >>\nendobj\n", newAcroNum, fieldNum)
		newObjs = append(newObjs, objBuf{newAcroNum, &nab})
	}
	if acroNum != 0 {
		newAcro := upsertArrayRef(acro, "Fields", fieldNum)
		var ab bytes.Buffer
		fmt.Fprintf(&ab, "%d 0 obj\n%s\nendobj\n", acroNum, newAcro)
		newObjs = append(newObjs, objBuf{acroNum, &ab})
	} else {
		var cb bytes.Buffer
		if newAcroNum > 0 {
			catalog = insertDictKey(catalog, fmt.Sprintf("/AcroForm %d 0 R", newAcroNum))
		} else {
			catalog = upsertInlineArrayRef(catalog, "AcroForm", "Fields", fieldNum)
		}
		fmt.Fprintf(&cb, "%d 0 obj\n%s\nendobj\n", rootNum, catalog)
		newObjs = append(newObjs, objBuf{rootNum, &cb})
	}

	// 6. 重定义页面（Annots 追加部件引用）
	newPage := upsertAnnotsRef(pageBody, fieldNum)
	var pb2 bytes.Buffer
	fmt.Fprintf(&pb2, "%d 0 obj\n%s\nendobj\n", pageNum, newPage)
	newObjs = append(newObjs, objBuf{pageNum, &pb2})

	// 字段部件、签名值字典与外观流
	newObjs = append(newObjs, objBuf{fieldNum, &fb}, objBuf{valNum, &vb})
	if apNum > 0 {
		newObjs = append(newObjs, objBuf{apNum, &apb})
	}

	// 7. 装配修订段：对象体 + xref 子段 + trailer
	out := append([]byte(nil), doc...)
	base := len(out)
	var body bytes.Buffer
	offsets := map[int]int{}
	maxNum := 0
	var nums []int
	for _, item := range newObjs {
		offsets[item.num] = base + body.Len()
		body.Write(item.buf.Bytes())
		nums = append(nums, item.num)
		if item.num > maxNum {
			maxNum = item.num
		}
	}
	out = append(out, body.Bytes()...)

	xrefPos := len(out)
	var xref bytes.Buffer
	xref.WriteString("xref\n")
	// 子段按对象号排序合并连续区间
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

	// trailer：沿用 Root/Info/ID，/Prev 指向上一个 xref
	var tr bytes.Buffer
	tr.WriteString("trailer\n<<")
	fmt.Fprintf(&tr, " /Size %d", maxNum+1)
	fmt.Fprintf(&tr, " /Root %d 0 R", rootNum)
	if trailer.info > 0 {
		fmt.Fprintf(&tr, " /Info %d 0 R", trailer.info)
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

// SignExisting 对任意既有 PDF（本库或第三方工具生成）添加签名字段并完成签名：
// 先以 AppendSignatureField 追加增量修订段（无 AcroForm 时自动新建），
// 再用 Sign 两遍回填。既有字节一概不动，原文档内容与前序签名保持有效。
// 输入限制见 AppendSignatureField 注释。
func SignExisting(doc []byte, field *Field, opts Options) ([]byte, error) {
	withField, err := AppendSignatureField(doc, field)
	if err != nil {
		return nil, err
	}
	return Sign(withField, opts)
}

// findPage 沿页树（支持嵌套 Pages 节点）找到第 pageIdx 个叶子页面对象号。
func (d *docIndex) findPage(doc []byte, root, pageIdx int) (int, error) {
	found := 0
	pageNum := 0
	var visit func(num, depth int) error
	visit = func(num, depth int) error {
		if pageNum != 0 {
			return nil
		}
		if depth > 32 {
			return fmt.Errorf("sign: 页树嵌套过深")
		}
		body, err := d.body(doc, num)
		if err != nil {
			return err
		}
		kids := arrayRefs(arrayBody(body, "Kids"))
		if len(kids) == 0 {
			if found == pageIdx {
				pageNum = num
			}
			found++
			return nil
		}
		for _, k := range kids {
			if err := visit(k, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return 0, err
	}
	if pageNum == 0 {
		if found == 0 {
			return 0, fmt.Errorf("sign: 页树为空")
		}
		// idx 越界回退第 0 页
		return d.findPage(doc, root, 0)
	}
	return pageNum, nil
}

// insertDictKey 在扁平最外层字典末尾 ">>" 前插入一个键值对文本。
func insertDictKey(body, kv string) string {
	trimmed := strings.TrimRight(body, " \t\r\n")
	i := strings.LastIndex(trimmed, ">>")
	if i < 0 {
		return body
	}
	return trimmed[:i] + " " + kv + " " + trimmed[i:]
}

// upsertArrayRef 向对象本体的 /Key [ ... ] 数组末尾插入引用；无该键则新建数组。
func upsertArrayRef(body, key string, num int) string {
	if strings.Contains(body, "/"+key) {
		return insertArrayRef(body, key, num)
	}
	return insertDictKey(body, fmt.Sprintf("/%s [%d 0 R]", key, num))
}

// upsertInlineArrayRef 向对象本体内联的 /Parent << ... >> 字典中的
// /Key 数组追加引用（无 /Key 则在内联字典内新建）。用于 Catalog 内联 AcroForm。
func upsertInlineArrayRef(body, parent, key string, num int) string {
	pi := strings.Index(body, "/"+parent)
	if pi < 0 {
		return insertDictKey(body, fmt.Sprintf("/%s << /%s [%d 0 R] /SigFlags 3 >>", parent, key, num))
	}
	open := strings.Index(body[pi:], "<<")
	if open < 0 {
		return body
	}
	open += pi
	// 括号配对找到内联字典范围
	depth := 0
	close := -1
	for i := open; i+1 < len(body); i++ {
		if body[i] == '<' && body[i+1] == '<' {
			depth++
			i++
		} else if body[i] == '>' && body[i+1] == '>' {
			depth--
			i++
			if depth == 0 {
				close = i - 1
				break
			}
		}
	}
	if close < 0 {
		return body
	}
	inner := body[open : close+2]
	if strings.Contains(inner, "/"+key) {
		inner = insertArrayRef(inner, key, num)
	} else {
		inner = insertDictKey(inner, fmt.Sprintf("/%s [%d 0 R]", key, num))
	}
	return body[:open] + inner + body[close+2:]
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
