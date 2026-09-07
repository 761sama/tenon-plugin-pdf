package font

// winansiExtra 为 WinAnsiEncoding 中 0x80–0x9F 区间的特殊字符映射（rune → 字节）。
// 0x81、0x8D、0x8F、0x90、0x9D 未定义。
var winansiExtra = map[rune]byte{
	0x20AC: 0x80, // €
	0x201A: 0x82, // ‚
	0x0192: 0x83, // ƒ
	0x201E: 0x84, // „
	0x2026: 0x85, // …
	0x2020: 0x86, // †
	0x2021: 0x87, // ‡
	0x02C6: 0x88, // ˆ
	0x2030: 0x89, // ‰
	0x0160: 0x8A, // Š
	0x2039: 0x8B, // ‹
	0x0152: 0x8C, // Œ
	0x017D: 0x8E, // Ž
	0x2018: 0x91, // '
	0x2019: 0x92, // '
	0x201C: 0x93, // "
	0x201D: 0x94, // "
	0x2022: 0x95, // •
	0x2013: 0x96, // –
	0x2014: 0x97, // —
	0x02DC: 0x98, // ˜
	0x2122: 0x99, // ™
	0x0161: 0x9A, // š
	0x203A: 0x9B, // ›
	0x0153: 0x9C, // œ
	0x017E: 0x9E, // ž
	0x0178: 0x9F, // Ÿ
}

// encodeWinAnsi 将 Unicode 码点映射为 WinAnsiEncoding 字节。
// 未定义字符返回 '?'。
func encodeWinAnsi(r rune) byte {
	switch {
	case r >= 0x20 && r <= 0x7e:
		return byte(r)
	case r >= 0xa0 && r <= 0xff:
		return byte(r)
	}
	if b, ok := winansiExtra[r]; ok {
		return b
	}
	return '?'
}

// DecodeWinAnsi 将 WinAnsiEncoding 字节序列解码为 UTF-8 字符串（用于测试与调试）。
func DecodeWinAnsi(data []byte) string {
	rev := make(map[byte]rune, len(winansiExtra))
	for r, b := range winansiExtra {
		rev[b] = r
	}
	out := make([]rune, 0, len(data))
	for _, b := range data {
		switch {
		case b >= 0x20 && b <= 0x7e:
			out = append(out, rune(b))
		case b >= 0xa0:
			out = append(out, rune(b))
		default:
			if r, ok := rev[b]; ok {
				out = append(out, r)
			} else {
				out = append(out, '\ufffd')
			}
		}
	}
	return string(out)
}
