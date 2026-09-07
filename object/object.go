// Package object 实现 PDF 对象模型（ISO 32000-1 §7.3）及其序列化。
//
// 支持的类型：null、布尔、整数、实数、字符串（字面量与十六进制）、
// 名称、数组、字典、流与间接引用。所有类型都实现 Object 接口，
// 通过 Encode 将自身序列化为 PDF 语法并追加到字节切片。
package object

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// Object 表示任意 PDF 对象。
type Object interface {
	// Encode 将对象序列化并追加到 dst，返回追加后的切片。
	Encode(dst []byte) []byte
}

// Serialize 返回对象的完整序列化字节。
func Serialize(o Object) []byte { return o.Encode(nil) }

const upperhex = "0123456789ABCDEF"

// --- null ---

// Null 表示 PDF null 对象。
var Null = null{}

type null struct{}

func (null) Encode(dst []byte) []byte { return append(dst, "null"...) }

// --- 布尔 ---

// Bool 布尔对象。
type Bool bool

func (b Bool) Encode(dst []byte) []byte {
	if b {
		return append(dst, "true"...)
	}
	return append(dst, "false"...)
}

// --- 数值 ---

// Int 整数对象。
type Int int64

func (i Int) Encode(dst []byte) []byte { return strconv.AppendInt(dst, int64(i), 10) }

// Real 实数对象。
type Real float64

func (r Real) Encode(dst []byte) []byte {
	return strconv.AppendFloat(dst, float64(r), 'f', -1, 64)
}

// --- 名称 ---

// nameDelims 是名称对象中必须转义的定界字符。
const nameDelims = "()<>[]{}/%#"

// Name 名称对象（/Name）。非法字符自动转义为 #XX。
type Name string

func (n Name) Encode(dst []byte) []byte {
	dst = append(dst, '/')
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c < 0x21 || c > 0x7E || strings.IndexByte(nameDelims, c) >= 0 {
			dst = append(dst, '#', upperhex[c>>4], upperhex[c&0x0f])
		} else {
			dst = append(dst, c)
		}
	}
	return dst
}

// --- 字符串 ---

// String 字面量字符串对象 (…)。定界符、控制字符与非 ASCII 字节自动转义。
type String []byte

// Str 由 Go 字符串构造字面量字符串对象。
func Str(s string) String { return String(s) }

// TextStr 按 PDF 文本字符串规范（ISO 32000-1 §7.9.2）编码：
// 纯 ASCII 使用字面量字符串（PDFDocEncoding），否则使用 UTF-16BE 十六进制字符串（含 BOM）。
// 用于文档信息、书签标题、批注内容等文本串。
func TextStr(s string) Object {
	ascii := true
	for _, r := range s {
		if r > 0x7e {
			ascii = false
			break
		}
	}
	if ascii {
		return Str(s)
	}
	buf := make([]byte, 0, len(s)*2+2)
	buf = append(buf, 0xfe, 0xff)
	for _, u := range utf16.Encode([]rune(s)) {
		buf = append(buf, byte(u>>8), byte(u))
	}
	return HexString(buf)
}

func (s String) Encode(dst []byte) []byte {
	dst = append(dst, '(')
	for _, c := range s {
		switch c {
		case '(', ')', '\\':
			dst = append(dst, '\\', c)
		case '\n':
			dst = append(dst, `\n`...)
		case '\r':
			dst = append(dst, `\r`...)
		case '\t':
			dst = append(dst, `\t`...)
		case '\b':
			dst = append(dst, `\b`...)
		case '\f':
			dst = append(dst, `\f`...)
		default:
			if c < 0x20 || c > 0x7e {
				dst = append(dst, '\\', '0'+c>>6, '0'+(c>>3)&0x07, '0'+c&0x07)
			} else {
				dst = append(dst, c)
			}
		}
	}
	return append(dst, ')')
}

// HexString 十六进制字符串对象 <…>。
type HexString []byte

func (s HexString) Encode(dst []byte) []byte {
	dst = append(dst, '<')
	for _, c := range s {
		dst = append(dst, upperhex[c>>4], upperhex[c&0x0f])
	}
	return append(dst, '>')
}

// --- 数组 ---

// Array 数组对象 […]。
type Array []Object

func (a Array) Encode(dst []byte) []byte {
	dst = append(dst, '[')
	for i, o := range a {
		if i > 0 {
			dst = append(dst, ' ')
		}
		if o == nil {
			o = Null
		}
		dst = o.Encode(dst)
	}
	return append(dst, ']')
}

// Rect 构造矩形数组 [x0 y0 x1 y1]。
func Rect(x0, y0, x1, y1 float64) Array {
	return Array{Real(x0), Real(y0), Real(x1), Real(y1)}
}

// --- 字典 ---

// Dict 字典对象 << … >>。键保持插入顺序，输出确定。
type Dict struct {
	keys []string
	vals []Object
}

// NewDict 创建空字典。
func NewDict() *Dict { return &Dict{} }

// Set 设置键值；键已存在则替换值并保持原位。返回 d 以支持链式调用。
func (d *Dict) Set(key string, v Object) *Dict {
	for i, k := range d.keys {
		if k == key {
			d.vals[i] = v
			return d
		}
	}
	d.keys = append(d.keys, key)
	d.vals = append(d.vals, v)
	return d
}

// Get 读取键对应的值。
func (d *Dict) Get(key string) (Object, bool) {
	for i, k := range d.keys {
		if k == key {
			return d.vals[i], true
		}
	}
	return nil, false
}

// Delete 删除键。
func (d *Dict) Delete(key string) {
	for i, k := range d.keys {
		if k == key {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			d.vals = append(d.vals[:i], d.vals[i+1:]...)
			return
		}
	}
}

// Len 返回字典中键的数量。
func (d *Dict) Len() int { return len(d.keys) }

// Keys 按插入顺序返回全部键。
func (d *Dict) Keys() []string {
	out := make([]string, len(d.keys))
	copy(out, d.keys)
	return out
}

func (d *Dict) Encode(dst []byte) []byte {
	dst = append(dst, "<<"...)
	for i, k := range d.keys {
		dst = append(dst, ' ')
		dst = Name(k).Encode(dst)
		dst = append(dst, ' ')
		dst = d.vals[i].Encode(dst)
	}
	return append(dst, " >>"...)
}

// --- 流 ---

// Stream 流对象：字典 + 字节数据。/Length 在序列化时自动写入。
type Stream struct {
	Dict *Dict
	Data []byte
}

// NewStream 以给定数据创建流对象。
func NewStream(data []byte) *Stream {
	return &Stream{Dict: NewDict(), Data: data}
}

func (s *Stream) Encode(dst []byte) []byte {
	s.Dict.Set("Length", Int(len(s.Data)))
	dst = s.Dict.Encode(dst)
	dst = append(dst, "\nstream\n"...)
	dst = append(dst, s.Data...)
	return append(dst, "\nendstream"...)
}

// --- 间接引用 ---

// Ref 间接对象引用（N G R）。
type Ref struct {
	Num int
	Gen int
}

func (r Ref) Encode(dst []byte) []byte {
	dst = strconv.AppendInt(dst, int64(r.Num), 10)
	dst = append(dst, ' ')
	dst = strconv.AppendInt(dst, int64(r.Gen), 10)
	return append(dst, " R"...)
}

// Raw 原样字节对象：序列化时不做任何加工。
// 用于数字签名占位符等需要精确控制字节布局的场景，慎用。
type Raw string

func (r Raw) Encode(dst []byte) []byte { return append(dst, string(r)...) }
