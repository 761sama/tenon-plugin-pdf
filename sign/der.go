package sign

import (
	"fmt"
	"math/big"
	"time"
)

// 手写 DER 编码（encoding/asn1 无法满足 CMS 的全部需求）。

// 编码 TLV：tag + length + content。
func derT(tag byte, content []byte) []byte {
	out := []byte{tag}
	out = append(out, derLen(len(content))...)
	return append(out, content...)
}

// 编码 DER 长度域（短形/长形）。
func derLen(n int) []byte {
	if n < 128 {
		return []byte{byte(n)}
	}
	var b []byte
	for v := n; v > 0; v >>= 8 {
		b = append([]byte{byte(v)}, b...)
	}
	return append([]byte{0x80 | byte(len(b))}, b...)
}

// 编码 SEQUENCE（内容按序拼接）。
func derSeq(parts ...[]byte) []byte { return derT(0x30, derConcat(parts...)) }

// 编码 SET（内容按序拼接，调用方须保证 DER 排序）。
func derSet(parts ...[]byte) []byte { return derT(0x31, derConcat(parts...)) }

// 拼接多个 DER 片段。
func derConcat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// 编码对象标识符。
func derOID(oids ...int) []byte {
	var body []byte
	body = append(body, byte(oids[0]*40+oids[1]))
	for _, v := range oids[2:] {
		// base-128 大端
		var stack []byte
		stack = append(stack, byte(v&0x7f))
		for v >>= 7; v > 0; v >>= 7 {
			stack = append([]byte{byte(v&0x7f) | 0x80}, stack...)
		}
		body = append(body, stack...)
	}
	return derT(0x06, body)
}

// 编码 INTEGER（正数高位为 1 时补前导零避免负数误判）。
func derInt(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return derT(0x02, b)
}

// 编码 OCTET STRING。
func derOctetString(b []byte) []byte { return derT(0x04, b) }

// 编码 UTCTime（YYMMDDHHMMSSZ，UTC）。
func derUTCTime(t time.Time) []byte {
	return derT(0x17, []byte(t.UTC().Format("060102150405Z")))
}

// 编码 [0] EXPLICIT 上下文标签（内容完整包裹）。
func derExplicit0(content []byte) []byte { return derT(0xa0, content) }

// 编码 [0] IMPLICIT 上下文标签（内容直接替换原标签）。
func derImplicit0(content []byte) []byte { return derT(0xa0, content) }

var derNull = derT(0x05, nil)

// --- DER 解析（TLV 遍历） ---

type tlv struct {
	tag     byte
	content []byte
	raw     []byte
}

// 读取一个 TLV，返回它与剩余字节。
func readTLV(data []byte) (tlv, []byte, error) {
	if len(data) < 2 {
		return tlv{}, nil, fmt.Errorf("sign: DER 数据过短")
	}
	tag := data[0]
	l := int(data[1])
	hdrlen := 2
	if l&0x80 != 0 {
		n := l & 0x7f
		if n == 0 || n > 4 || len(data) < 2+n {
			return tlv{}, nil, fmt.Errorf("sign: DER 长度字段非法")
		}
		l = 0
		for i := 0; i < n; i++ {
			l = l<<8 | int(data[2+i])
		}
		hdrlen += n
	}
	if len(data) < hdrlen+l {
		return tlv{}, nil, fmt.Errorf("sign: DER 内容越界")
	}
	return tlv{tag: tag, content: data[hdrlen : hdrlen+l], raw: data[:hdrlen+l]}, data[hdrlen+l:], nil
}

// 展开 SEQUENCE/SET/[0] 的内容为 TLV 列表。
func parseSeq(data []byte) ([]tlv, error) {
	var out []tlv
	for len(data) > 0 {
		t, rest, err := readTLV(data)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		data = rest
	}
	return out, nil
}

// 比较 TLV 中的 OID 与给定 OID。
func oidEqual(t tlv, oids ...int) bool {
	want := derOID(oids...)
	if t.tag != 0x06 || len(t.raw) != len(want) {
		return false
	}
	for i := range t.raw {
		if t.raw[i] != want[i] {
			return false
		}
	}
	return true
}
