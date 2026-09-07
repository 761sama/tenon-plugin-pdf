package sign_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/security"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// TestEncryptedAndSigned 验证加密与签名组合：同一文档先加密装配再签名，
// 签名占位符（object.Raw）不被加密器处理，ByteRange 覆盖密文形态文件。
func TestEncryptedAndSigned(t *testing.T) {
	for _, level := range []security.Level{security.AES128, security.AES256} {
		doc := pdf.New()
		doc.Info().Title = "Encrypted + Signed"
		doc.SetEncryption(security.Options{
			OwnerPassword: "owner456",
			Level:         level,
		})
		p := doc.AddPage(page.A4)
		p.DrawText(font.Helvetica, 18, 72, 760, "Encrypted and signed content")
		doc.SetSignature(&sign.Field{Reason: "combo test"})
		data, err := doc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		// 加密态：明文不可见，占位符仍明文可寻
		if bytes.Contains(data, []byte("Encrypted and signed content")) {
			t.Fatalf("level %v: 内容未加密", level)
		}
		if !bytes.Contains(data, []byte(sign.ByteRangePlaceholder)) {
			t.Fatalf("level %v: ByteRange 占位符应明文存在", level)
		}

		key, err := sign.GenerateRSAKey()
		if err != nil {
			t.Fatal(err)
		}
		cert, err := sign.GenerateSelfSigned("Combo Signer", key)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := sign.Sign(data, sign.Options{Signer: key, Certificate: cert})
		if err != nil {
			t.Fatalf("level %v: 加密文档签名失败: %v", level, err)
		}

		// 签名后 ByteRange 应覆盖全文（占位符已回填）
		if bytes.Contains(signed, []byte(sign.ByteRangePlaceholder)) {
			t.Fatalf("level %v: ByteRange 占位符未回填", level)
		}

		// pdfsig 在加密文档上验签（所有者密码解锁）
		if _, err := exec.LookPath("pdfsig"); err == nil {
			dir := t.TempDir()
			path := filepath.Join(dir, "combo.pdf")
			os.WriteFile(path, signed, 0644)
			out, _ := exec.Command("pdfsig", "-opw", "owner456", path).CombinedOutput()
			if !strings.Contains(string(out), "Signature is Valid") {
				t.Errorf("level %v: pdfsig 验证加密+签名文档失败:\n%s", level, out)
			}
			// pdfinfo 应仍识别加密态
			info, _ := exec.Command("pdfinfo", "-opw", "owner456", path).CombinedOutput()
			if !strings.Contains(string(info), "Encrypted:       yes") {
				t.Errorf("level %v: pdfinfo 未识别加密态:\n%s", level, info)
			}
		}
	}
}

// TestEncryptedAndSignedTamper 篡改加密+签名文档后验签必须失败。
func TestEncryptedAndSignedTamper(t *testing.T) {
	doc := pdf.New()
	doc.SetEncryption(security.Options{OwnerPassword: "o", Level: security.AES256})
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "tamper target")
	doc.SetSignature(&sign.Field{})
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("T", key)
	signed, err := sign.Sign(data, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := exec.LookPath("pdfsig"); err != nil {
		t.Skip("pdfsig 不可用")
	}
	dir := t.TempDir()
	tampered := append([]byte(nil), signed...)
	tampered[len(tampered)-50] ^= 0xff // 篡改文件尾部（ByteRange 覆盖区内）
	path := filepath.Join(dir, "t.pdf")
	os.WriteFile(path, tampered, 0644)
	out, _ := exec.Command("pdfsig", "-opw", "o", path).CombinedOutput()
	if strings.Contains(string(out), "Signature is Valid") {
		t.Error("篡改后 pdfsig 不应判有效:\n", out)
	}
}
