package sign

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

// RFC 3161 时间戳（Time-Stamp Protocol）支持：
// 客户端向 TSA 请求时间戳令牌（TimeStampToken），并作为未认证属性
// signature-time-stamp（1.2.840.113549.1.9.16.2.14）嵌入 SignerInfo。
// 按 PAdES/ISO 32000-2 约定，messageImprint 为签名值（signature 字段字节）的 SHA-256。

var (
	oidTSTInfo                = []int{1, 2, 840, 113549, 1, 9, 16, 1, 4}  // id-ct-TSTInfo
	oidAttrSignatureTimeStamp = []int{1, 2, 840, 113549, 1, 9, 16, 2, 14} // id-aa-signatureTimeStampToken
	oidSigningCertV2          = []int{1, 2, 840, 113549, 1, 9, 16, 2, 47} // id-aa-signingCertificateV2
	oidTSAPolicyDemo          = []int{1, 3, 6, 1, 4, 1, 99999, 1}         // 演示 TSA 策略 OID
)

// TSAOptions RFC 3161 时间戳服务器选项。
type TSAOptions struct {
	URL     string        // TSA 服务地址（HTTP POST，application/timestamp-query）
	Timeout time.Duration // 请求超时，默认 30 秒
}

// FetchTimestamp 向 TSA 请求 data 的时间戳令牌，返回 TimeStampToken（ContentInfo DER）。
func FetchTimestamp(data []byte, opts TSAOptions) ([]byte, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("sign: TSA URL 为空")
	}
	digest := sha256.Sum256(data)
	req := derSeq(
		derInt(big.NewInt(1)),
		derSeq( // messageImprint
			derSeq(derOID(oidSHA256...), derNull),
			derOctetString(digest[:]),
		),
		derT(0x01, []byte{0xff}), // certReq TRUE
	)

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(opts.URL, "application/timestamp-query", bytes.NewReader(req))
	if err != nil {
		return nil, fmt.Errorf("sign: TSA 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sign: TSA 返回 HTTP %d", resp.StatusCode)
	}

	// TimeStampResp ::= SEQUENCE { status PKIStatusInfo, timeStampToken ContentInfo OPTIONAL }
	// （RFC 3161 §2.4.2：token 无上下文标签，直接是 ContentInfo SEQUENCE）
	top, _, err := readTLV(body)
	if err != nil || top.tag != 0x30 {
		return nil, fmt.Errorf("sign: TimeStampResp 结构错误")
	}
	parts, err := parseSeq(top.content)
	if err != nil || len(parts) < 1 {
		return nil, fmt.Errorf("sign: TimeStampResp 字段不足")
	}
	statusParts, err := parseSeq(parts[0].content)
	if err != nil || len(statusParts) < 1 {
		return nil, fmt.Errorf("sign: PKIStatusInfo 结构错误")
	}
	status := asnInt(statusParts[0])
	if status != 0 && status != 1 { // granted / grantedWithMods
		return nil, fmt.Errorf("sign: TSA 拒绝请求（PKIStatus %d）", status)
	}
	if len(parts) < 2 || parts[1].tag != 0x30 {
		return nil, fmt.Errorf("sign: 响应缺少 TimeStampToken")
	}
	return parts[1].raw, nil
}

// MockTSAHandler 返回一个最小 RFC 3161 时间戳服务的 http.Handler
// （测试/演示用途；生产环境请对接真实 TSA，如 freetsa.org）。
// 签发策略 OID 固定为 1.3.6.1.4.1.99999.1（示例域）。
func MockTSAHandler(key crypto.Signer, cert *x509.Certificate) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail := func(code int) { w.WriteHeader(code) }
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			fail(400)
			return
		}
		// TimeStampReq → messageImprint.hashedMessage
		top, _, err := readTLV(body)
		if err != nil || top.tag != 0x30 {
			fail(400)
			return
		}
		parts, err := parseSeq(top.content)
		if err != nil || len(parts) < 2 {
			fail(400)
			return
		}
		mi, err := parseSeq(parts[1].content)
		if err != nil || len(mi) != 2 || mi[1].tag != 0x04 {
			fail(400)
			return
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
		if err != nil {
			fail(500)
			return
		}
		tstInfo := buildTSTInfo(oidTSAPolicyDemo, mi[1].content, serial, time.Now())
		token, err := buildTimestampToken(tstInfo, key, cert, time.Now())
		if err != nil {
			fail(500)
			return
		}
		// TimeStampResp ::= SEQ { PKIStatusInfo(0 granted), TimeStampToken }
		resp := derSeq(derSeq(derInt(big.NewInt(0))), token)
		w.Header().Set("Content-Type", "application/timestamp-reply")
		w.Write(resp)
	})
}

// asnInt 解析 INTEGER TLV 为 int。
func asnInt(t tlv) int {
	n := 0
	for _, b := range t.content {
		n = n<<8 | int(b)
	}
	return n
}

// BuildTimestampResponse 是 TSA 服务端构件：解析 TimeStampReq，
// 用给定 TSA 凭据签发 TimeStampResp（status=granted + TimeStampToken）。
// policyOID 为 TSA 的时间戳策略 OID。供搭建 RFC 3161 服务或测试使用。
func BuildTimestampResponse(reqDER []byte, key crypto.Signer, cert *x509.Certificate, policyOID []int) ([]byte, error) {
	top, _, err := readTLV(reqDER)
	if err != nil || top.tag != 0x30 {
		return nil, fmt.Errorf("sign: TimeStampReq 结构错误")
	}
	parts, err := parseSeq(top.content)
	if err != nil || len(parts) < 2 {
		return nil, fmt.Errorf("sign: TimeStampReq 字段不足")
	}
	// messageImprint ::= SEQUENCE { hashAlgorithm, hashedMessage OCTET STRING }
	miParts, err := parseSeq(parts[1].content)
	if err != nil || len(miParts) != 2 || miParts[1].tag != 0x04 {
		return nil, fmt.Errorf("sign: messageImprint 结构错误")
	}
	imprint := miParts[1].content

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 96))
	if err != nil {
		return nil, err
	}
	tstInfo := buildTSTInfo(policyOID, imprint, serial, time.Now())
	token, err := buildTimestampToken(tstInfo, key, cert, time.Now())
	if err != nil {
		return nil, err
	}
	// TimeStampResp ::= SEQUENCE { PKIStatusInfo{status granted(0)}, TimeStampToken }
	// （RFC 3161 §2.4.2：token 无上下文标签，直接是 ContentInfo SEQUENCE）
	return derSeq(
		derSeq(derInt(big.NewInt(0))),
		token,
	), nil
}

// timestampAttr 构建 signature-time-stamp 未认证属性。
func timestampAttr(token []byte) []byte {
	return derSeq(derOID(oidAttrSignatureTimeStamp...), derSet(token))
}

// buildTimestampToken 构建 RFC 3161 时间戳令牌（供测试/演示 TSA 使用）。
// tstInfo 为已编码的 TSTInfo DER；key/cert 为 TSA 签名凭据。
func buildTimestampToken(tstInfo []byte, key crypto.Signer, cert *x509.Certificate, signingTime time.Time) ([]byte, error) {
	contentHash := sha256.Sum256(tstInfo)

	attrContentType := derSeq(derOID(oidAttrContentType...), derSet(derOID(oidTSTInfo...)))
	attrMessageDigest := derSeq(derOID(oidAttrMessageDigest...), derSet(derOctetString(contentHash[:])))
	attrSigningTime := derSeq(derOID(oidAttrSigningTime...), derSet(derUTCTime(signingTime)))
	// ESS signingCertificate-v2（id-aa-signingCertificateV2）：openssl ts -verify 要求
	certHash := sha256.Sum256(cert.Raw)
	// ESSCertIDv2 ::= SEQ { hashAlgorithm AlgorithmIdentifier, certHash OCTET STRING }
	essCertID := derSeq(derSeq(derOID(oidSHA256...), derNull), derOctetString(certHash[:]))
	// SigningCertificateV2 ::= SEQ { certs SEQ OF ESSCertIDv2 }，共三层 SEQUENCE
	attrSigningCert := derSeq(derOID(oidSigningCertV2...), derSet(derSeq(derSeq(essCertID))))
	// DER SET OF 按编码排序：9.3 < 9.4 < 9.5 < 9.16.2.47
	attrsContent := derConcat(attrContentType, attrMessageDigest, attrSigningTime, attrSigningCert)
	signedAttrs := derSet(attrContentType, attrMessageDigest, attrSigningTime, attrSigningCert)
	attrsHash := sha256.Sum256(signedAttrs)

	var sigAlg, sigValue []byte
	switch k := key.(type) {
	case *rsa.PrivateKey:
		sigAlg = derSeq(derOID(oidRSAEncryption...), derNull)
		s, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, attrsHash[:])
		if err != nil {
			return nil, err
		}
		sigValue = s
	case *ecdsa.PrivateKey:
		sigAlg = derSeq(derOID(oidECDSASHA256...))
		r, s, err := ecdsa.Sign(rand.Reader, k, attrsHash[:])
		if err != nil {
			return nil, err
		}
		der, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
		if err != nil {
			return nil, err
		}
		sigValue = der
	default:
		return nil, fmt.Errorf("sign: 不支持的密钥类型 %T", key)
	}

	signerInfo := derSeq(
		derInt(big.NewInt(1)),
		derSeq(cert.RawIssuer, derInt(cert.SerialNumber)),
		derSeq(derOID(oidSHA256...), derNull),
		derImplicit0(attrsContent),
		sigAlg,
		derOctetString(sigValue),
	)

	signedData := derSeq(
		derInt(big.NewInt(3)), // eContentType != data → version 3
		derSet(derSeq(derOID(oidSHA256...), derNull)),
		derSeq( // EncapsulatedContentInfo
			derOID(oidTSTInfo...),
			derExplicit0(derOctetString(tstInfo)),
		),
		derImplicit0(cert.Raw),
		derSet(signerInfo),
	)
	return derSeq(
		derOID(oidSignedData...),
		derExplicit0(signedData),
	), nil
}

// buildTSTInfo 编码 TSTInfo（策略 OID 由调用方给定）。
func buildTSTInfo(policyOID []int, imprint []byte, serial *big.Int, genTime time.Time) []byte {
	return derSeq(
		derInt(big.NewInt(1)),
		derOID(policyOID...),
		derSeq(derSeq(derOID(oidSHA256...), derNull), derOctetString(imprint)),
		derInt(serial),
		derT(0x18, []byte(genTime.UTC().Format("20060102150405Z"))), // GeneralizedTime
	)
}

// parseTimestampToken 从 TimeStampToken（ContentInfo DER）解析 TSTInfo 信息。
type timestampInfo struct {
	GenTime time.Time // 时间戳时间
	Imprint []byte    // messageImprint 摘要（应等于签名值的 SHA-256）
}

func parseTimestampToken(token []byte) (*timestampInfo, error) {
	top, _, err := readTLV(token)
	if err != nil || top.tag != 0x30 {
		return nil, fmt.Errorf("sign: TimeStampToken 顶层应为 SEQUENCE")
	}
	parts, err := parseSeq(top.content)
	if err != nil || len(parts) != 2 {
		return nil, fmt.Errorf("sign: TimeStampToken ContentInfo 结构错误")
	}
	if !oidEqual(parts[0], oidSignedData...) {
		return nil, fmt.Errorf("sign: TimeStampToken 不是 signedData")
	}
	sd, _, err := readTLV(parts[1].content)
	if err != nil {
		return nil, err
	}
	sdParts, err := parseSeq(sd.content)
	if err != nil || len(sdParts) < 3 {
		return nil, fmt.Errorf("sign: TimeStampToken SignedData 字段不足")
	}
	// EncapsulatedContentInfo
	eci := sdParts[2]
	eciParts, err := parseSeq(eci.content)
	if err != nil || len(eciParts) != 2 {
		return nil, fmt.Errorf("sign: EncapsulatedContentInfo 结构错误")
	}
	if !oidEqual(eciParts[0], oidTSTInfo...) {
		return nil, fmt.Errorf("sign: eContentType 不是 id-ct-TSTInfo")
	}
	// eContent [0] EXPLICIT OCTET STRING(TSTInfo)
	oct, _, err := readTLV(eciParts[1].content)
	if err != nil || oct.tag != 0x04 {
		return nil, fmt.Errorf("sign: 缺少 TSTInfo 内容")
	}
	// TSTInfo ::= SEQUENCE { version, policy, messageImprint, serialNumber, genTime, ... }
	ti, _, err := readTLV(oct.content)
	if err != nil || ti.tag != 0x30 {
		return nil, fmt.Errorf("sign: TSTInfo 结构错误")
	}
	tiParts, err := parseSeq(ti.content)
	if err != nil || len(tiParts) < 5 {
		return nil, fmt.Errorf("sign: TSTInfo 字段不足")
	}
	out := &timestampInfo{}
	// messageImprint
	miParts, err := parseSeq(tiParts[2].content)
	if err == nil && len(miParts) == 2 && miParts[1].tag == 0x04 {
		out.Imprint = miParts[1].content
	}
	// genTime（GeneralizedTime）
	if tiParts[4].tag == 0x18 {
		out.GenTime, _ = time.Parse("20060102150405Z", string(tiParts[4].content))
	} else if tiParts[4].tag == 0x17 {
		out.GenTime, _ = time.Parse("060102150405Z", string(tiParts[4].content))
	}
	return out, nil
}
