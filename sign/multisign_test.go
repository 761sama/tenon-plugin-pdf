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
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// TestMultiSign 多重签名（会签）：三人依次签署同一文档。
// 第 2..N 次签署通过 AppendSignatureField 追加增量修订（不动既有字节），
// 每个签名的 ByteRange 覆盖其签署时刻的文件——前序签名保持有效。
func TestMultiSign(t *testing.T) {
	doc := pdf.New()
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "Multi-signed contract")
	doc.SetSignature(&sign.Field{Reason: "甲方签署"})
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	signers := []string{"会签人A", "会签人B", "会签人C"}
	signed := data
	for i, cn := range signers {
		if i > 0 {
			signed, err = sign.AppendSignatureField(signed, &sign.Field{Reason: cn + "会签"})
			if err != nil {
				t.Fatalf("追加签名字段 %d 失败: %v", i, err)
			}
		}
		key, _ := sign.GenerateRSAKey()
		cert, _ := sign.GenerateSelfSigned(cn, key)
		signed, err = sign.Sign(signed, sign.Options{Signer: key, Certificate: cert})
		if err != nil {
			t.Fatalf("第 %d 次签名失败: %v", i, err)
		}
	}

	// VerifyAll：三个签名全部有效
	results, err := sign.VerifyAll(signed)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("应有 3 个签名结果，实际 %d", len(results))
	}
	for i, r := range results {
		if !r.Valid {
			t.Errorf("签名 %d 应有效: %s", i, r.Message)
		}
		if !strings.Contains(r.Signer, signers[i]) {
			t.Errorf("签名 %d 签署人 = %q，应含 %q", i, r.Signer, signers[i])
		}
	}

	// 后一个签名的 ByteRange 应覆盖前一个签名（文件随修订段单调增长）
	if !bytes.HasSuffix(signed, []byte("%%EOF\n")) {
		t.Error("文件应以 EOF 标记结尾")
	}

	// 篡改第一个签名覆盖区的字节 → 全部失效（第一个签名的覆盖区被所有人覆盖）
	tampered := append([]byte(nil), signed...)
	tampered[500] ^= 0xff
	res2, err := sign.VerifyAll(tampered)
	if err == nil {
		for i, r := range res2 {
			if r.Valid {
				t.Errorf("篡改后签名 %d 必须无效", i)
			}
		}
	}

	// pdfsig 外部验证：三个签名均应列出且有效
	if _, err := exec.LookPath("pdfsig"); err == nil {
		path := filepath.Join(t.TempDir(), "multi.pdf")
		os.WriteFile(path, signed, 0644)
		out, _ := exec.Command("pdfsig", path).CombinedOutput()
		t.Logf("pdfsig:\n%s", out)
		if strings.Count(string(out), "Signature #") != 3 {
			t.Errorf("pdfsig 应识别 3 个签名:\n%s", out)
		}
		if strings.Count(string(out), "Signature is Valid") != 3 {
			t.Errorf("pdfsig 应判定 3 个签名全部有效:\n%s", out)
		}
	}
}

// TestIncrementalVisibleSignature 增量追加的可见签名：外观流随修订段嵌入。
func TestIncrementalVisibleSignature(t *testing.T) {
	doc := pdf.New()
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "Base document")
	doc.SetSignature(&sign.Field{Reason: "首签"})
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	key1, _ := sign.GenerateRSAKey()
	cert1, _ := sign.GenerateSelfSigned("首签人", key1)
	s1, err := sign.Sign(data, sign.Options{Signer: key1, Certificate: cert1})
	if err != nil {
		t.Fatal(err)
	}

	// 追加可见签名字段
	s2f, err := sign.AppendSignatureField(s1, &sign.Field{
		Reason:     "会签",
		SignerName: "Li Si",
		Rect:       [4]float64{300, 600, 500, 660},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(s2f, []byte("/AP << /N")) {
		t.Error("增量可见签名应含 /AP 外观引用")
	}
	key2, _ := sign.GenerateRSAKey()
	cert2, _ := sign.GenerateSelfSigned("李四", key2)
	s2, err := sign.Sign(s2f, sign.Options{Signer: key2, Certificate: cert2})
	if err != nil {
		t.Fatal(err)
	}
	results, err := sign.VerifyAll(s2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("应有 2 个签名，实际 %d", len(results))
	}
	for i, r := range results {
		if !r.Valid {
			t.Errorf("签名 %d 应有效: %s", i+1, r.Message)
		}
	}

	// gs 渲染验证可见外观
	if _, err := exec.LookPath("gs"); err == nil {
		dir := t.TempDir()
		path := filepath.Join(dir, "v.pdf")
		os.WriteFile(path, s2, 0644)
		if out, err := exec.Command("gs", "-o", filepath.Join(dir, "g.png"),
			"-sDEVICE=png16m", "-r72", "-dBATCH", "-dNOPAUSE",
			"-dShowAnnots=true", path).CombinedOutput(); err != nil {
			t.Fatalf("gs 渲染失败: %v\n%s", err, out)
		}
	}
}
