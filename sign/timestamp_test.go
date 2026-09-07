package sign_test

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// makeTSACert 生成带 critical id-kp-timeStamping EKU 的 TSA 证书（RFC 3161 §2.3）。
func makeTSACert(t *testing.T) (crypto.Signer, *x509.Certificate) {
	t.Helper()
	k, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	// id-kp-timeStamping (1.3.6.1.5.5.7.3.8) 作为唯一 EKU 且 critical
	ekuVal := []byte{0x30, 0x0a, 0x06, 0x08, 0x2b, 0x06, 0x01, 0x05, 0x05, 0x07, 0x03, 0x08}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "测试TSA", Organization: []string{"tenon-test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{
			{Id: []int{2, 5, 29, 37}, Critical: true, Value: ekuVal},
		},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, k.Public(), k)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return k, cert
}

// extractTSToken 从 CMS DER 中定位 signature-time-stamp 属性并取出令牌 ContentInfo。
// OID 1.2.840.113549.1.9.16.2.14 = 06 0b 2a 86 48 86 f7 0d 01 09 10 02 0e
func extractTSToken(t *testing.T, cms []byte) []byte {
	t.Helper()
	oid := []byte{0x06, 0x0b, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x09, 0x10, 0x02, 0x0e}
	idx := indexOf(cms, oid)
	if idx < 0 {
		t.Fatal("CMS 中未找到签名时间戳属性")
	}
	// 之后依次是 SET（属性值集合）与 ContentInfo SEQUENCE
	p := idx + len(oid)
	_, n := readTLVLen(t, cms[p:]) // SET 头
	p += n
	return cms[p : p+tlvTotalLen(t, cms[p:])]
}

// extractTSTImprint 从 TimeStampToken 中取出 TSTInfo 的 messageImprint（32 字节）。
// 结构：ContentInfo SEQ { OID, [0]{ SignedData SEQ { ver, SET,
// EncapsulatedContentInfo SEQ { OID, [0]{ OCTET STRING(TSTInfo) } }, ... } } }
// TSTInfo SEQ { version, policy, messageImprint SEQ { algId, OCTET STRING } }
func extractTSTImprint(t *testing.T, token []byte) []byte {
	t.Helper()
	// 逐层钻取 content
	drill := func(b []byte, skip int) []byte { // 跳过 skip 个 TLV，返回下一个 TLV 的内容
		for i := 0; i < skip; i++ {
			_, hl := readTLVLen(t, b)
			cl, _ := readTLVLen(t, b)
			b = b[hl+cl:]
		}
		cl, hl := readTLVLen(t, b)
		return b[hl : hl+cl]
	}
	content := drill(token, 0) // ContentInfo 内容
	e0 := drill(content, 1)    // [0] 内容（SignedData）
	sd := drill(e0, 0)         // SignedData 内容
	eci := drill(sd, 2)        // 跳过 version+SET → EncapsulatedContentInfo 内容
	ec0 := drill(eci, 1)       // [0] 内容（OCTET STRING）
	tstInfo := drill(ec0, 0)   // TSTInfo DER 字节
	ti := drill(tstInfo, 0)    // TSTInfo 内容
	mi := drill(ti, 2)         // 跳过 version+policy → messageImprint 内容
	oct := drill(mi, 1)        // hashedMessage OCTET STRING 内容
	return oct
}

func indexOf(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		ok := true
		for j := range sub {
			if b[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// readTLVLen 读取 TLV 头，返回内容长度与头部长度。
func readTLVLen(t *testing.T, b []byte) (contentLen, headerLen int) {
	t.Helper()
	if len(b) < 2 {
		t.Fatal("TLV 过短")
	}
	l := int(b[1])
	if l&0x80 == 0 {
		return l, 2
	}
	n := l & 0x7f
	l = 0
	for i := 0; i < n; i++ {
		l = l<<8 | int(b[2+i])
	}
	return l, 2 + n
}

func tlvTotalLen(t *testing.T, b []byte) int {
	cl, hl := readTLVLen(t, b)
	return hl + cl
}

func TestTimestamp(t *testing.T) {
	tsaKey, tsaCert := makeTSACert(t)
	srv := httptest.NewServer(sign.MockTSAHandler(tsaKey, tsaCert))
	t.Cleanup(srv.Close)

	key, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := sign.GenerateSelfSigned("带时间戳的签名者", key)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := sign.Sign(buildSignableDoc(t), sign.Options{
		Signer:      key,
		Certificate: cert,
		TSA:         &sign.TSAOptions{URL: srv.URL},
	})
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
	if res.Timestamp == nil {
		t.Fatal("应解析出 RFC 3161 时间戳")
	}
	if !res.Timestamp.ImprintValid {
		t.Error("时间戳 messageImprint 与签名值摘要不一致")
	}
	if time.Since(res.Timestamp.Time) > time.Hour {
		t.Errorf("时间戳时间异常: %v", res.Timestamp.Time)
	}
	t.Logf("TSA 时间戳: %v", res.Timestamp.Time)

	// 无 TSA 时 Timestamp 应为 nil
	signed2, _ := sign.Sign(buildSignableDoc(t), sign.Options{Signer: key, Certificate: cert})
	res2, _ := sign.Verify(signed2)
	if res2.Timestamp != nil {
		t.Error("未嵌入时间戳时 Timestamp 应为 nil")
	}

	// pdfsig 仍应判定签名有效（时间戳是未认证属性，不影响主签名）
	if _, err := exec.LookPath("pdfsig"); err == nil {
		p := filepath.Join(t.TempDir(), "ts.pdf")
		os.WriteFile(p, signed, 0644)
		out, _ := exec.Command("pdfsig", p).CombinedOutput()
		if !strings.Contains(string(out), "Signature is Valid") {
			t.Errorf("pdfsig 验证带时间戳签名失败:\n%s", out)
		}
	}

	// openssl ts -verify 外部交叉验证时间戳令牌（imprint 从令牌 TSTInfo 提取；
	// 与签名值摘要的一致性已由上面 Verify 的 ImprintValid 断言）
	if _, err := exec.LookPath("openssl"); err == nil {
		dir := t.TempDir()
		m := regexp.MustCompile(`/Contents <([0-9A-Fa-f]+)>`).FindSubmatch(signed)
		if m == nil {
			t.Fatal("未找到 /Contents")
		}
		hexStr := strings.TrimRight(string(m[1]), "0")
		if len(hexStr)%2 != 0 {
			hexStr += "0"
		}
		cmsDER, err := hex.DecodeString(hexStr)
		if err != nil {
			t.Fatal(err)
		}
		token := extractTSToken(t, cmsDER)
		imprint := extractTSTImprint(t, token)
		tokenPath := filepath.Join(dir, "token.der")
		os.WriteFile(tokenPath, token, 0644)
		os.WriteFile(filepath.Join(dir, "tsa.pem"), sign.MarshalCertPEM(tsaCert), 0644)

		out, err := exec.Command("openssl", "ts", "-verify",
			"-digest", hex.EncodeToString(imprint),
			"-token_in", "-in", tokenPath,
			"-CAfile", filepath.Join(dir, "tsa.pem")).CombinedOutput()
		if err != nil {
			t.Fatalf("openssl ts -verify 失败: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "Verification: OK") {
			t.Fatalf("openssl ts -verify 输出异常: %s", out)
		}
		t.Log("openssl ts -verify:", strings.TrimSpace(string(out)))
	}
}
