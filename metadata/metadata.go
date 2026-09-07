// Package metadata 实现 PDF 文档信息字典（ISO 32000-1 §14.3.3）
// 与 XMP 元数据流（§14.3.2）。
package metadata

import (
	"fmt"
	"strings"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Producer 默认生成器标识。
const Producer = "tenon-plugin-pdf"

// Info 文档信息。
type Info struct {
	Title        string
	Author       string
	Subject      string
	Keywords     string
	Creator      string // 创建工具
	Producer     string // 生成器，默认 tenon-plugin-pdf
	CreationDate time.Time
	ModDate      time.Time
}

// Empty 返回信息是否全为空。
func (i *Info) Empty() bool {
	return i.Title == "" && i.Author == "" && i.Subject == "" && i.Keywords == "" &&
		i.Creator == "" && i.CreationDate.IsZero() && i.ModDate.IsZero()
}

func (i *Info) producer() string {
	if i.Producer != "" {
		return i.Producer
	}
	return Producer
}

// pdfDate 格式化为 PDF 日期字符串 D:YYYYMMDDHHmmSS+HH'mm'。
func pdfDate(t time.Time) string {
	_, off := t.Zone()
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	return fmt.Sprintf("%s%s%02d'%02d'", t.Format("D:20060102150405"), sign, off/3600, off%3600/60)
}

// FormatDate 导出 PDF 日期格式化（供签名等模块使用）。
func FormatDate(t time.Time) string { return pdfDate(t) }

// Dict 生成文档信息字典。
func (i *Info) Dict() *object.Dict {
	d := object.NewDict()
	if i.Title != "" {
		d.Set("Title", object.TextStr(i.Title))
	}
	if i.Author != "" {
		d.Set("Author", object.TextStr(i.Author))
	}
	if i.Subject != "" {
		d.Set("Subject", object.TextStr(i.Subject))
	}
	if i.Keywords != "" {
		d.Set("Keywords", object.TextStr(i.Keywords))
	}
	if i.Creator != "" {
		d.Set("Creator", object.TextStr(i.Creator))
	}
	d.Set("Producer", object.TextStr(i.producer()))
	if !i.CreationDate.IsZero() {
		d.Set("CreationDate", object.Str(pdfDate(i.CreationDate)))
	}
	if !i.ModDate.IsZero() {
		d.Set("ModDate", object.Str(pdfDate(i.ModDate)))
	}
	return d
}

// XMP 生成 XMP 元数据包内容。
func (i *Info) XMP() []byte {
	esc := func(s string) string {
		r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
		return r.Replace(s)
	}
	xmpDate := func(t time.Time) string { return t.Format("2006-01-02T15:04:05Z07:00") }

	var sb strings.Builder
	sb.WriteString("<?xpacket begin=\"\uFEFF\" id=\"W5M0MpCehiHzreSzNTczkc9c\"?>\n")
	sb.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	sb.WriteString(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	sb.WriteString(`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/" xmlns:xmp="http://ns.adobe.com/xap/1.0/">` + "\n")
	if i.Title != "" {
		sb.WriteString("<dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">" + esc(i.Title) + "</rdf:li></rdf:Alt></dc:title>\n")
	}
	if i.Author != "" {
		sb.WriteString("<dc:creator><rdf:Seq><rdf:li>" + esc(i.Author) + "</rdf:li></rdf:Seq></dc:creator>\n")
	}
	if i.Subject != "" {
		sb.WriteString("<dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">" + esc(i.Subject) + "</rdf:li></rdf:Alt></dc:description>\n")
	}
	if i.Keywords != "" {
		sb.WriteString("<pdf:Keywords>" + esc(i.Keywords) + "</pdf:Keywords>\n")
	}
	sb.WriteString("<pdf:Producer>" + esc(i.producer()) + "</pdf:Producer>\n")
	if !i.CreationDate.IsZero() {
		sb.WriteString("<xmp:CreateDate>" + xmpDate(i.CreationDate) + "</xmp:CreateDate>\n")
	}
	if !i.ModDate.IsZero() {
		sb.WriteString("<xmp:ModDate>" + xmpDate(i.ModDate) + "</xmp:ModDate>\n")
	}
	sb.WriteString("</rdf:Description>\n</rdf:RDF>\n</x:xmpmeta>\n<?xpacket end=\"w\"?>\n")
	return []byte(sb.String())
}
