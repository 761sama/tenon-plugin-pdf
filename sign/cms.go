package sign

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// CMS/PKCS#7 OID
var (
	oidSignedData        = []int{1, 2, 840, 113549, 1, 7, 2}
	oidData              = []int{1, 2, 840, 113549, 1, 7, 1}
	oidSHA256            = []int{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidRSAEncryption     = []int{1, 2, 840, 113549, 1, 1, 1}
	oidECDSASHA256       = []int{1, 2, 840, 10045, 4, 3, 2}
	oidAttrContentType   = []int{1, 2, 840, 113549, 1, 9, 3}
	oidAttrMessageDigest = []int{1, 2, 840, 113549, 1, 9, 4}
	oidAttrSigningTime   = []int{1, 2, 840, 113549, 1, 9, 5}
)

// buildCMS 构建 detached CMS SignedData（adbe.pkcs7.detached）。
// digest 为被签内容（ByteRange 覆盖字节）的 SHA-256 摘要。
func buildCMS(digest []byte, opts Options) ([]byte, error) {
	if opts.Signer == nil || opts.Certificate == nil {
		return nil, fmt.Errorf("sign: 需要证书与私钥")
	}
	cert := opts.Certificate
	signTime := opts.Time
	if signTime.IsZero() {
		signTime = time.Now()
	}

	// 已认证属性（SET OF 需按 DER 排序：9.3 < 9.4 < 9.5）
	attrContentType := derSeq(derOID(oidAttrContentType...), derSet(derOID(oidData...)))
	attrMessageDigest := derSeq(derOID(oidAttrMessageDigest...), derSet(derOctetString(digest)))
	attrSigningTime := derSeq(derOID(oidAttrSigningTime...), derSet(derUTCTime(signTime)))
	attrsContent := derConcat(attrContentType, attrMessageDigest, attrSigningTime)

	// 签名输入：已认证属性的 SET OF DER 编码
	signedAttrs := derSet(attrContentType, attrMessageDigest, attrSigningTime)
	attrsHash := sha256.Sum256(signedAttrs)

	// 按密钥类型签名
	var sigAlg []byte
	var sigValue []byte
	switch key := opts.Signer.(type) {
	case *rsa.PrivateKey:
		sigAlg = derSeq(derOID(oidRSAEncryption...), derNull)
		s, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, attrsHash[:])
		if err != nil {
			return nil, err
		}
		sigValue = s
	case *ecdsa.PrivateKey:
		sigAlg = derSeq(derOID(oidECDSASHA256...))
		r, s, err := ecdsa.Sign(rand.Reader, key, attrsHash[:])
		if err != nil {
			return nil, err
		}
		der, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
		if err != nil {
			return nil, err
		}
		sigValue = der
	default:
		return nil, fmt.Errorf("sign: 不支持的密钥类型 %T", opts.Signer)
	}

	// certificates [0] IMPLICIT：签名证书 + 可选中间证书链（DER 依次拼接）
	certsContent := append([]byte(nil), cert.Raw...)
	for _, c := range opts.Chain {
		if c != nil {
			certsContent = append(certsContent, c.Raw...)
		}
	}

	// RFC 3161 时间戳：对签名值请求 TSA 令牌，作为未认证属性 [1] 嵌入
	var unsignedAttrs []byte
	if opts.TSA != nil {
		token, err := FetchTimestamp(sigValue, *opts.TSA)
		if err != nil {
			return nil, err
		}
		unsignedAttrs = timestampAttr(token)
	}

	siParts := [][]byte{
		derInt(big.NewInt(1)),
		derSeq(cert.RawIssuer, derInt(cert.SerialNumber)),
		derSeq(derOID(oidSHA256...), derNull),
		derImplicit0(attrsContent),
		sigAlg,
		derOctetString(sigValue),
	}
	if unsignedAttrs != nil {
		siParts = append(siParts, derT(0xa1, unsignedAttrs)) // [1] IMPLICIT unsignedAttrs
	}
	signerInfo := derSeq(siParts...)

	signedData := derSeq(
		derInt(big.NewInt(1)),
		derSet(derSeq(derOID(oidSHA256...), derNull)),
		derSeq(derOID(oidData...)), // detached：无内容
		derImplicit0(certsContent), // certificates [0] IMPLICIT
		derSet(signerInfo),
	)

	return derSeq(
		derOID(oidSignedData...),
		derExplicit0(signedData),
	), nil
}

// parsedCMS 解析出的 CMS 签名数据。
type parsedCMS struct {
	cert          *x509.Certificate   // 签名者证书（certificates 首个）
	chain         []*x509.Certificate // 其余嵌入证书（中间 CA 等）
	messageDigest []byte
	attrsContent  []byte // [0] IMPLICIT 的内容（各属性 DER 拼接）
	sigValue      []byte
	isECDSA       bool
	signingTime   time.Time
	timestamp     *timestampInfo // RFC 3161 时间戳（未认证属性，可空）
}

// parseCMS 解析 detached CMS SignedData。
func parseCMS(der []byte) (*parsedCMS, error) {
	top, _, err := readTLV(der)
	if err != nil {
		return nil, err
	}
	if top.tag != 0x30 {
		return nil, fmt.Errorf("sign: CMS 顶层应为 SEQUENCE")
	}
	parts, err := parseSeq(top.content)
	if err != nil || len(parts) != 2 {
		return nil, fmt.Errorf("sign: ContentInfo 结构错误")
	}
	if !oidEqual(parts[0], oidSignedData...) {
		return nil, fmt.Errorf("sign: 不是 signedData")
	}
	// [0] EXPLICIT SignedData
	sd, _, err := readTLV(parts[1].content)
	if err != nil || sd.tag != 0x30 {
		return nil, fmt.Errorf("sign: SignedData 结构错误")
	}
	sdParts, err := parseSeq(sd.content)
	if err != nil || len(sdParts) < 4 {
		return nil, fmt.Errorf("sign: SignedData 字段不足")
	}

	out := &parsedCMS{}
	var signerInfos tlv
	for _, p := range sdParts[1:] {
		switch {
		case p.tag == 0xa0 && out.cert == nil:
			// certificates [0] IMPLICIT：内容为若干证书 DER 依次拼接
			rest := p.content
			for len(rest) > 0 {
				certDER, r, err := readTLV(rest)
				if err != nil {
					return nil, fmt.Errorf("sign: 证书解析失败")
				}
				c, err := x509.ParseCertificate(certDER.raw)
				if err != nil {
					return nil, fmt.Errorf("sign: 证书解析失败: %w", err)
				}
				if out.cert == nil {
					out.cert = c
				} else {
					out.chain = append(out.chain, c)
				}
				rest = r
			}
		case p.tag == 0x31:
			signerInfos = p
		}
	}
	if signerInfos.tag == 0 {
		return nil, fmt.Errorf("sign: 缺少 signerInfos")
	}

	si, _, err := readTLV(signerInfos.content)
	if err != nil || si.tag != 0x30 {
		return nil, fmt.Errorf("sign: SignerInfo 结构错误")
	}
	siParts, err := parseSeq(si.content)
	if err != nil || len(siParts) < 6 {
		return nil, fmt.Errorf("sign: SignerInfo 字段不足")
	}
	// version, issuerAndSerial, digestAlg, [0] signedAttrs, sigAlg, signature, [1] unsignedAttrs?
	var attrs, unsignedAttrs tlv
	var sigAlgT, sigT tlv
	for i, p := range siParts {
		switch p.tag {
		case 0xa0:
			attrs = p
		case 0xa1:
			unsignedAttrs = p
		default:
			// sigAlg(SEQ) 与 signature(OCTET STRING) 位于属性之后：从尾部排除 [1] 后取末两位
			_ = i
		}
	}
	// 从尾部定位 signature 与 sigAlg（跳过 [1] 未认证属性）
	tail := siParts
	if len(tail) > 0 && tail[len(tail)-1].tag == 0xa1 {
		tail = tail[:len(tail)-1]
	}
	sigAlgT = tail[len(tail)-2]
	sigT = tail[len(tail)-1]
	if attrs.tag == 0 {
		return nil, fmt.Errorf("sign: 缺少已认证属性")
	}
	out.attrsContent = attrs.content

	// 未认证属性中的 RFC 3161 时间戳
	if unsignedAttrs.tag == 0xa1 {
		attrList, err := parseSeq(unsignedAttrs.content)
		if err == nil {
			for _, a := range attrList {
				av, err := parseSeq(a.content)
				if err != nil || len(av) != 2 {
					continue
				}
				if oidEqual(av[0], oidAttrSignatureTimeStamp...) {
					tok, _, err := readTLV(av[1].content)
					if err != nil {
						continue
					}
					if ti, err := parseTimestampToken(tok.raw); err == nil {
						out.timestamp = ti
					}
				}
			}
		}
	}

	if sigT.tag != 0x04 {
		return nil, fmt.Errorf("sign: 签名值应为 OCTET STRING")
	}
	out.sigValue = sigT.content

	algOID, _, err := readTLV(sigAlgT.content)
	if err != nil {
		return nil, err
	}
	switch {
	case oidEqual(algOID, oidRSAEncryption...):
		out.isECDSA = false
	case oidEqual(algOID, oidECDSASHA256...):
		out.isECDSA = true
	default:
		return nil, fmt.Errorf("sign: 不支持的签名算法")
	}

	// 属性中提取 messageDigest 与 signingTime
	attrList, err := parseSeq(attrs.content)
	if err != nil {
		return nil, err
	}
	for _, a := range attrList {
		av, err := parseSeq(a.content)
		if err != nil || len(av) != 2 {
			continue
		}
		if oidEqual(av[0], oidAttrMessageDigest...) {
			v, _, err := readTLV(av[1].content)
			if err == nil && v.tag == 0x04 {
				out.messageDigest = v.content
			}
		}
		if oidEqual(av[0], oidAttrSigningTime...) {
			v, _, err := readTLV(av[1].content)
			if err == nil && v.tag == 0x17 {
				out.signingTime, _ = time.Parse("060102150405Z", string(v.content))
			}
		}
	}
	if len(out.messageDigest) == 0 {
		return nil, fmt.Errorf("sign: 缺少 messageDigest 属性")
	}
	return out, nil
}

// verifySignature 用证书公钥校验 CMS 签名。
func (c *parsedCMS) verifySignature() error {
	// 重建 SET OF 编码（签名输入）
	set := derT(0x31, c.attrsContent)
	h := sha256.Sum256(set)
	if c.isECDSA {
		pub, ok := c.cert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("sign: 证书非 ECDSA 公钥")
		}
		if !ecdsa.VerifyASN1(pub, h[:], c.sigValue) {
			return fmt.Errorf("sign: ECDSA 签名校验失败")
		}
		return nil
	}
	pub, ok := c.cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("sign: 证书非 RSA 公钥")
	}
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], c.sigValue)
}
