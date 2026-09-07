package security

// rc4 流密码（仅用于 R4 握手的 O/U 值计算，规范强制；不用于内容加密）。
// 安全提示：RC4 已被认为不安全，新文档请使用 AES-256（LevelAES256）。

type rc4Cipher struct {
	s [256]byte
	i uint8
	j uint8
}

func newRC4(key []byte) *rc4Cipher {
	c := &rc4Cipher{}
	for i := 0; i < 256; i++ {
		c.s[i] = byte(i)
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + int(c.s[i]) + int(key[i%len(key)])) & 0xff
		c.s[i], c.s[j] = c.s[j], c.s[i]
	}
	return c
}

func (c *rc4Cipher) crypt(data []byte) []byte {
	out := make([]byte, len(data))
	for k := range data {
		c.i++
		c.j += c.s[c.i]
		c.s[c.i], c.s[c.j] = c.s[c.j], c.s[c.i]
		out[k] = data[k] ^ c.s[(c.s[c.i]+c.s[c.j])&0xff]
	}
	return out
}

// rc4 便捷函数。
func rc4(key, data []byte) []byte { return newRC4(key).crypt(data) }
