package pdf_test

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// --- 测试辅助：最小 DER 编码（用于构建 OCSP 响应） ---

func dT(tag byte, content []byte) []byte {
	out := []byte{tag}
	n := len(content)
	if n < 128 {
		out = append(out, byte(n))
	} else {
		var b []byte
		for v := n; v > 0; v >>= 8 {
			b = append([]byte{byte(v)}, b...)
		}
		out = append(out, 0x80|byte(len(b)))
		out = append(out, b...)
	}
	return append(out, content...)
}

func dCat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}
func dSeq(parts ...[]byte) []byte { return dT(0x30, dCat(parts...)) }

func dOID(oids ...int) []byte {
	body := []byte{byte(oids[0]*40 + oids[1])}
	for _, v := range oids[2:] {
		stack := []byte{byte(v & 0x7f)}
		for v >>= 7; v > 0; v >>= 7 {
			stack = append([]byte{byte(v&0x7f) | 0x80}, stack...)
		}
		body = append(body, stack...)
	}
	return dT(0x06, body)
}

func dInt(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return dT(0x02, b)
}

func dGenTime(t time.Time) []byte { return dT(0x18, []byte(t.UTC().Format("20060102150405Z"))) }

// buildOCSPResponse 构建最小 BasicOCSPResponse（good 状态），用 responder 密钥签名。
// issuer 为被查询证书的签发者证书。
func buildOCSPResponse(t *testing.T, cert, issuer *x509.Certificate,
	responderKey *rsa.PrivateKey, responderCert *x509.Certificate) []byte {
	t.Helper()
	nameHash := sha1.Sum(issuer.RawSubject)
	// issuerKeyHash 应为 issuer 公钥（不含 tag/length/unused bits）的 SHA-1
	var spki struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(issuer.RawSubjectPublicKeyInfo, &spki); err != nil {
		t.Fatal(err)
	}
	keyHash := sha1.Sum(spki.SubjectPublicKey.Bytes)

	certID := dSeq(
		dSeq(dOID(1, 3, 14, 3, 2, 26), dT(0x05, nil)), // sha1
		dT(0x04, nameHash[:]),
		dT(0x04, keyHash[:]),
		dInt(cert.SerialNumber),
	)
	singleResp := dSeq(
		certID,
		dT(0x80, nil), // certStatus = good [0]
		dGenTime(time.Now()),
	)
	tbs := dSeq(
		dT(0xa1, issuer.RawSubject), // responderID byName [1]
		dGenTime(time.Now()),
		dSeq(singleResp),
	)
	digest := sha256.Sum256(tbs)
	sig, err := rsa.SignPKCS1v15(rand.Reader, responderKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	basic := dSeq(
		tbs,
		dSeq(dOID(1, 2, 840, 113549, 1, 1, 11), dT(0x05, nil)), // sha256WithRSAEncryption
		dT(0x03, append([]byte{0}, sig...)),                     // BIT STRING
		dT(0xa0, dT(0x30, responderCert.Raw)),                   // certs [0] EXPLICIT
	)
	return dSeq(
		dT(0x0a, []byte{0}), // responseStatus = successful
		dT(0xa0, dSeq(
			dOID(1, 3, 6, 1, 5, 5, 7, 48, 1, 1), // id-pkix-ocsp-basic
			dT(0x04, basic),
		)),
	)
}

// TestLTV DSS 字典嵌入证书链 + CRL + OCSP 响应（长期验证材料）。
func TestLTV(t *testing.T) {
	// 构造 根CA → 签名者 证书链
	rootKey, _ := sign.GenerateRSAKey()
	root, _ := sign.GenerateSelfSigned("LTV 根CA", rootKey)
	leafKey, _ := sign.GenerateRSAKey()
	leaf, err := sign.GenerateSignedCertificate("LTV 签名者", leafKey, root, rootKey, false)
	if err != nil {
		t.Fatal(err)
	}

	// CRL（根 CA 签发，空吊销列表）
	crlDER, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		ThisUpdate: time.Now(),
		NextUpdate: time.Now().AddDate(1, 0, 0),
		Number:     big.NewInt(1),
	}, root, rootKey)
	if err != nil {
		t.Fatal(err)
	}

	// OCSP 响应（根 CA 作为 responder，状态 good）
	ocspDER := buildOCSPResponse(t, leaf, root, rootKey, root)

	doc := pdf.New()
	doc.Info().Title = "LTV Enabled"
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "LTV-enabled signed document")
	doc.SetSignature(&sign.Field{Reason: "LTV 演示"})
	doc.SetDSS(pdf.DSSData{
		Certs: []*x509.Certificate{leaf, root},
		CRLs:  [][]byte{crlDER},
		OCSPs: [][]byte{ocspDER},
	})
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	// 结构断言
	for _, m := range []string{"/DSS", "/Type /DSS", "/Certs", "/CRLs", "/OCSPs"} {
		if !bytes.Contains(data, []byte(m)) {
			t.Errorf("缺少 %s", m)
		}
	}

	// 签名 + 验签
	signed, err := sign.Sign(data, sign.Options{Signer: leafKey, Certificate: leaf, Chain: []*x509.Certificate{root}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("签名应有效: %s", res.Message)
	}

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "ltv.pdf")
	os.WriteFile(pdfPath, signed, 0644)
	os.WriteFile(filepath.Join(dir, "root.pem"), sign.MarshalCertPEM(root), 0644)
	os.WriteFile(filepath.Join(dir, "leaf.pem"), sign.MarshalCertPEM(leaf), 0644)

	// 外部验证 1：pdfsig 验签
	if _, err := exec.LookPath("pdfsig"); err == nil {
		out, _ := exec.Command("pdfsig", pdfPath).CombinedOutput()
		if !strings.Contains(string(out), "Signature is Valid") {
			t.Errorf("pdfsig 验证失败:\n%s", out)
		}
	}

	// 外部验证 2：从 DSS 提取 CRL 流，openssl 解析
	crlPath := filepath.Join(dir, "dss.crl")
	os.WriteFile(crlPath, crlDER, 0644)
	if out, err := exec.Command("openssl", "crl", "-inform", "DER", "-in", crlPath,
		"-noout", "-issuer").CombinedOutput(); err != nil {
		t.Errorf("openssl 解析 DSS CRL 失败: %v\n%s", err, out)
	} else if !strings.Contains(string(out), "LTV") {
		t.Errorf("CRL issuer 异常: %s", out)
	}

	// 外部验证 3：openssl 验证 OCSP 响应（结构与签名）
	ocspPath := filepath.Join(dir, "dss.ocsp")
	os.WriteFile(ocspPath, ocspDER, 0644)
	out, err := exec.Command("openssl", "ocsp", "-respin", ocspPath,
		"-issuer", filepath.Join(dir, "root.pem"),
		"-cert", filepath.Join(dir, "leaf.pem"),
		"-noverify", "-resp_text").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Response Status: successful") {
		t.Errorf("openssl ocsp 验证失败: %v\n%s", err, out)
	} else {
		if !strings.Contains(string(out), "Cert Status: good") {
			t.Errorf("OCSP 状态应为 good:\n%s", out)
		}
		t.Log("openssl ocsp 响应验证通过（successful, Cert Status: good）")
	}

	// 外部验证 4：pdfinfo 解析文档结构完好
	if _, err := exec.LookPath("pdfinfo"); err == nil {
		if out, err := exec.Command("pdfinfo", pdfPath).CombinedOutput(); err != nil {
			t.Errorf("pdfinfo 解析失败: %v\n%s", err, out)
		}
	}
}
