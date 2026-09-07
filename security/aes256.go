package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/sha512"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// R6（AES-256，V5）实现。ISO 32000-2 §7.6.4。

// hashR6 算法 2.B：R6 迭代散列。
// 注意：循环中的 K 保留完整散列长度（SHA-384 为 48 字节、SHA-512 为 64 字节），
// 仅在最终返回时截断为 32 字节。
func hashR6(password, salt, udata []byte) []byte {
	k0 := sha256.Sum256(concat(password, salt, udata))
	k := k0[:]
	round := 0
	var e []byte
	for {
		round++
		// K1 = (password + K + udata) × 64
		unit := concat(password, k, udata)
		k1 := make([]byte, 0, len(unit)*64)
		for i := 0; i < 64; i++ {
			k1 = append(k1, unit...)
		}
		// E = AES-128-CBC(key=K[:16], iv=K[16:32], K1)，无填充
		block, _ := aes.NewCipher(k[:16])
		e = make([]byte, len(k1))
		cipher.NewCBCEncrypter(block, k[16:32]).CryptBlocks(e, k1)
		// 取 E 前 16 字节大端数 mod 3 选择散列（256≡1 mod 3，等价于字节和）
		sum := 0
		for _, b := range e[:16] {
			sum = (sum*256 + int(b)) % 3
		}
		switch sum {
		case 0:
			s := sha256.Sum256(e)
			k = s[:]
		case 1:
			s := sha512.Sum384(e)
			k = s[:]
		case 2:
			s := sha512.Sum512(e)
			k = s[:]
		}
		if round >= 64 && e[len(e)-1] <= byte(round-32) {
			break
		}
	}
	return k[:32]
}

func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// aes256NoPad AES-256-CBC，零 IV、无填充（用于 UE/OE/Perms）。
func aes256NoPad(key, data []byte) []byte {
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, make([]byte, 16)).CryptBlocks(out, data)
	return out
}

// initR6 计算 R6 的 O/U/OE/UE/Perms 与 256 位文件密钥。
func (h *Handler) initR6(fileID []byte) error {
	pw := []byte(h.opts.UserPassword)
	if len(pw) > 127 {
		pw = pw[:127]
	}
	opw := []byte(h.opts.OwnerPassword)
	if len(opw) > 127 {
		opw = opw[:127]
	}

	// 文件密钥：32 字节随机
	h.key = randomBytes(32)

	// U / UE
	vsalt, ksalt := randomBytes(8), randomBytes(8)
	uHash := hashR6(pw, vsalt, nil)
	uVal := concat(uHash, vsalt, ksalt)
	ue := aes256NoPad(hashR6(pw, ksalt, nil), h.key)

	// O / OE（udata = U）
	ovsalt, oksalt := randomBytes(8), randomBytes(8)
	oHash := hashR6(opw, ovsalt, uVal)
	oVal := concat(oHash, ovsalt, oksalt)
	oe := aes256NoPad(hashR6(opw, oksalt, uVal), h.key)

	// Perms：P(4B LE) + 0xFFFFFFFF + 'T' + "adb" + 4B 随机
	p := h.pValue()
	perms := make([]byte, 16)
	perms[0] = byte(p)
	perms[1] = byte(p >> 8)
	perms[2] = byte(p >> 16)
	perms[3] = byte(p >> 24)
	for i := 4; i < 8; i++ {
		perms[i] = 0xff
	}
	perms[8] = 'T' // EncryptMetadata
	copy(perms[9:12], "adb")
	copy(perms[12:], randomBytes(4))
	permsEnc := aes256NoPad(h.key, perms)

	h.dict = object.NewDict()
	h.dict.Set("Filter", object.Name("Standard"))
	h.dict.Set("V", object.Int(5))
	h.dict.Set("R", object.Int(6))
	h.dict.Set("Length", object.Int(256))
	h.dict.Set("P", object.Int(h.pValue()))
	h.dict.Set("O", object.HexString(oVal))
	h.dict.Set("U", object.HexString(uVal))
	h.dict.Set("OE", object.HexString(oe))
	h.dict.Set("UE", object.HexString(ue))
	h.dict.Set("Perms", object.HexString(permsEnc))
	h.dict.Set("EncryptMetadata", object.Bool(true))
	h.dict.Set("CF", object.NewDict().Set("StdCF", object.NewDict().
		Set("CFM", object.Name("AESV3")).
		Set("AuthEvent", object.Name("DocOpen")).
		Set("Length", object.Int(32))))
	h.dict.Set("StmF", object.Name("StdCF"))
	h.dict.Set("StrF", object.Name("StdCF"))
	return nil
}
