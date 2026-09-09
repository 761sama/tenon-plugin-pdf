package sign_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// --- 手工构造第三方风格 PDF（经典 xref 表布局，非本库产物） ---

// buildClassicPDF 按给定间接对象构造 PDF 1.4 文件（正确 xref 偏移）。
// newline 可取 "\n" 或 "\r\n" 模拟不同生成器。
func buildClassicPDF(objs map[int]string, rootNum int, extraTrailer, newline string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4" + newline + "%\xe2\xe3\xcf\xd3" + newline)
	nums := make([]int, 0, len(objs))
	maxN := 0
	for n := range objs {
		nums = append(nums, n)
		if n > maxN {
			maxN = n
		}
	}
	sort.Ints(nums)
	offsets := map[int]int{}
	for _, n := range nums {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj%s%s%sendobj%s", n, newline, objs[n], newline, newline)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref%s0 %d%s", newline, maxN+1, newline)
	buf.WriteString("0000000000 65535 f " + newline)
	for i := 1; i <= maxN; i++ {
		if off, ok := offsets[i]; ok {
			fmt.Fprintf(&buf, "%010d 00000 n %s", off, newline)
		} else {
			buf.WriteString("0000000000 65535 f " + newline)
		}
	}
	fmt.Fprintf(&buf, "trailer%s<< /Size %d /Root %d 0 R%s >>%s", newline, maxN+1, rootNum, extraTrailer, newline)
	fmt.Fprintf(&buf, "startxref%s%d%s%%%%EOF%s", newline, xref, newline, newline)
	return buf.Bytes()
}

// thirdPartyObjs 单页第三方文档（无 AcroForm，页面含一行 Helvetica 文本）。
func thirdPartyObjs() map[int]string {
	content := "BT /F1 12 Tf 72 770 Td (Third-party contract content) Tj ET"
	return map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] " +
			"/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> >>" +
			" /Contents 4 0 R >>",
		4: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
}

// TestSignExistingThirdParty 无 AcroForm 的第三方 PDF：SignExisting 一步签署。
func TestSignExistingThirdParty(t *testing.T) {
	data := buildClassicPDF(thirdPartyObjs(), 1, "", "\n")
	if bytes.Contains(data, []byte("/AcroForm")) {
		t.Fatal("前提：第三方文档不含 AcroForm")
	}
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("外部签署人", key)
	signed, err := sign.SignExisting(data, &sign.Field{Reason: "第三方签署"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	// 既有字节一概不动（增量修订）
	if !bytes.HasPrefix(signed, data) {
		t.Error("增量修订不得改动既有字节")
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("第三方文档签名应有效: %s", res.Message)
	}
	if !strings.Contains(res.Signer, "外部签署人") {
		t.Errorf("Signer = %q", res.Signer)
	}
	if res.Reason != "第三方签署" {
		t.Errorf("Reason = %q", res.Reason)
	}
	// 篡改既有内容区任意字节 → 验签失败
	tampered := append([]byte(nil), signed...)
	tampered[bytes.Index(tampered, []byte("contract"))] ^= 0xff
	res2, err := sign.Verify(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Valid {
		t.Error("篡改后签名必须无效")
	}
}

// TestSignExistingCRLF CRLF 换行的第三方文档。
func TestSignExistingCRLF(t *testing.T) {
	data := buildClassicPDF(thirdPartyObjs(), 1, "", "\r\n")
	key, _ := sign.GenerateECDSAKey()
	cert, _ := sign.GenerateSelfSigned("ECDSA Third Party", key)
	signed, err := sign.SignExisting(data, nil, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("CRLF 第三方文档签名应有效: %s", res.Message)
	}
}

// TestSignExistingNestedPageTree 嵌套页树（Pages → Pages → Page）。
func TestSignExistingNestedPageTree(t *testing.T) {
	objs := thirdPartyObjs()
	objs[2] = "<< /Type /Pages /Kids [5 0 R] /Count 2 >>"
	objs[3] = "<< /Type /Page /Parent 5 0 R /MediaBox [0 0 595 842] /Contents 4 0 R " +
		"/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> >> >>"
	objs[5] = "<< /Type /Pages /Parent 2 0 R /Kids [3 0 R 6 0 R] /Count 2 >>"
	objs[6] = "<< /Type /Page /Parent 5 0 R /MediaBox [0 0 595 842] >>"
	data := buildClassicPDF(objs, 1, "", "\n")

	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Nested", key)
	// 签署到第 2 页（PageIndex=1，嵌套页树遍历）
	signed, err := sign.SignExisting(data, &sign.Field{PageIndex: 1}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("嵌套页树文档签名应有效: %s", res.Message)
	}
	// 字段应挂到对象 6（第 2 页）的 Annots
	if !bytes.Contains(signed, []byte("/P 6 0 R")) {
		t.Error("签名字段应引用嵌套页树的第 2 页对象")
	}
}

// TestSignExistingInlineAcroForm Catalog 内联 AcroForm 字典的第三方文档。
func TestSignExistingInlineAcroForm(t *testing.T) {
	objs := thirdPartyObjs()
	objs[1] = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] /DR << /Font << >> >> >> >>"
	data := buildClassicPDF(objs, 1, "", "\n")
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Inline", key)
	signed, err := sign.SignExisting(data, nil, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("内联 AcroForm 文档签名应有效: %s", res.Message)
	}
}

// TestSignExistingVisible 第三方文档的可见签名（外观流随修订段嵌入）。
func TestSignExistingVisible(t *testing.T) {
	data := buildClassicPDF(thirdPartyObjs(), 1, "", "\n")
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Visible Signer", key)
	signed, err := sign.SignExisting(data, &sign.Field{
		Rect:       [4]float64{350, 100, 560, 160},
		SignerName: "Wang Wu",
		Reason:     "external approval",
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
		t.Fatalf("第三方可见签名应有效: %s", res.Message)
	}
}

// TestSignExistingMulti 第三方文档会签：两次 SignExisting，前序签名保持有效。
func TestSignExistingMulti(t *testing.T) {
	data := buildClassicPDF(thirdPartyObjs(), 1, "", "\n")
	signed := data
	for i, cn := range []string{"外签A", "外签B"} {
		key, _ := sign.GenerateRSAKey()
		cert, _ := sign.GenerateSelfSigned(cn, key)
		var err error
		signed, err = sign.SignExisting(signed, &sign.Field{Reason: cn + "签署"},
			sign.Options{Signer: key, Certificate: cert})
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

// TestSignExistingErrors 不支持的输入应给出明确错误。
func TestSignExistingErrors(t *testing.T) {
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("X", key)
	opts := sign.Options{Signer: key, Certificate: cert}

	// 纯 xref 流（无 trailer 字典）
	xrefStream := []byte("%PDF-1.5\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"3 0 obj\n<< /Type /XRef /Size 4 /Root 1 0 R /W [1 2 1] /Length 10 >>\nstream\n0123456789\nendstream\nendobj\n" +
		"startxref\n60\n%%EOF\n")
	if _, err := sign.SignExisting(xrefStream, nil, opts); err == nil ||
		!strings.Contains(err.Error(), "trailer") {
		t.Errorf("xref 流文档应报 trailer 相关错误, got %v", err)
	}

	// 加密文档
	enc := buildClassicPDF(map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R >>",
		2: "<< /Type /Pages /Kids [] /Count 0 >>",
		7: "<< /Filter /Standard /V 4 >>",
	}, 1, " /Encrypt 7 0 R", "\n")
	if _, err := sign.SignExisting(enc, nil, opts); err == nil ||
		!strings.Contains(err.Error(), "加密") {
		t.Errorf("加密文档应报明确错误, got %v", err)
	}
}

// TestSignExistingExternal 外部工具交叉验证（pdfsig 可用时）。
func TestSignExistingExternal(t *testing.T) {
	if _, err := exec.LookPath("pdfsig"); err != nil {
		t.Skip("pdfsig 不可用")
	}
	data := buildClassicPDF(thirdPartyObjs(), 1, "", "\n")
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("External Check", key)
	signed, err := sign.SignExisting(data, &sign.Field{Reason: "ext"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "third.pdf")
	os.WriteFile(path, signed, 0644)
	out, _ := exec.Command("pdfsig", path).CombinedOutput()
	if !strings.Contains(string(out), "Signature is Valid") {
		t.Errorf("pdfsig 验证第三方签署文档失败:\n%s", out)
	}
	// 文档内容未被破坏：pdftotext 可提取原文
	if txt, err := exec.Command("pdftotext", path, "-").Output(); err != nil ||
		!strings.Contains(string(txt), "Third-party contract content") {
		t.Errorf("pdftotext 提取失败或内容丢失: %v\n%s", err, txt)
	}
	// 篡改后 pdfsig 应判无效
	tampered := append([]byte(nil), signed...)
	tampered[bytes.Index(tampered, []byte("contract"))] ^= 0xff
	os.WriteFile(path, tampered, 0644)
	out, _ = exec.Command("pdfsig", path).CombinedOutput()
	if strings.Contains(string(out), "Signature is Valid") {
		t.Error("篡改后 pdfsig 不应判有效:\n", out)
	}
}
