package sign

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Options 签名选项。
type Options struct {
	Signer      crypto.Signer       // RSA 或 ECDSA 私钥
	Certificate *x509.Certificate   // 签名证书（随文档嵌入）
	Chain       []*x509.Certificate // 中间证书链（可选，随 CMS 一并嵌入，供链式验证）
	Time        time.Time           // 签名时间，默认当前
	TSA         *TSAOptions         // RFC 3161 时间戳服务器（可选，嵌入时间戳令牌）
}

// Sign 对包含签名占位符的 PDF 进行数字签名（两遍法）。
//
// doc 必须含有签名字段占位符：第一个字段由 pdf.Document.SetSignature
// 在生成期写入；多重签名（会签）的后续字段由 sign.AppendSignatureField
// 以增量修订追加——每个修订段自带占位符，签署时不改动既有字节，
// 前序签名的 ByteRange 覆盖区因此保持有效（标准增量/追加签名语义）。
func Sign(doc []byte, opts Options) ([]byte, error) {
	// 1. 定位占位符
	contentsPh := ContentsPlaceholder()
	ci := bytes.Index(doc, []byte(contentsPh))
	if ci < 0 {
		return nil, fmt.Errorf("sign: 文档中未找到签名占位符（本库文档用 SetSignature；第三方既有 PDF 请用 SignExisting 或先 AppendSignatureField）")
	}
	bi := bytes.Index(doc, []byte(ByteRangePlaceholder))
	if bi < 0 {
		return nil, fmt.Errorf("sign: 文档中未找到 ByteRange 占位符")
	}

	out := append([]byte(nil), doc...)
	a := ci                   // '<' 的位置
	b := ci + len(contentsPh) // '>' 之后

	// 2. 回填 ByteRange（定宽，不改变长度）
	br := fmt.Sprintf("[%010d %010d %010d %010d]", 0, a, b, len(out)-b)
	copy(out[bi:bi+len(ByteRangePlaceholder)], br)

	// 3. 计算 ByteRange 覆盖内容的摘要
	h := sha256.New()
	h.Write(out[:a])
	h.Write(out[b:])
	digest := h.Sum(nil)

	// 4. 构建 CMS 并回填 /Contents（不足补零）
	cms, err := buildCMS(digest, opts)
	if err != nil {
		return nil, err
	}
	hexSig := hex.EncodeToString(cms)
	if len(hexSig) > PlaceholderHexLen {
		return nil, fmt.Errorf("sign: 签名数据 %d 字节超出占位空间 %d 字节（证书链过大？）",
			len(cms), PlaceholderHexLen/2)
	}
	padded := hexSig + strings.Repeat("0", PlaceholderHexLen-len(hexSig))
	copy(out[a+1:b-1], padded)
	return out, nil
}

// VerifyResult 验签结果。
type VerifyResult struct {
	Valid       bool   // 文档完整且签名有效
	Signer      string // 签名证书主体
	SigningTime time.Time
	Reason      string
	Message     string // 失败原因

	// 以下字段仅 VerifyWithOptions 填充（Verify 不做证书信任链验证）
	ChainChecked bool   // 是否执行了证书链验证
	ChainValid   bool   // 证书链能否构建到受信根（含有效期/密钥用途校验）
	ChainMessage string // 链验证结论或失败原因

	// Timestamp 为嵌入的 RFC 3161 签名时间戳（无时间戳时为 nil）
	Timestamp *TimestampInfo
}

// TimestampInfo 签名中嵌入的 RFC 3161 时间戳验证结果。
type TimestampInfo struct {
	Time         time.Time // TSTInfo 的 genTime（TSA 背书的签名时刻）
	ImprintValid bool      // 令牌 messageImprint 与本签名值摘要一致
}

// Verify 校验 PDF 中的第一个 adbe.pkcs7.detached 签名：
// ByteRange 完整性 → 内容摘要 → CMS 签名。篡改任意字节都会使 Valid 为 false。
// 注意：Verify 不验证证书信任链（自签名证书也会 Valid=true）；
// 需要链式信任验证请用 VerifyWithOptions；多重签名请用 VerifyAll。
func Verify(doc []byte) (*VerifyResult, error) {
	res, _, _, err := verifyCore(doc, 0)
	return res, err
}

// VerifyAll 校验文档中的全部签名（多重签名/会签场景），
// 按 /ByteRange 出现顺序逐个校验，返回数量与签名字段一致。
func VerifyAll(doc []byte) ([]*VerifyResult, error) {
	return verifyAll(doc, nil)
}

// VerifyAllWithOptions 校验全部签名并追加证书信任链验证。
func VerifyAllWithOptions(doc []byte, opts *VerifyOptions) ([]*VerifyResult, error) {
	return verifyAll(doc, opts)
}

func verifyAll(doc []byte, opts *VerifyOptions) ([]*VerifyResult, error) {
	var out []*VerifyResult
	from := 0
	for {
		res, parsed, next, err := verifyCore(doc, from)
		if err != nil {
			if len(out) > 0 && from > 0 {
				break // 没有更多签名
			}
			return nil, err
		}
		if opts != nil && parsed != nil && parsed.cert != nil {
			checkChain(res, parsed, opts)
		}
		out = append(out, res)
		from = next
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sign: 无 /ByteRange，文档未签名")
	}
	return out, nil
}

// VerifyOptions 证书链验证选项。
type VerifyOptions struct {
	Roots         *x509.CertPool // 受信根证书池；nil 表示使用系统根证书池
	Intermediates *x509.CertPool // 附加中间证书（CMS 内嵌链会自动加入，无需重复提供）
	CurrentTime   time.Time      // 验证时间点（证书有效期判定），零值表示当前时间
}

// VerifyWithOptions 在 Verify 基础上追加证书信任链验证：
// 从签名证书沿链（CMS 嵌入的中间证书 + Intermediates）构建到受信根
// （Roots 或系统根证书池）。结果写入 ChainChecked/ChainValid/ChainMessage。
func VerifyWithOptions(doc []byte, opts *VerifyOptions) (*VerifyResult, error) {
	res, parsed, _, err := verifyCore(doc, 0)
	if err != nil || parsed == nil || parsed.cert == nil {
		return res, err
	}
	checkChain(res, parsed, opts)
	return res, nil
}

// checkChain 执行证书信任链验证并填充结果字段。
func checkChain(res *VerifyResult, parsed *parsedCMS, opts *VerifyOptions) {
	res.ChainChecked = true

	if opts == nil {
		opts = &VerifyOptions{}
	}
	roots := opts.Roots
	if roots == nil {
		var err error
		roots, err = x509.SystemCertPool()
		if err != nil || roots == nil {
			res.ChainMessage = "系统根证书池不可用"
			return
		}
	}
	inters := opts.Intermediates
	if inters == nil {
		inters = x509.NewCertPool()
	}
	for _, c := range parsed.chain {
		inters.AddCert(c)
	}
	vo := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: inters,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		CurrentTime:   opts.CurrentTime,
	}
	chains, err := parsed.cert.Verify(vo)
	if err != nil {
		res.ChainValid = false
		res.ChainMessage = err.Error()
		return
	}
	res.ChainValid = true
	res.ChainMessage = fmt.Sprintf("证书链有效（根: %s）", chains[0][len(chains[0])-1].Subject.String())
}

// verifyCore 校验 from 偏移之后第一个签名的文档完整性与 CMS 签名本身，
// 返回结果、解析出的 CMS 数据与下一个签名可开始搜索的偏移。
func verifyCore(doc []byte, from int) (*VerifyResult, *parsedCMS, int, error) {
	fail := func(format string, args ...interface{}) (*VerifyResult, *parsedCMS, int, error) {
		return nil, nil, 0, fmt.Errorf(format, args...)
	}
	// 1. 定位 /ByteRange 与 /Contents
	bi := bytes.Index(doc[from:], []byte("/ByteRange"))
	if bi < 0 {
		return fail("sign: 无 /ByteRange，文档未签名")
	}
	bi += from
	lb := bytes.Index(doc[bi:], []byte("["))
	rb := bytes.Index(doc[bi:], []byte("]"))
	if lb < 0 || rb < 0 {
		return fail("sign: /ByteRange 格式错误")
	}
	var r0, l0, r1, l1 int
	if _, err := fmt.Sscanf(string(doc[bi+lb:bi+rb+1]), "[%d %d %d %d]", &r0, &l0, &r1, &l1); err != nil {
		return fail("sign: /ByteRange 解析失败: %v", err)
	}
	// 非末位签名的 ByteRange 只覆盖到自身 /Contents 结束（r1+l1 < len(doc)），
	// 属多重签名的正常增量语义；末位签名须覆盖到文件末尾。
	if r0 != 0 || r1 <= l0 || r1+l1 > len(doc) {
		return fail("sign: /ByteRange 范围非法")
	}

	// 2. /Contents 签名值
	ck := bytes.Index(doc[bi:], []byte("/Contents"))
	if ck < 0 {
		return fail("sign: 缺少 /Contents")
	}
	hexStart := bytes.Index(doc[bi+ck:], []byte("<"))
	hexEnd := bytes.Index(doc[bi+ck:], []byte(">"))
	if hexStart < 0 || hexEnd < 0 {
		return fail("sign: /Contents 格式错误")
	}
	// /Contents 以 '0' 填充至定宽占位符：不可按尾部 '0' 反推长度
	//（会误删 CMS 真实尾部的 0x00 字节），应完整解码后按首 TLV 长度截取。
	hexStr := string(doc[bi+ck+hexStart+1 : bi+ck+hexEnd])
	if len(hexStr)%2 != 0 {
		hexStr += "0" // 奇数半字节按规范补 0
	}
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return fail("sign: /Contents 十六进制解码失败: %v", err)
	}
	top, _, err := readTLV(raw)
	if err != nil {
		return fail("sign: /Contents CMS 结构解析失败: %v", err)
	}
	cms := top.raw

	// 3. ByteRange 内容摘要
	h := sha256.New()
	h.Write(doc[r0 : r0+l0])
	h.Write(doc[r1 : r1+l1])
	digest := h.Sum(nil)

	// 4. 解析 CMS 并校验
	parsed, err := parseCMS(cms)
	if err != nil {
		return nil, nil, 0, err
	}
	next := bi + ck + hexEnd + 1 // 下一个签名从本签名 /Contents 之后继续搜索
	res := &VerifyResult{
		Signer:      parsed.cert.Subject.String(),
		SigningTime: parsed.signingTime,
	}
	// RFC 3161 时间戳：imprint 须等于签名值的 SHA-256
	if parsed.timestamp != nil {
		want := sha256.Sum256(parsed.sigValue)
		res.Timestamp = &TimestampInfo{
			Time:         parsed.timestamp.GenTime,
			ImprintValid: bytes.Equal(parsed.timestamp.Imprint, want[:]),
		}
	}
	// 提取 /Reason（加密文档中该字符串是密文，无法离线解密，跳过）
	if bytes.Index(doc, []byte("/Encrypt")) < 0 {
		if rk := bytes.Index(doc[bi:], []byte("/Reason")); rk >= 0 {
			res.Reason = extractPDFString(doc[bi+rk:])
		}
	}

	if !bytes.Equal(parsed.messageDigest, digest) {
		res.Message = "内容摘要不匹配（文档已被篡改）"
		return res, parsed, next, nil
	}
	if err := parsed.verifySignature(); err != nil {
		res.Message = err.Error()
		return res, parsed, next, nil
	}
	res.Valid = true
	res.Message = "签名有效"
	return res, parsed, next, nil
}

// extractPDFString 从 "/Key (value)" 或 "/Key <hex>" 提取字符串值。
func extractPDFString(data []byte) string {
	i := bytes.IndexAny(data, "(<")
	if i < 0 {
		return ""
	}
	if data[i] == '(' {
		j := bytes.IndexByte(data[i:], ')')
		if j < 0 {
			return ""
		}
		return string(data[i+1 : i+j])
	}
	j := bytes.IndexByte(data[i:], '>')
	if j < 0 {
		return ""
	}
	raw, err := hex.DecodeString(string(data[i+1 : i+j]))
	if err != nil {
		return ""
	}
	// UTF-16BE
	if len(raw) >= 2 && raw[0] == 0xfe && raw[1] == 0xff {
		runes := make([]rune, 0, len(raw)/2)
		for k := 2; k+1 < len(raw); k += 2 {
			runes = append(runes, rune(raw[k])<<8|rune(raw[k+1]))
		}
		return string(runes)
	}
	return string(raw)
}
