package sign_test

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// --- 手工构造 xref 流 / 对象流布局 PDF（模拟 Chrome/Word/qpdf 输出） ---

type xsOpts struct {
	flateXref bool   // xref 流数据 FlateDecode 压缩
	predictor bool   // xref 流附加 /DecodeParms /Predictor 12
	flateStm  bool   // 对象流 FlateDecode 压缩
	extraDict string // xref 流字典附加文本（如 " /Encrypt 9 0 R"）
}

// buildXrefStreamPDF 构造 xref 流布局文档：plain 为普通间接对象，
// comp 为压入对象流的对象（objstmNum 为对象流的对象号）。
// 返回文档字节与 xref 流偏移（供追加第二段修订时填 /Prev）。
func buildXrefStreamPDF(plain map[int]string, objstmNum int, comp map[int]string,
	rootNum int, o xsOpts) ([]byte, int) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\xe2\xe3\xcf\xd3\n")
	offsets := map[int]int{}
	nums := make([]int, 0, len(plain))
	for n := range plain {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, plain[n])
	}

	// 对象流（ObjStm）
	compNums := make([]int, 0, len(comp))
	for n := range comp {
		compNums = append(compNums, n)
	}
	sort.Ints(compNums)
	stmIdx := map[int]int{}
	if len(compNums) > 0 {
		var hdr, body bytes.Buffer
		off := 0
		for i, n := range compNums {
			fmt.Fprintf(&hdr, "%d %d ", n, off)
			body.WriteString(comp[n])
			body.WriteByte(' ')
			off += len(comp[n]) + 1
			stmIdx[n] = i
		}
		raw := append(hdr.Bytes(), body.Bytes()...)
		data := raw
		filter := ""
		if o.flateStm {
			data = flateCompress(raw)
			filter = " /Filter /FlateDecode"
		}
		offsets[objstmNum] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /ObjStm /N %d /First %d /Length %d%s >>\nstream\n",
			objstmNum, len(compNums), hdr.Len(), len(data), filter)
		buf.Write(data)
		buf.WriteString("\nendstream\nendobj\n")
	}

	// xref 流：W [1 4 2]，对象 0 为 free
	size := 1
	for n := range offsets {
		if n+1 > size {
			size = n + 1
		}
	}
	for n := range comp {
		if n+1 > size {
			size = n + 1
		}
	}
	xrefNum := size
	size++
	var entries bytes.Buffer
	put := func(typ, f1, f2 int) {
		entries.WriteByte(byte(typ))
		for shift := 24; shift >= 0; shift -= 8 {
			entries.WriteByte(byte(f1 >> uint(shift)))
		}
		entries.WriteByte(byte(f2 >> 8))
		entries.WriteByte(byte(f2))
	}
	put(0, 0, 65535) // 对象 0 free
	for n := 1; n < xrefNum; n++ {
		if off, ok := offsets[n]; ok {
			put(1, off, 0)
		} else if _, ok := comp[n]; ok {
			put(2, objstmNum, stmIdx[n])
		} else {
			put(0, 0, 65535)
		}
	}
	put(1, buf.Len(), 0) // xref 流自身
	xrefData := entries.Bytes()
	dict := fmt.Sprintf("<< /Type /XRef /Size %d /Root %d 0 R /W [1 4 2] /Index [0 %d]",
		size, rootNum, size)
	if o.predictor {
		// PNG predictor 12：每行前加滤波器字节 0
		var rows bytes.Buffer
		for p := 0; p+7 <= len(xrefData); p += 7 {
			rows.WriteByte(0)
			rows.Write(xrefData[p : p+7])
		}
		xrefData = rows.Bytes()
		dict += fmt.Sprintf(" /DecodeParms << /Predictor 12 /Columns 7 >>")
	}
	if o.flateXref {
		xrefData = flateCompress(xrefData)
		dict += " /Filter /FlateDecode"
	}
	dict += fmt.Sprintf(" /Length %d%s >>", len(xrefData), o.extraDict)

	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "%d 0 obj\n%s\nstream\n", xrefNum, dict)
	buf.Write(xrefData)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefPos)
	return buf.Bytes(), xrefPos
}

// appendXrefStreamRevision 模拟其他工具的增量更新：追加一段 xref 流修订
// （重定义 redefine 中的对象），/Prev 指向上一段。
func appendXrefStreamRevision(doc []byte, prevXref int, redefine map[int]string, o xsOpts) ([]byte, int) {
	var buf bytes.Buffer
	buf.Write(doc)
	offsets := map[int]int{}
	nums := make([]int, 0, len(redefine))
	maxN := 0
	for n := range redefine {
		nums = append(nums, n)
		if n > maxN {
			maxN = n
		}
	}
	sort.Ints(nums)
	for _, n := range nums {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, redefine[n])
	}
	size := maxN + 2
	xrefNum := maxN + 1
	var entries bytes.Buffer
	put := func(typ, f1, f2 int) {
		entries.WriteByte(byte(typ))
		for shift := 24; shift >= 0; shift -= 8 {
			entries.WriteByte(byte(f1 >> uint(shift)))
		}
		entries.WriteByte(byte(f2 >> 8))
		entries.WriteByte(byte(f2))
	}
	for _, n := range nums {
		put(1, offsets[n], 0)
	}
	put(1, buf.Len(), 0) // xref 流自身
	dict := fmt.Sprintf("<< /Type /XRef /Size %d /Prev %d /W [1 4 2] /Index [", size, prevXref)
	for i, n := range nums {
		if i > 0 {
			dict += " "
		}
		dict += fmt.Sprintf("%d 1", n)
	}
	dict += fmt.Sprintf(" %d 1] /Length %d%s >>", xrefNum, entries.Len(), o.extraDict)
	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "%d 0 obj\n%s\nstream\n", xrefNum, dict)
	buf.Write(entries.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefPos)
	return buf.Bytes(), xrefPos
}

func flateCompress(data []byte) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write(data)
	w.Close()
	return b.Bytes()
}

// xsPlainObjs 单页文档的普通对象（Catalog/Pages/Page 可整体放入对象流）。
func xsPlainObjs() map[int]string {
	content := "BT /F1 12 Tf 72 770 Td (XRef stream contract content) Tj ET"
	return map[int]string{
		3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] " +
			"/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>" +
			" /Contents 4 0 R >>",
		4: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
}

func xsCompObjs() map[int]string {
	return map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
	}
}

// TestSignExistingXRefStream 纯 xref 流布局（FlateDecode + PNG predictor 压缩的
// xref 流，Catalog/Pages 为普通对象）。
func TestSignExistingXRefStream(t *testing.T) {
	plain := xsPlainObjs()
	plain[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	plain[2] = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	data, _ := buildXrefStreamPDF(plain, 0, nil, 1, xsOpts{flateXref: true, predictor: true})
	if bytes.Contains(data, []byte("trailer")) {
		t.Fatal("前提：xref 流布局不含 trailer 字典")
	}
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("流布局签署人", key)
	signed, err := sign.SignExisting(data, &sign.Field{Reason: "xref流签署"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(signed, data) {
		t.Error("增量修订不得改动既有字节")
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("xref 流文档签名应有效: %s", res.Message)
	}
	if res.Reason != "xref流签署" {
		t.Errorf("Reason = %q", res.Reason)
	}
}

// TestSignExistingObjStm Catalog/Pages 压缩在对象流中（页面为普通对象）。
func TestSignExistingObjStm(t *testing.T) {
	data, _ := buildXrefStreamPDF(xsPlainObjs(), 5, xsCompObjs(), 1, xsOpts{flateStm: true})
	key, _ := sign.GenerateECDSAKey()
	cert, _ := sign.GenerateSelfSigned("ObjStm Signer", key)
	signed, err := sign.SignExisting(data, &sign.Field{Reason: "objstm"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("对象流文档签名应有效: %s", res.Message)
	}
}

// TestSignExistingObjStmPage 页面对象也压缩在对象流中。
func TestSignExistingObjStmPage(t *testing.T) {
	plain := map[int]string{
		4: xsPlainObjs()[4],
	}
	comp := xsCompObjs()
	comp[3] = xsPlainObjs()[3]
	data, _ := buildXrefStreamPDF(plain, 5, comp, 1, xsOpts{flateStm: true, flateXref: true})
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Compressed Page", key)
	signed, err := sign.SignExisting(data, nil, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("页面压缩文档签名应有效: %s", res.Message)
	}
}

// TestSignExistingXRefChain 文档已被其他工具增量更新过（两段 xref 流链），
// 第二段重定义了 Catalog；签署追加第三段（经典表），链上任意混排。
func TestSignExistingXRefChain(t *testing.T) {
	plain := xsPlainObjs()
	plain[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	plain[2] = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	data, prev := buildXrefStreamPDF(plain, 0, nil, 1, xsOpts{flateXref: true})
	// 模拟其他工具的增量更新：重定义 Catalog（附加 ViewerPreferences）
	data, _ = appendXrefStreamRevision(data, prev, map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R /ViewerPreferences << /FitWindow true >> >>",
	}, xsOpts{})

	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Chain Signer", key)
	signed, err := sign.SignExisting(data, &sign.Field{Reason: "chain"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(signed, data) {
		t.Error("增量修订不得改动既有字节")
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("多段 xref 链文档签名应有效: %s", res.Message)
	}
	// 新修订应基于重定义后的 Catalog（含 ViewerPreferences）
	tail := signed[len(data):]
	if !bytes.Contains(tail, []byte("/ViewerPreferences")) {
		t.Error("新修订的 Catalog 应沿用上一修订段的重定义内容")
	}
}

// TestSignExistingXRefStreamVisible 可见签名（外观流随修订段嵌入）。
func TestSignExistingXRefStreamVisible(t *testing.T) {
	plain := xsPlainObjs()
	plain[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	plain[2] = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	data, _ := buildXrefStreamPDF(plain, 0, nil, 1, xsOpts{flateXref: true})
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Visible XS", key)
	signed, err := sign.SignExisting(data, &sign.Field{
		Rect: [4]float64{350, 100, 560, 160}, SignerName: "Liu Liu", Reason: "xs visible",
	}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(signed, []byte("/AP << /N")) {
		t.Error("可见签名应含 /AP 外观引用")
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("xref 流可见签名应有效: %s", res.Message)
	}
}

// TestSignExistingXRefStreamMulti xref 流文档会签：两次签署均有效。
func TestSignExistingXRefStreamMulti(t *testing.T) {
	plain := xsPlainObjs()
	plain[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	plain[2] = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	data, _ := buildXrefStreamPDF(plain, 0, nil, 1, xsOpts{flateXref: true, predictor: true})
	signed := data
	for i, cn := range []string{"流签A", "流签B"} {
		key, _ := sign.GenerateRSAKey()
		cert, _ := sign.GenerateSelfSigned(cn, key)
		var err error
		signed, err = sign.SignExisting(signed, &sign.Field{Reason: cn}, sign.Options{Signer: key, Certificate: cert})
		if err != nil {
			t.Fatalf("第 %d 次签署失败: %v", i, err)
		}
	}
	results, err := sign.VerifyAll(signed)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("应有 2 个签名，实际 %d", len(results))
	}
	for i, r := range results {
		if !r.Valid {
			t.Errorf("签名 %d 应有效: %s", i, r.Message)
		}
	}
}

// TestSignExistingHybridXRefStm 混合引用段（§7.5.8.1）：经典 xref 表文档，
// trailer 带 /XRefStm 指向补充 xref 流（压缩对象条目），Catalog/Pages 在对象流中。
func TestSignExistingHybridXRefStm(t *testing.T) {
	content := "BT /F1 12 Tf 72 770 Td (Hybrid xref contract) Tj ET"
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\xe2\xe3\xcf\xd3\n")
	offsets := map[int]int{}
	write := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	write(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] "+
		"/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>"+
		" /Contents 4 0 R >>")
	write(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	// 对象流 5：Catalog(1) + Pages(2)
	comp := map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
	}
	var hdr, body bytes.Buffer
	off := 0
	for _, n := range []int{1, 2} {
		fmt.Fprintf(&hdr, "%d %d ", n, off)
		body.WriteString(comp[n])
		body.WriteByte(' ')
		off += len(comp[n]) + 1
	}
	raw := append(hdr.Bytes(), body.Bytes()...)
	write(5, fmt.Sprintf("<< /Type /ObjStm /N 2 /First %d /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream",
		hdr.Len(), len(flateCompress(raw)), flateCompress(raw)))
	// xref 流 6：压缩对象 1、2（type 2 → 流 5）与自身 6
	put := func(entries *bytes.Buffer, typ, f1, f2 int) {
		entries.WriteByte(byte(typ))
		for shift := 24; shift >= 0; shift -= 8 {
			entries.WriteByte(byte(f1 >> uint(shift)))
		}
		entries.WriteByte(byte(f2 >> 8))
		entries.WriteByte(byte(f2))
	}
	var entries bytes.Buffer
	put(&entries, 2, 5, 0) // 对象 1 → 对象流 5 序号 0
	put(&entries, 2, 5, 1) // 对象 2 → 对象流 5 序号 1
	put(&entries, 1, buf.Len(), 0)
	write(6, fmt.Sprintf("<< /Type /XRef /Size 7 /W [1 4 2] /Index [1 2 6 1] /Length %d >>\nstream\n%s\nendstream",
		entries.Len(), entries.Bytes()))
	xrefStmPos := offsets[6]
	// 经典 xref 表：仅普通对象 3/4/5，trailer 带 /XRefStm
	xrefPos := buf.Len()
	buf.WriteString("xref\n0 1\n0000000000 65535 f \n")
	for _, n := range []int{3, 4, 5} {
		fmt.Fprintf(&buf, "%d 1\n%010d 00000 n \n", n, offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 7 /Root 1 0 R /XRefStm %d >>\n", xrefStmPos)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefPos)
	data := buf.Bytes()

	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Hybrid Signer", key)
	signed, err := sign.SignExisting(data, &sign.Field{Reason: "hybrid"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("混合引用段文档签名应有效: %s", res.Message)
	}
}

// TestSignExistingXRefStreamEncrypted 加密的 xref 流文档 → 明确中文错误。
func TestSignExistingXRefStreamEncrypted(t *testing.T) {
	plain := xsPlainObjs()
	plain[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	plain[2] = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	plain[9] = "<< /Filter /Standard /V 5 /R 6 /O (xx) /U (yy) /OE <00> /UE <00> /Perms <00> >>"
	data, _ := buildXrefStreamPDF(plain, 0, nil, 1, xsOpts{extraDict: " /Encrypt 9 0 R"})
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("X", key)
	_, err := sign.SignExisting(data, nil, sign.Options{Signer: key, Certificate: cert})
	if err == nil || !strings.Contains(err.Error(), "加密") {
		t.Errorf("加密 xref 流文档应报明确中文错误, got %v", err)
	}
}

// TestSignExistingXRefStreamCorrupt 损坏/截断的 xref 流文档 → 明确错误，不 panic。
func TestSignExistingXRefStreamCorrupt(t *testing.T) {
	plain := xsPlainObjs()
	plain[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	plain[2] = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	good, xrefPos := buildXrefStreamPDF(plain, 0, nil, 1, xsOpts{flateXref: true})
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("X", key)
	opts := sign.Options{Signer: key, Certificate: cert}

	cases := map[string][]byte{
		"截断在xref流数据中间": good[:xrefPos+120],
		"截断在对象体中间":     good[:len(good)/3],
		"startxref指向垃圾":    append(append([]byte(nil), good[:xrefPos]...), []byte("startxref\n99999999\n%%EOF\n")...),
		"完全不是PDF":          []byte("not a pdf at all"),
	}
	for name, bad := range cases {
		_, err := sign.SignExisting(bad, nil, opts)
		if err == nil {
			t.Errorf("%s：应报错", name)
		} else {
			t.Logf("%s → %v", name, err)
		}
	}
}
