package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// R4（AES-128，V4）实现。ISO 32000-1 §7.6.3。

var pwPadding = []byte{
	0x28, 0xbf, 0x4e, 0x5e, 0x4e, 0x75, 0x8a, 0x41,
	0x64, 0x00, 0x4e, 0x56, 0xff, 0xfa, 0x01, 0x08,
	0x2e, 0x2e, 0x00, 0xb6, 0xd0, 0x68, 0x3e, 0x80,
	0x2f, 0x0c, 0xa9, 0xfe, 0x64, 0x53, 0x69, 0x7a,
}

const r4KeyLen = 16 // 128 位

// padPassword 按规范填充/截断密码到 32 字节。
func padPassword(pw string) []byte {
	p := make([]byte, 32)
	n := copy(p, pw)
	copy(p[n:], pwPadding)
	return p
}

// initR4 计算 R4 的 O/U 值与文件加密密钥。
func (h *Handler) initR4(fileID []byte) {
	userPad := padPassword(h.opts.UserPassword)
	ownerPad := padPassword(h.opts.OwnerPassword)

	// O 值（算法 2）
	digest := md5.Sum(ownerPad)
	d := digest[:]
	for i := 0; i < 50; i++ {
		nd := md5.Sum(d[:r4KeyLen])
		d = nd[:]
	}
	ownerKey := append([]byte(nil), d[:r4KeyLen]...)
	oVal := userPad
	for i := 0; i < 20; i++ {
		k := make([]byte, r4KeyLen)
		for j := range k {
			k[j] = ownerKey[j] ^ byte(i)
		}
		oVal = rc4(k, oVal)
	}

	// 文件加密密钥（算法 5）
	p := h.pValue()
	pb := []byte{byte(p), byte(p >> 8), byte(p >> 16), byte(p >> 24)}
	m := md5.New()
	m.Write(userPad)
	m.Write(oVal)
	m.Write(pb)
	m.Write(fileID)
	d2 := m.Sum(nil)
	for i := 0; i < 50; i++ {
		nd := md5.Sum(d2[:r4KeyLen])
		d2 = nd[:]
	}
	h.key = append([]byte(nil), d2[:r4KeyLen]...)

	// U 值（算法 5.2，R3+）
	um := md5.New()
	um.Write(pwPadding)
	um.Write(fileID)
	uVal := um.Sum(nil)
	for i := 0; i < 20; i++ {
		k := make([]byte, r4KeyLen)
		for j := range k {
			k[j] = h.key[j] ^ byte(i)
		}
		uVal16 := rc4(k, uVal)
		copy(uVal[:], uVal16)
	}
	uFull := append(uVal[:], randomBytes(16)...)

	h.dict = object.NewDict()
	h.dict.Set("Filter", object.Name("Standard"))
	h.dict.Set("V", object.Int(4))
	h.dict.Set("R", object.Int(4))
	h.dict.Set("Length", object.Int(128))
	h.dict.Set("P", object.Int(h.pValue()))
	h.dict.Set("O", object.HexString(oVal))
	h.dict.Set("U", object.HexString(uFull))
	h.dict.Set("EncryptMetadata", object.Bool(true))
	h.dict.Set("CF", object.NewDict().Set("StdCF", object.NewDict().
		Set("CFM", object.Name("AESV2")).
		Set("AuthEvent", object.Name("DocOpen")).
		Set("Length", object.Int(16))))
	h.dict.Set("StmF", object.Name("StdCF"))
	h.dict.Set("StrF", object.Name("StdCF"))
}

// crypt 加密/解密单个对象内容（R4：派生对象密钥 + AES-128-CBC + 随机 IV）。
func (h *Handler) crypt(num int, data []byte) []byte {
	if h.aes256 {
		return aesCrypt(h.key, data) // R6：直接使用文件密钥
	}
	// 对象级密钥：MD5(fileKey + objNum(3B LE) + gen(2B LE) + "sAlT")
	m := md5.New()
	m.Write(h.key)
	m.Write([]byte{byte(num), byte(num >> 8), byte(num >> 16), 0, 0})
	m.Write([]byte("sAlT"))
	key := m.Sum(nil)
	return aesCrypt(key[:16], data)
}

// aesCrypt AES-CBC 加密：随机 16 字节 IV 前缀 + PKCS#7 填充。
func aesCrypt(key, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	iv := randomBytes(16)
	// PKCS#7 填充
	padLen := 16 - len(data)%16
	padded := append(append([]byte(nil), data...), bytes.Repeat([]byte{byte(padLen)}, padLen)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return append(iv, out...)
}
