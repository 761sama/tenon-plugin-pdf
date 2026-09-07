package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"math/big"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// 公钥安全处理器（Public-Key Security Handler），ISO 32000-1 §7.6.5：
// /Filter /Adobe.PubSec + /SubFilter /adbe.pkcs7.s5，支持多收件人。
//
// 算法（与 Adobe/PDFBox 实现一致）：
//  1. 生成 20 字节随机种子 seed；
//  2. 每个收件人一个 CMS EnvelopedData，内容为 seed ‖ 该收件人权限（4 字节大端）；
//     密钥传输用收件人证书的 RSA 公钥（PKCS#1 v1.5），内容加密用 AES-128-CBC；
//  3. 文件密钥 = HASH(seed ‖ 各收件人信封 DER 按序拼接) 截取所需长度——
//     V4（AESV2）用 SHA-1 取 16 字节，V5（AESV3）用 SHA-256 取 32 字节；
//  4. 内容加密与标准处理器相同：V4 对象级密钥派生（MD5+sAlT），V5 直接用文件密钥，
//     因此 crypt/EncryptObject 路径完全复用。
//
// 注意：信封字节本身参与密钥派生，必须先构建全部信封再计算文件密钥。
// 仅支持 RSA 收件人证书（PDF 读者普遍仅支持 RSA 密钥传输）。

// Recipient 公钥加密收件人。
type Recipient struct {
	Certificate *x509.Certificate // 收件人证书（必须是 RSA 公钥证书）
	Permissions Permission        // 该收件人的权限，零值表示 PermAll
}

// PubKeyOptions 公钥（证书）加密选项。
type PubKeyOptions struct {
	Recipients []Recipient // 收件人（至少一个），各持私钥者可解密
	Level      Level       // AES128（V4，默认）或 AES256（V5）
}

// NewPubKeyHandler 创建公钥安全处理器。
// 与 NewHandler 不同：不需要文件 ID（密钥派生不依赖 ID），也没有密码。
func NewPubKeyHandler(opts PubKeyOptions) (*Handler, error) {
	if len(opts.Recipients) == 0 {
		return nil, fmt.Errorf("security: 公钥加密至少需要一个收件人")
	}
	h := &Handler{
		opts:   Options{Level: opts.Level},
		aes256: opts.Level == AES256,
	}

	// 1. 随机种子 + 逐收件人信封（内容为 seed ‖ 权限 4 字节大端）
	seed := randomBytes(20)
	envelopes := make([][]byte, len(opts.Recipients))
	for i, r := range opts.Recipients {
		if r.Certificate == nil {
			return nil, fmt.Errorf("security: 收件人 %d 缺少证书", i)
		}
		perms := r.Permissions
		if perms == 0 {
			perms = PermAll
		}
		pv := uint32(perms) | 0xFFFFF000 // 与 /P 相同的位布局
		content := make([]byte, 24)
		copy(content, seed)
		content[20] = byte(pv >> 24)
		content[21] = byte(pv >> 16)
		content[22] = byte(pv >> 8)
		content[23] = byte(pv)
		env, err := buildEnvelope(content, r.Certificate)
		if err != nil {
			return nil, fmt.Errorf("security: 收件人 %d 信封构建失败: %w", i, err)
		}
		envelopes[i] = env
	}

	// 2. 文件密钥 = HASH(seed ‖ 各信封 DER)（EncryptMetadata 为 true，不附加 0xFFFFFFFF）
	shaInput := append([]byte(nil), seed...)
	for _, e := range envelopes {
		shaInput = append(shaInput, e...)
	}
	if h.aes256 {
		sum := sha256.Sum256(shaInput)
		h.key = append([]byte(nil), sum[:]...)
	} else {
		sum := sha1.Sum(shaInput)
		h.key = append([]byte(nil), sum[:r4KeyLen]...)
	}

	// 3. Encrypt 字典：V4/V5 时 Recipients 挂在默认密码过滤器（DefaultCryptFilter）下
	rcpts := make(object.Array, len(envelopes))
	for i, e := range envelopes {
		rcpts[i] = object.HexString(e)
	}
	// /P 取全体收件人权限的并集
	var allPerms Permission
	for _, r := range opts.Recipients {
		p := r.Permissions
		if p == 0 {
			p = PermAll
		}
		allPerms |= p
	}
	h.opts.Permissions = allPerms

	d := object.NewDict()
	d.Set("Filter", object.Name("Adobe.PubSec"))
	d.Set("SubFilter", object.Name("adbe.pkcs7.s5"))
	d.Set("P", object.Int(h.pValue()))
	d.Set("EncryptMetadata", object.Bool(true))
	cf := object.NewDict().Set("Type", object.Name("CryptFilter"))
	if h.aes256 {
		d.Set("V", object.Int(5))
		d.Set("R", object.Int(5))
		d.Set("Length", object.Int(256))
		cf.Set("CFM", object.Name("AESV3"))
		cf.Set("Length", object.Int(256))
	} else {
		d.Set("V", object.Int(4))
		d.Set("R", object.Int(4))
		d.Set("Length", object.Int(128))
		cf.Set("CFM", object.Name("AESV2"))
		cf.Set("Length", object.Int(128))
	}
	cf.Set("AuthEvent", object.Name("DocOpen"))
	cf.Set("Recipients", rcpts)
	d.Set("CF", object.NewDict().Set("DefaultCryptFilter", cf))
	d.Set("StmF", object.Name("DefaultCryptFilter"))
	d.Set("StrF", object.Name("DefaultCryptFilter"))
	h.dict = d
	return h, nil
}

// CMS/PKCS#7 OID（enveloped-data 相关；与 sign 包独立的副本，保持包依赖单向）。
var (
	oidEnvelopedData = []int{1, 2, 840, 113549, 1, 7, 3}
	oidCMSData       = []int{1, 2, 840, 113549, 1, 7, 1}
	oidRSAEnc        = []int{1, 2, 840, 113549, 1, 1, 1}
	oidAES128CBC     = []int{2, 16, 840, 1, 101, 3, 4, 1, 2}
)

// buildEnvelope 将 content（24 字节：seed ‖ 权限）封装为收件人的
// CMS EnvelopedData（DER）：随机 CEK 以 AES-128-CBC 加密内容，
// CEK 用收件人 RSA 公钥以 PKCS#1 v1.5 包裹（KeyTransRecipientInfo，
// IssuerAndSerialNumber 标识收件人）。
func buildEnvelope(content []byte, cert *x509.Certificate) ([]byte, error) {
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("收件人证书 %q 不是 RSA 公钥", cert.Subject.String())
	}
	cek := randomBytes(16)
	iv := randomBytes(16)
	encContent := aesCryptWithIV(cek, iv, content)
	encKey, err := rsa.EncryptPKCS1v15(rand.Reader, pub, cek)
	if err != nil {
		return nil, err
	}

	recipientInfo := pkSeq(
		pkInt(0),
		pkSeq(cert.RawIssuer, pkT(0x02, pkIntBytes(cert.SerialNumber))),
		pkSeq(pkOID(oidRSAEnc), pkNull),
		pkT(0x04, encKey),
	)
	encContentInfo := pkSeq(
		pkOID(oidCMSData),
		pkSeq(pkOID(oidAES128CBC), pkT(0x04, iv)),
		pkT(0x80, encContent), // [0] IMPLICIT OCTET STRING
	)
	envelopedData := pkSeq(
		pkInt(0),
		pkT(0x31, recipientInfo), // SET OF RecipientInfo
		encContentInfo,
	)
	return pkSeq(
		pkOID(oidEnvelopedData),
		pkT(0xa0, envelopedData), // [0] EXPLICIT
	), nil
}

// aesCryptWithIV AES-128-CBC 加密（显式 IV + PKCS#7 填充），输出仅密文。
func aesCryptWithIV(key, iv, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	padLen := 16 - len(data)%16
	padded := append([]byte(nil), data...)
	for i := 0; i < padLen; i++ {
		padded = append(padded, byte(padLen))
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

// --- 极简 DER 编码（security 包不依赖 sign，保持依赖方向 security→object） ---

func pkT(tag byte, content []byte) []byte {
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

func pkSeq(parts ...[]byte) []byte {
	var c []byte
	for _, p := range parts {
		c = append(c, p...)
	}
	return pkT(0x30, c)
}

var pkNull = []byte{0x05, 0x00}

func pkInt(v int64) []byte { return pkT(0x02, pkIntBytes(big.NewInt(v))) }

func pkIntBytes(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return b
}

func pkOID(oids []int) []byte {
	body := []byte{byte(oids[0]*40 + oids[1])}
	for _, v := range oids[2:] {
		stack := []byte{byte(v & 0x7f)}
		for v >>= 7; v > 0; v >>= 7 {
			stack = append([]byte{byte(v&0x7f) | 0x80}, stack...)
		}
		body = append(body, stack...)
	}
	return pkT(0x06, body)
}
