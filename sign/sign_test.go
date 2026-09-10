package sign_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// buildSignableDoc 生成带签名占位符的文档。
func buildSignableDoc(t *testing.T) []byte {
	t.Helper()
	doc := pdf.New()
	doc.Info().Title = "Signed Doc"
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "Contract content v1.0")
	doc.SetSignature(&sign.Field{Reason: "合同审批", Location: "Shanghai"})
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSignAndVerifyRSA(t *testing.T) {
	key, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := sign.GenerateSelfSigned("测试签名者", key)
	if err != nil {
		t.Fatal(err)
	}

	signed, err := sign.Sign(buildSignableDoc(t), sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}

	// 自检
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("签名应有效: %s", res.Message)
	}
	if !strings.Contains(res.Signer, "测试签名者") {
		t.Errorf("Signer = %q", res.Signer)
	}
	if res.Reason != "合同审批" {
		t.Errorf("Reason = %q", res.Reason)
	}

	// 篡改任意字节 → 必须失败（改文件尾部内容区）
	tampered := append([]byte(nil), signed...)
	tampered[len(tampered)-30] ^= 0xff
	res2, err := sign.Verify(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Valid {
		t.Error("篡改后签名必须无效")
	}
	// 篡改签名值本身 → 校验也必须失败（报错或无效均可）
	tampered2 := append([]byte(nil), signed...)
	idx := bytes.Index(signed, []byte("/ByteRange"))
	if idx > 0 {
		tampered2[idx+60] = 'F' // /Contents 十六进制区内
		res3, err3 := sign.Verify(tampered2)
		if err3 == nil && res3.Valid {
			t.Error("篡改签名值后校验必须失败")
		}
	}

	// pdfsig 外部验证
	if _, err := exec.LookPath("pdfsig"); err == nil {
		path := filepath.Join(t.TempDir(), "signed.pdf")
		if err := os.WriteFile(path, signed, 0644); err != nil {
			t.Fatal(err)
		}
		out, _ := exec.Command("pdfsig", path).CombinedOutput()
		if !strings.Contains(string(out), "Signature is Valid") {
			t.Errorf("pdfsig 验证失败:\n%s", out)
		}
	}
}

func TestSignAndVerifyECDSA(t *testing.T) {
	key, err := sign.GenerateECDSAKey()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := sign.GenerateSelfSigned("ECDSA Signer", key)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := sign.Sign(buildSignableDoc(t), sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("ECDSA 签名应有效: %s", res.Message)
	}
}

// TestSignOpenSSL 用 openssl 独立验证 CMS 结构与摘要。
func TestSignOpenSSL(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl 不可用")
	}
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("OpenSSL Check", key)
	signed, err := sign.Sign(buildSignableDoc(t), sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "s.pdf")
	os.WriteFile(pdfPath, signed, 0644)

	// 用 python 提取 ByteRange 内容与 /Contents 签名值
	script := `
import re, sys
d = open(sys.argv[1], 'rb').read()
m = re.search(rb'/ByteRange \[(\d+) (\d+) (\d+) (\d+)\]', d)
a, b, c, l = int(m[1]), int(m[2]), int(m[3]), int(m[4])
open(sys.argv[2], 'wb').write(d[a:a+b] + d[c:c+l])
m2 = re.search(rb'/Contents <([0-9A-Fa-f]+)>', d[m.end():])
raw = bytes.fromhex(m2[1].decode())
# 按 DER 首 TLV 长度截取（尾部是占位符补零，不能 rstrip '0'）
i, ln = 2, raw[1]
if ln & 0x80:
    n = ln & 0x7f
    ln = int.from_bytes(raw[2:2+n], 'big')
    i = 2 + n
open(sys.argv[3], 'wb').write(raw[:i+ln])
`
	sigDer := filepath.Join(dir, "sig.der")
	content := filepath.Join(dir, "content.bin")
	if out, err := exec.Command("python3", "-c", script, pdfPath, content, sigDer).CombinedOutput(); err != nil {
		t.Fatalf("提取失败: %v %s", err, out)
	}
	out, err := exec.Command("openssl", "cms", "-verify", "-inform", "DER",
		"-in", sigDer, "-content", content, "-noverify", "-binary", "-out", os.DevNull).CombinedOutput()
	if err != nil {
		t.Fatalf("openssl 验签失败: %v\n%s", err, out)
	}
}

// TestVerifyTrailingZeroCMS 回归：/Contents 尾部以 '0' 填充占位，
// 验签不得 TrimRight '0'——CMS 真实尾部为 0x00 字节时（约 1/256 概率）
// 会被误删导致 "DER 内容越界" 误报。应完整解码后按首 TLV 长度截取。
func TestVerifyTrailingZeroCMS(t *testing.T) {
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Trailing Zero", key)
	// 反复签署直至样本 CMS 末字节恰为 0x00（/M 时间戳逐次变化 → 签名值变化）
	found := false
	for i := 0; i < 2000 && !found; i++ {
		signed, err := sign.Sign(buildSignableDoc(t), sign.Options{Signer: key, Certificate: cert})
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`/Contents <([0-9A-Fa-f]+)>`).FindSubmatch(signed)
		if m == nil {
			t.Fatal("未找到 /Contents")
		}
		raw, err := hex.DecodeString(string(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		// 首 TLV 长度定位有效 CMS
		hdr, ln := 2, int(raw[1])
		if ln&0x80 != 0 {
			n := ln & 0x7f
			ln = 0
			for k := 0; k < n; k++ {
				ln = ln<<8 | int(raw[2+k])
			}
			hdr += n
		}
		if raw[hdr+ln-1] != 0x00 {
			continue // 不是目标样本
		}
		found = true
		res, err := sign.Verify(signed)
		if err != nil {
			t.Fatalf("CMS 尾字节为 0x00 的签名验签误报: %v", err)
		}
		if !res.Valid {
			t.Fatalf("CMS 尾字节为 0x00 的签名应有效: %s", res.Message)
		}
	}
	if !found {
		t.Skip("2000 轮未命中尾字节 0x00 样本（概率应极低）")
	}
}

func TestVerifyUnsigned(t *testing.T) {
	doc := pdf.New()
	doc.AddPage(page.A4)
	data, _ := doc.Bytes()
	if _, err := sign.Verify(data); err == nil {
		t.Error("未签名文档应报错")
	}
}
