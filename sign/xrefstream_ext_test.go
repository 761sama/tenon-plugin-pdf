package sign_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// 外部工具交叉验证：qpdf 生成 xref 流+对象流布局 → SignExisting 签署 →
// pdfsig / pdftotext / pdfinfo / pyhanko 验证。工具缺失时自动跳过。

func lookPath(t *testing.T, name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s 不可用", name)
	}
	return p
}

// pdfsigRun 运行 pdfsig；Windows/msys2 构建在无默认 NSS 数据库时报
// "NSS_Init failed"，此时用同包 certutil 建空库并以 -nssdir 重试。
func pdfsigRun(t *testing.T, path string) string {
	out, _ := exec.Command("pdfsig", path).CombinedOutput()
	if !strings.Contains(string(out), "NSS_Init failed") {
		return string(out)
	}
	// certutil 须为 NSS 版本：优先取与 pdfsig 同目录的
	//（PATH 上可能命中 Windows 自带 certutil，参数不兼容）
	pdfsigPath, _ := exec.LookPath("pdfsig")
	certutil := filepath.Join(filepath.Dir(pdfsigPath), "certutil")
	if _, err := os.Stat(certutil + ".exe"); err == nil {
		certutil += ".exe"
	} else if _, err := os.Stat(certutil); err != nil {
		if p, err := exec.LookPath("certutil"); err == nil {
			certutil = p
		} else {
			t.Skip("pdfsig 无可用 NSS 数据库且无 certutil")
		}
	}
	dir := t.TempDir()
	if co, err := exec.Command(certutil, "-N", "-d", "sql:"+dir, "--empty-password").CombinedOutput(); err != nil {
		t.Skipf("certutil 建库失败: %v\n%s", err, co)
	}
	out, _ = exec.Command("pdfsig", "-nssdir", dir, path).CombinedOutput()
	return string(out)
}

// TestSignExistingXRefStreamExternal qpdf 转换出的 xref 流+对象流文档：
// 签署 → pdfsig 判有效且 Total document signed → pdftotext 一字不丢 →
// pdfinfo 页数不变 → 会签后两个签名均有效。
func TestSignExistingXRefStreamExternal(t *testing.T) {
	lookPath(t, "qpdf")
	lookPath(t, "pdfsig")
	lookPath(t, "pdftotext")
	lookPath(t, "pdfinfo")

	dir := t.TempDir()
	classic := filepath.Join(dir, "classic.pdf")
	xs := filepath.Join(dir, "xrefstream.pdf")
	signedPath := filepath.Join(dir, "signed.pdf")

	// 1. 经典布局第三方风格文档 → qpdf 转 xref 流 + 对象流
	orig := buildClassicPDF(thirdPartyObjs(), 1, "", "\n")
	os.WriteFile(classic, orig, 0644)
	if out, err := exec.Command("qpdf", "--object-streams=generate", classic, xs).CombinedOutput(); err != nil {
		t.Fatalf("qpdf 转换失败: %v\n%s", err, out)
	}
	xsData, err := os.ReadFile(xs)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(xsData, []byte("trailer")) {
		t.Fatal("前提：qpdf 输出应为纯 xref 流布局（无 trailer）")
	}
	if !bytes.Contains(xsData, []byte("/ObjStm")) {
		t.Fatal("前提：qpdf 输出应含对象流")
	}

	// 2. 签署（可见签名）
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("External XS", key)
	signed, err := sign.SignExisting(xsData, &sign.Field{
		Reason: "ext xs", Rect: [4]float64{350, 100, 560, 160}, SignerName: "Ext XS",
	}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(signed, xsData) {
		t.Fatal("增量修订不得改动既有字节")
	}
	os.WriteFile(signedPath, signed, 0644)

	// 3. pdfsig：有效 + 覆盖整篇
	out := pdfsigRun(t, signedPath)
	if !strings.Contains(out, "Signature is Valid") {
		t.Errorf("pdfsig 应判签名有效:\n%s", out)
	}
	if !strings.Contains(out, "Total document signed") {
		t.Errorf("pdfsig 应判 Total document signed:\n%s", out)
	}

	// 4. pdftotext：原文文本一字不丢
	origTxt, err1 := exec.Command("pdftotext", classic, "-").Output()
	signedTxt, err2 := exec.Command("pdftotext", signedPath, "-").Output()
	if err1 != nil || err2 != nil {
		t.Fatalf("pdftotext 失败: %v / %v", err1, err2)
	}
	if !strings.Contains(string(signedTxt), "Third-party contract content") {
		t.Errorf("pdftotext 提取内容丢失:\n%s", signedTxt)
	}
	// 可见签名外观文本会被额外提取，其余内容须一致
	if !strings.HasPrefix(string(signedTxt), string(origTxt)) &&
		strings.TrimSpace(string(origTxt)) != strings.TrimSpace(string(signedTxt)) {
		if !strings.Contains(string(signedTxt), strings.TrimSpace(string(origTxt))) {
			t.Errorf("pdftotext 原文内容不一致:\n原文 %q\n签署后 %q", origTxt, signedTxt)
		}
	}

	// 5. pdfinfo：页数不变
	pagesOf := func(p string) string {
		o, _ := exec.Command("pdfinfo", p).Output()
		for _, line := range strings.Split(string(o), "\n") {
			if strings.HasPrefix(line, "Pages:") {
				return strings.TrimSpace(line)
			}
		}
		return ""
	}
	if pagesOf(classic) != pagesOf(signedPath) || pagesOf(classic) == "" {
		t.Errorf("页数应不变: %q vs %q", pagesOf(classic), pagesOf(signedPath))
	}

	// 6. 会签：第二个签名同样有效，前序签名不被破坏
	signed2, err := sign.SignExisting(signed, &sign.Field{Reason: "ext xs 2"},
		sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(signedPath, signed2, 0644)
	out = pdfsigRun(t, signedPath)
	if n := strings.Count(out, "Signature is Valid"); n != 2 {
		t.Errorf("会签后应有 2 个有效签名，实际 %d:\n%s", n, out)
	}
}

// TestSignExistingXRefStreamPyhanko pyhanko 交叉验证（python + pyhanko 可用时）。
func TestSignExistingXRefStreamPyhanko(t *testing.T) {
	lookPath(t, "qpdf")
	py, err := exec.LookPath("python")
	if err != nil {
		if p, err2 := exec.LookPath("python3"); err2 == nil {
			py = p
		} else {
			t.Skip("python 不可用")
		}
	}
	script := `
import sys
from pyhanko.pdf_utils.reader import PdfFileReader
from pyhanko.sign.validation import validate_pdf_signature
from pyhanko_certvalidator.context import ValidationContext
r = PdfFileReader(open(sys.argv[1], 'rb'))
n = 0
for s in r.embedded_signatures:
    vc = ValidationContext(trust_roots=[s.signer_cert])
    st = validate_pdf_signature(s, signer_validation_context=vc)
    if not (st.intact and st.valid):
        print('FAIL: intact=%s valid=%s' % (st.intact, st.valid))
        sys.exit(1)
    n += 1
print('OK %d signature(s) intact+valid' % n)
`
	if err := exec.Command(py, "-c", "import pyhanko").Run(); err != nil {
		t.Skip("pyhanko 不可用")
	}

	dir := t.TempDir()
	classic := filepath.Join(dir, "classic.pdf")
	xs := filepath.Join(dir, "xrefstream.pdf")
	os.WriteFile(classic, buildClassicPDF(thirdPartyObjs(), 1, "", "\n"), 0644)
	if out, err := exec.Command("qpdf", "--object-streams=generate", classic, xs).CombinedOutput(); err != nil {
		t.Fatalf("qpdf 转换失败: %v\n%s", err, out)
	}
	xsData, _ := os.ReadFile(xs)

	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Pyhanko XS", key)
	signed, err := sign.SignExisting(xsData, &sign.Field{Reason: "pyhanko"}, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "signed.pdf")
	os.WriteFile(p, signed, 0644)
	out, err := exec.Command(py, "-c", script, p).CombinedOutput()
	if err != nil {
		t.Errorf("pyhanko 验证失败: %v\n%s", err, out)
	} else {
		t.Logf("pyhanko: %s", strings.TrimSpace(string(out)))
	}
}
