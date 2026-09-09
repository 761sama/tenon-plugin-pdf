package sign_test

import (
	"crypto"
	"crypto/x509"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// buildChain 构造 根CA → 中间CA → 签名者 三级链。
func buildChain(t *testing.T) (signerKey crypto.Signer,
	root, inter, signer *x509.Certificate) {
	t.Helper()
	rk, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	root, err = sign.GenerateSelfSigned("测试根CA", rk)
	if err != nil {
		t.Fatal(err)
	}
	ik, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	inter, err = sign.GenerateSignedCertificate("测试中间CA", ik, root, rk, true)
	if err != nil {
		t.Fatal(err)
	}
	sk, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err = sign.GenerateSignedCertificate("链式签名者", sk, inter, ik, false)
	if err != nil {
		t.Fatal(err)
	}
	return sk, root, inter, signer
}

// TestChainVerify 证书信任链验证：完整链 → 通过；根不受信 → 失败；链断裂 → 失败。
func TestChainVerify(t *testing.T) {
	signerKey, root, inter, signer := buildChain(t)

	signOpts := sign.Options{
		Signer:      signerKey,
		Certificate: signer,
		Chain:       []*x509.Certificate{inter}, // 嵌入中间证书
	}
	signed, err := sign.Sign(buildSignableDoc(t), signOpts)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(root)

	// 1. 完整链 + 受信根 → 签名有效且链有效
	res, err := sign.VerifyWithOptions(signed, &sign.VerifyOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("签名应有效: %s", res.Message)
	}
	if !res.ChainChecked || !res.ChainValid {
		t.Fatalf("证书链应有效: %s", res.ChainMessage)
	}
	t.Logf("链验证: %s", res.ChainMessage)

	// 2. 旧 API Verify 不受影响（不验链）
	res2, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Valid || res2.ChainChecked {
		t.Error("Verify 应保持原语义：验签名不验链")
	}

	// 3. 根不在信任池 → 链失败（签名本身仍有效）
	otherRoots := x509.NewCertPool()
	otherKey, _ := sign.GenerateRSAKey()
	otherRoot, _ := sign.GenerateSelfSigned("不受信的根", otherKey)
	otherRoots.AddCert(otherRoot)
	res3, err := sign.VerifyWithOptions(signed, &sign.VerifyOptions{Roots: otherRoots})
	if err != nil {
		t.Fatal(err)
	}
	if !res3.Valid {
		t.Error("签名本身应仍有效")
	}
	if res3.ChainValid {
		t.Error("根不受信时链验证必须失败")
	}
	t.Logf("不受信场景: %s", res3.ChainMessage)

	// 4. 链断裂：不嵌入中间证书，也不提供 Intermediates → 失败
	signed2, err := sign.Sign(buildSignableDoc(t), sign.Options{
		Signer: signerKey, Certificate: signer, // 无 Chain
	})
	if err != nil {
		t.Fatal(err)
	}
	res4, err := sign.VerifyWithOptions(signed2, &sign.VerifyOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	if res4.ChainValid {
		t.Error("缺少中间证书时链验证必须失败")
	}

	// 5. 通过 Intermediates 补全断裂的链 → 恢复有效
	inters := x509.NewCertPool()
	inters.AddCert(inter)
	res5, err := sign.VerifyWithOptions(signed2, &sign.VerifyOptions{Roots: roots, Intermediates: inters})
	if err != nil {
		t.Fatal(err)
	}
	if !res5.ChainValid {
		t.Errorf("提供中间证书后链应有效: %s", res5.ChainMessage)
	}

	// 6. openssl 用 CAfile 做证书链的外部交叉验证
	//（-purpose any：openssl 默认 smimesign 用途不接受 anyEKU 证书，与 PDF 场景无关）
	if _, err := exec.LookPath("openssl"); err == nil {
		dir := t.TempDir()
		pdfPath := filepath.Join(dir, "s.pdf")
		os.WriteFile(pdfPath, signed, 0644)
		os.WriteFile(filepath.Join(dir, "root.pem"), sign.MarshalCertPEM(root), 0644)

		script := `
import re, sys
d = open(sys.argv[1], 'rb').read()
m = re.search(rb'/ByteRange \[(\d+) (\d+) (\d+) (\d+)\]', d)
a, b, c, l = int(m[1]), int(m[2]), int(m[3]), int(m[4])
open(sys.argv[2], 'wb').write(d[a:a+b] + d[c:c+l])
m2 = re.search(rb'/Contents <([0-9A-Fa-f]+)>', d[m.end():])
sig = m2[1].rstrip(b'0')
if len(sig) % 2: sig += b'0'
open(sys.argv[3], 'wb').write(bytes.fromhex(sig.decode()))
`
		content := filepath.Join(dir, "content.bin")
		sigDer := filepath.Join(dir, "sig.der")
		if out, err := exec.Command("python3", "-c", script, pdfPath, content, sigDer).CombinedOutput(); err != nil {
			t.Fatalf("提取失败: %v %s", err, out)
		}
		// 带 CAfile 全链验证（不再 -noverify）：CMS 内嵌的中间证书参与构链
		out, err := exec.Command("openssl", "cms", "-verify", "-inform", "DER",
			"-in", sigDer, "-content", content, "-binary", "-purpose", "any",
			"-CAfile", filepath.Join(dir, "root.pem"), "-out", os.DevNull).CombinedOutput()
		if err != nil {
			t.Fatalf("openssl 链式验签失败: %v\n%s", err, out)
		}
		t.Log("openssl cms -verify -CAfile root.pem 链式验证通过")
	}
}
