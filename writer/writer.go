// Package writer 负责 PDF 文件级结构（ISO 32000-1 §7.5）：
// 文件头、间接对象体、交叉引用表与文件尾（trailer）。
package writer

import (
	"crypto/rand"
	"fmt"
	"io"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Encryptor 对象加密器（标准安全处理器实现）。
type Encryptor interface {
	// EncryptObject 返回对象加密后的版本（字符串与流被加密）。
	// num 为间接对象号（从 1 起），用于对象级密钥派生。
	EncryptObject(num int, obj object.Object) object.Object
}

// Writer 收集间接对象并输出完整 PDF 文件。
type Writer struct {
	objs       []object.Object // 下标+1 即对象号
	version    string
	encryptor  Encryptor
	encryptRef object.Ref
}

// New 创建写入器（PDF 版本 1.7）。
func New() *Writer { return &Writer{version: "1.7"} }

// SetEncrypt 启用文档加密：ref 为 Encrypt 字典的间接引用（其自身不加密），
// e 为加密处理器。trailer 会自动加入 /Encrypt 条目。
func (w *Writer) SetEncrypt(ref object.Ref, e Encryptor) {
	w.encryptRef = ref
	w.encryptor = e
}

// Alloc 预留一个间接对象号，内容稍后通过 Set 填充。
func (w *Writer) Alloc() object.Ref {
	w.objs = append(w.objs, nil)
	return object.Ref{Num: len(w.objs)}
}

// Add 登记一个间接对象，返回其引用。
func (w *Writer) Add(o object.Object) object.Ref {
	w.objs = append(w.objs, o)
	return object.Ref{Num: len(w.objs)}
}

// Set 填充或替换已登记的对象内容。
func (w *Writer) Set(r object.Ref, o object.Object) { w.objs[r.Num-1] = o }

// NumObjects 返回已登记的间接对象数量。
func (w *Writer) NumObjects() int { return len(w.objs) }

// countingWriter 统计写入字节数。
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// WriteTo 输出完整 PDF 文件。root 为文档目录（Catalog）引用；
// info 为文档信息字典（可为 nil）；id 为 16 字节文件标识（为空则随机生成）。
func (w *Writer) WriteTo(out io.Writer, root object.Ref, info object.Object, id []byte) (int64, error) {
	if len(id) == 0 {
		id = make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return 0, err
		}
	}
	cw := &countingWriter{w: out}

	// 文件头：版本声明 + 二进制标记注释
	if _, err := fmt.Fprintf(cw, "%%PDF-%s\n%%\xe2\xe3\xcf\xd3\n", w.version); err != nil {
		return 0, err
	}

	// 间接对象体
	offsets := make([]int64, len(w.objs)+1)
	for i, o := range w.objs {
		offsets[i+1] = cw.n
		if o == nil {
			o = object.Null
		}
		if w.encryptor != nil && i+1 != w.encryptRef.Num {
			o = w.encryptor.EncryptObject(i+1, o)
		}
		if _, err := fmt.Fprintf(cw, "%d 0 obj\n", i+1); err != nil {
			return 0, err
		}
		if _, err := cw.Write(object.Serialize(o)); err != nil {
			return 0, err
		}
		if _, err := cw.Write([]byte("\nendobj\n")); err != nil {
			return 0, err
		}
	}

	// 交叉引用表
	xrefPos := cw.n
	if _, err := fmt.Fprintf(cw, "xref\n0 %d\n", len(w.objs)+1); err != nil {
		return 0, err
	}
	if _, err := cw.Write([]byte("0000000000 65535 f \n")); err != nil {
		return 0, err
	}
	for i := 1; i <= len(w.objs); i++ {
		if _, err := fmt.Fprintf(cw, "%010d 00000 n \n", offsets[i]); err != nil {
			return 0, err
		}
	}

	// 文件尾
	trailer := object.NewDict()
	trailer.Set("Size", object.Int(len(w.objs)+1))
	trailer.Set("Root", root)
	if info != nil {
		trailer.Set("Info", info)
	}
	if w.encryptor != nil {
		trailer.Set("Encrypt", w.encryptRef)
	}
	trailer.Set("ID", object.Array{object.HexString(id), object.HexString(id)})
	if _, err := cw.Write([]byte("trailer\n")); err != nil {
		return 0, err
	}
	if _, err := cw.Write(object.Serialize(trailer)); err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(cw, "\nstartxref\n%d\n%%%%EOF\n", xrefPos); err != nil {
		return 0, err
	}
	return cw.n, nil
}
