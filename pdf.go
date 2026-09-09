// Package pdf 是 tenon-plugin-pdf 的门面包：聚合页面、大纲、表单、
// 元数据等组件，将文档序列化为完整 PDF 文件。
//
// 用法示例：
//
//	doc := pdf.New()
//	doc.Info().Title = "示例"
//	p := doc.AddPage(page.A4)
//	p.DrawText(font.Helvetica, 24, 72, 720, "Hello PDF")
//	err := doc.SaveFile("out.pdf")
package pdf

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/form"
	"gopkg.761sama.com/tenon-plugin-pdf/image"
	"gopkg.761sama.com/tenon-plugin-pdf/metadata"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/outline"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/security"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// PageLayout 页面布局（/PageLayout）。
type PageLayout string

const (
	LayoutSinglePage     PageLayout = "SinglePage"     // 单页
	LayoutOneColumn      PageLayout = "OneColumn"      // 单列连续
	LayoutTwoColumnLeft  PageLayout = "TwoColumnLeft"  // 双列（奇数页在左）
	LayoutTwoColumnRight PageLayout = "TwoColumnRight" // 双列（奇数页在右）
	LayoutTwoPageLeft    PageLayout = "TwoPageLeft"    // 双页（奇数页在左）
	LayoutTwoPageRight   PageLayout = "TwoPageRight"   // 双页（奇数页在右）
)

// PageMode 页面模式（/PageMode）。
type PageMode string

const (
	ModeUseNone     PageMode = "UseNone"     // 默认
	ModeUseOutlines PageMode = "UseOutlines" // 显示书签面板
	ModeUseThumbs   PageMode = "UseThumbs"   // 显示缩略图
	ModeFullScreen  PageMode = "FullScreen"  // 全屏
	ModeUseOC       PageMode = "UseOC"       // 显示可选内容面板
)

// Document PDF 文档。
type Document struct {
	pages    []*page.Page
	info     metadata.Info
	outline  outline.Outline
	fields   []form.Field
	compress bool
	layout   PageLayout
	mode     PageMode

	encrypt    *security.Options
	pubkey     *security.PubKeyOptions
	signature  []*sign.Field // 签名字段（可多个，按添加顺序依次签署）
	dss        *DSSData      // LTV：DSS 验证材料（证书链 + CRL/OCSP）
}

// New 创建空文档（默认压缩内容流）。
func New() *Document {
	return &Document{compress: true}
}

// SetCompress 设置是否压缩内容流（默认开启）。
func (d *Document) SetCompress(on bool) { d.compress = on }

// SetEncryption 启用标准安全处理器加密（用户/所有者密码 + 权限位 + AES-128/AES-256）。
// 与 SetPubKeyEncryption 互斥，同时设置时 WriteTo 报错。
func (d *Document) SetEncryption(opts security.Options) { d.encrypt = &opts }

// SetPubKeyEncryption 启用公钥证书加密（PubSec，SubFilter adbe.pkcs7.s5）：
// 文件密钥用各收件人证书的 RSA 公钥封装（CMS EnvelopedData），
// 任一收件人持对应私钥即可解密；支持多收件人（Recipients 数组）。
// 与 SetEncryption 互斥，同时设置时 WriteTo 报错。
func (d *Document) SetPubKeyEncryption(opts security.PubKeyOptions) { d.pubkey = &opts }

// SetSignature 为文档添加数字签名字段（含占位符），替换既有签名设置。
// 序列化后需调用 sign.Sign 回填签名值与 /ByteRange。
// 多重签名（依次多人会签）请使用 AddSignature。
func (d *Document) SetSignature(f *sign.Field) { d.signature = []*sign.Field{f} }

// DSSData 是 LTV（长期验证，ISO 32000-2 §12.8.4.3）的验证材料，
// 通过 SetDSS 嵌入文档 Catalog 的 /DSS 字典，支持离线长期验证。
type DSSData struct {
	Certs []*x509.Certificate // 签名证书链（中间 CA / 根 CA 等验证用证书）
	CRLs  [][]byte            // DER 编码的证书吊销列表
	OCSPs [][]byte            // DER 编码的 OCSP 响应
}

// SetDSS 在文档 Catalog 中嵌入 DSS 字典（Document Security Store）：
// 证书、CRL、OCSP 响应各存为独立间接流，供验证方离线完成
// 证书链构建与吊销状态检查（LTV 的验证材料部分）。
//
// 典型流程：签署完成后从 CA 获取证书链与 CRL/OCSP 响应，重新装配
// 或经增量修订写入。本库在生成期嵌入（与签名同一修订），阅读器
// 同样可识别；严格 PAdES 流程可自行用 sign.AppendSignatureField
// 风格的增量段追加。
func (d *Document) SetDSS(dss DSSData) { d.dss = &dss }

// AddSignature 追加一个签名字段声明（多重签名/会签场景）。
// 生成期仅第一个字段携带签名占位符（由 sign.Sign 原地回填）；
// 其余字段先生成未签名的空字段，第 2..N 次签署通过
// sign.AppendSignatureField 以增量修订追加新字段占位符后签署——
// 同一修订内的多个占位符无法顺序签署（后签名会改变前签名覆盖区内的字节）。
func (d *Document) AddSignature(f *sign.Field) { d.signature = append(d.signature, f) }

// Info 返回文档信息（直接修改字段即可）。
func (d *Document) Info() *metadata.Info { return &d.info }

// Outline 返回书签大纲。
func (d *Document) Outline() *outline.Outline { return &d.outline }

// SetPageLayout 设置页面布局。
func (d *Document) SetPageLayout(l PageLayout) { d.layout = l }

// SetPageMode 设置页面模式。
func (d *Document) SetPageMode(m PageMode) { d.mode = m }

// AddPage 添加页面并返回画布。
func (d *Document) AddPage(s page.Size) *page.Page {
	p := page.New(s)
	d.pages = append(d.pages, p)
	return p
}

// PageCount 返回页数。
func (d *Document) PageCount() int { return len(d.pages) }

// AddField 添加交互表单字段。
func (d *Document) AddField(f form.Field) { d.fields = append(d.fields, f) }

// SaveFile 将文档写入文件。
func (d *Document) SaveFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = d.WriteTo(f)
	return err
}

// Bytes 将文档序列化为字节切片。
func (d *Document) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := d.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteTo 将文档序列化写入 out。
func (d *Document) WriteTo(out io.Writer) (int64, error) {
	if len(d.pages) == 0 {
		return 0, fmt.Errorf("pdf: 文档至少需要一个页面")
	}
	w := writer.New()

	// 0. 加密：标准处理器的文件 ID 参与密钥派生，须先行生成；
	//    公钥处理器不依赖文件 ID，直接构建。
	if d.encrypt != nil && d.pubkey != nil {
		return 0, fmt.Errorf("pdf: 标准密码加密与公钥证书加密互斥，只能启用其一")
	}
	var fileID []byte
	if d.encrypt != nil {
		fileID = make([]byte, 16)
		if _, err := rand.Read(fileID); err != nil {
			return 0, err
		}
		h, err := security.NewHandler(*d.encrypt, fileID)
		if err != nil {
			return 0, err
		}
		w.SetEncrypt(w.Add(h.Dict()), h)
	}
	if d.pubkey != nil {
		h, err := security.NewPubKeyHandler(*d.pubkey)
		if err != nil {
			return 0, err
		}
		w.SetEncrypt(w.Add(h.Dict()), h)
	}

	// 1. 预留页树与所有页面引用
	pagesRef := w.Alloc()
	pageRefs := make([]object.Ref, len(d.pages))
	pageRefOf := make(map[*page.Page]object.Ref, len(d.pages))
	for i, p := range d.pages {
		pageRefs[i] = w.Alloc()
		pageRefOf[p] = pageRefs[i]
	}

	// 2. 图像去重登记（SMask 作为独立间接对象）
	imageRefs := make(map[*image.Image]object.Ref)
	registerImage := func(im *image.Image) object.Ref {
		if r, ok := imageRefs[im]; ok {
			return r
		}
		var smask object.Object
		if im.SMask != nil {
			smaskRef := w.Add(im.SMask.Stream(nil))
			smask = smaskRef
		}
		r := w.Add(im.Stream(smask))
		imageRefs[im] = r
		return r
	}
	// 预扫描登记所有页面图像，保证多页共享
	for _, p := range d.pages {
		for _, im := range p.Images() {
			registerImage(im)
		}
	}

	// 3. 跳转目标解析器
	resolver := func(dest annot.Destination) object.Object {
		if dest.PageIndex < 0 || dest.PageIndex >= len(d.pages) {
			dest.PageIndex = 0
		}
		top := dest.Top
		if top != top { // NaN → 页顶
			top = d.pages[dest.PageIndex].Size.H
		}
		return object.Array{pageRefs[dest.PageIndex], object.Name("XYZ"),
			object.Null, object.Real(top), object.Null}
	}

	// 4. 表单（在页面序列化前构建，部件引用会登记到页面）
	var acroFormRef object.Ref

	// 4.1 数字签名字段（多重签名/会签）：
	// 第一个字段写入带 /V 占位符的签名值字典（间接对象，sign.Sign 两遍回填）；
	// 其余字段生成无 /V 的未签名空字段——后续签署用 sign.AppendSignatureField
	// 追加增量修订（不改动既有字节），保证前序签名的覆盖区不变。
	var sigRefs []object.Ref
	for sigIdx, s := range d.signature {
		pageIdx := s.PageIndex
		if pageIdx < 0 || pageIdx >= len(d.pages) {
			pageIdx = 0
		}
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Signature%d", sigIdx+1)
		}
		widget := object.NewDict()
		widget.Set("Type", object.Name("Annot"))
		widget.Set("Subtype", object.Name("Widget"))
		widget.Set("FT", object.Name("Sig"))
		widget.Set("T", object.TextStr(name))
		widget.Set("Rect", object.Rect(s.Rect[0], s.Rect[1], s.Rect[2], s.Rect[3]))
		if s.Visible() {
			// 可见签名：打印标志 + 正常外观流（/AP /N）
			widget.Set("F", object.Int(4)) // Print
			widget.Set("AP", object.NewDict().Set("N", buildSigAppearance(w, s, s.Rect)))
		} else {
			widget.Set("F", object.Int(2)) // Hidden（不可见签名）
		}
		widget.Set("P", pageRefs[pageIdx])
		if sigIdx == 0 {
			v := object.NewDict()
			v.Set("Type", object.Name("Sig"))
			v.Set("Filter", object.Name("Adobe.PPKLite"))
			v.Set("SubFilter", object.Name("adbe.pkcs7.detached"))
			v.Set("ByteRange", object.Raw(sign.ByteRangePlaceholder))
			v.Set("Contents", object.Raw(sign.ContentsPlaceholder()))
			v.Set("M", object.Str(metadata.FormatDate(time.Now())))
			if s.Reason != "" {
				v.Set("Reason", object.TextStr(s.Reason))
			}
			if s.Location != "" {
				v.Set("Location", object.TextStr(s.Location))
			}
			if s.Contact != "" {
				v.Set("ContactInfo", object.TextStr(s.Contact))
			}
			// /V 为间接引用（部分验证器如 pyhanko 不接受内联签名值字典）
			widget.Set("V", w.Add(v))
		}
		sigRef := w.Add(widget)
		sigRefs = append(sigRefs, sigRef)
		d.pages[pageIdx].AddAnnotationRef(sigRef)
	}

	if len(d.fields) > 0 || len(sigRefs) > 0 {
		fonts := form.FormFonts{
			Helv: w.Add(font.Helvetica.Dict()),
			ZaDb: w.Add(font.ZapfDingbats.Dict()),
		}
		acroFormRef = form.Build(w, d.fields, fonts, func(p *page.Page) object.Ref {
			if r, ok := pageRefOf[p]; ok {
				return r
			}
			return pageRefs[0]
		}, sigRefs...)
	}

	// 4.5 字体字典缓存：同一字体资源跨页共享同一间接对象
	fontRefs := map[font.Resource]object.Ref{}
	buildFont := func(f font.Resource) object.Ref {
		if r, ok := fontRefs[f]; ok {
			return r
		}
		r := w.Add(f.BuildDict(w))
		fontRefs[f] = r
		return r
	}

	// 5. 逐页序列化
	for i, p := range d.pages {
		w.Set(pageRefs[i], d.buildPage(w, p, pagesRef, registerImage, resolver, buildFont))
	}

	// 6. 页树
	kids := make(object.Array, len(pageRefs))
	for i, r := range pageRefs {
		kids[i] = r
	}
	w.Set(pagesRef, object.NewDict().
		Set("Type", object.Name("Pages")).
		Set("Kids", kids).
		Set("Count", object.Int(len(pageRefs))))

	// 7. 大纲
	var outlineRef object.Ref
	if !d.outline.Empty() {
		outlineRef = d.outline.Build(w, resolver)
	}

	// 8. XMP 元数据
	var metadataRef object.Ref
	if !d.info.Empty() {
		st := object.NewStream(d.info.XMP())
		st.Dict.Set("Type", object.Name("Metadata"))
		st.Dict.Set("Subtype", object.Name("XML"))
		metadataRef = w.Add(st)
	}

	// 9. 文档目录
	catalog := object.NewDict()
	catalog.Set("Type", object.Name("Catalog"))
	catalog.Set("Pages", pagesRef)
	if outlineRef.Num != 0 {
		catalog.Set("Outlines", outlineRef)
		if d.mode == "" {
			catalog.Set("PageMode", object.Name("UseOutlines"))
		}
	}
	if d.mode != "" {
		catalog.Set("PageMode", object.Name(d.mode))
	}
	if d.layout != "" {
		catalog.Set("PageLayout", object.Name(d.layout))
	}
	if metadataRef.Num != 0 {
		catalog.Set("Metadata", metadataRef)
	}
	if acroFormRef.Num != 0 {
		catalog.Set("AcroForm", acroFormRef)
	}
	// DSS（LTV 验证材料）：证书/CRL/OCSP 各为独立间接流
	if d.dss != nil && (len(d.dss.Certs) > 0 || len(d.dss.CRLs) > 0 || len(d.dss.OCSPs) > 0) {
		dss := object.NewDict()
		dss.Set("Type", object.Name("DSS"))
		if len(d.dss.Certs) > 0 {
			refs := make(object.Array, 0, len(d.dss.Certs))
			for _, c := range d.dss.Certs {
				if c != nil {
					refs = append(refs, w.Add(object.NewStream(c.Raw)))
				}
			}
			dss.Set("Certs", refs)
		}
		if len(d.dss.CRLs) > 0 {
			refs := make(object.Array, 0, len(d.dss.CRLs))
			for _, der := range d.dss.CRLs {
				refs = append(refs, w.Add(object.NewStream(der)))
			}
			dss.Set("CRLs", refs)
		}
		if len(d.dss.OCSPs) > 0 {
			refs := make(object.Array, 0, len(d.dss.OCSPs))
			for _, der := range d.dss.OCSPs {
				refs = append(refs, w.Add(object.NewStream(der)))
			}
			dss.Set("OCSPs", refs)
		}
		catalog.Set("DSS", w.Add(dss))
	}
	catalogRef := w.Add(catalog)

	// 10. 信息字典
	infoRef := w.Add(d.info.Dict())

	return w.WriteTo(out, catalogRef, infoRef, fileID)
}

// buildPage 序列化单个页面对象。
func (d *Document) buildPage(w *writer.Writer, p *page.Page, pagesRef object.Ref,
	registerImage func(*image.Image) object.Ref, resolver annot.Resolver,
	buildFont func(font.Resource) object.Ref) *object.Dict {

	// 资源字典
	res := object.NewDict()
	if len(p.Fonts()) > 0 {
		fd := object.NewDict()
		for name, f := range p.Fonts() {
			fd.Set(name, buildFont(f))
		}
		res.Set("Font", fd)
	}
	if len(p.Images()) > 0 {
		xd := object.NewDict()
		for name, im := range p.Images() {
			xd.Set(name, registerImage(im))
		}
		res.Set("XObject", xd)
	}
	if len(p.ExtGStates()) > 0 {
		gd := object.NewDict()
		for name, gs := range p.ExtGStates() {
			gd.Set(name, gs)
		}
		res.Set("ExtGState", gd)
	}
	if len(p.Shadings()) > 0 {
		sd := object.NewDict()
		for name, sh := range p.Shadings() {
			sd.Set(name, sh)
		}
		res.Set("Shading", sd)
	}

	// 内容流
	data := p.Content.Bytes()
	contentStream := object.NewStream(data)
	if d.compress {
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		zw.Write(data)
		zw.Close()
		contentStream = object.NewStream(buf.Bytes())
		contentStream.Dict.Set("Filter", object.Name("FlateDecode"))
	}
	contentRef := w.Add(contentStream)

	// 批注
	var annots object.Array
	for _, a := range p.Annotations() {
		annots = append(annots, w.Add(a.Dict(resolver)))
	}
	for _, r := range p.AnnotationRefs() {
		annots = append(annots, r)
	}

	dict := object.NewDict()
	dict.Set("Type", object.Name("Page"))
	dict.Set("Parent", pagesRef)
	dict.Set("MediaBox", object.Rect(0, 0, p.Size.W, p.Size.H))
	if p.Rotate != 0 {
		dict.Set("Rotate", object.Int(p.Rotate))
	}
	dict.Set("Resources", res)
	dict.Set("Contents", contentRef)
	if len(annots) > 0 {
		dict.Set("Annots", annots)
	}
	return dict
}
