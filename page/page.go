// Package page 实现 PDF 页面：标准尺寸、盒模型与画布高级绘图 API。
//
// Page 聚合内容流构建器与页面资源（字体、图像、扩展图形状态、渐变），
// 提供文字、图形、渐变、透明度、图像与批注的一站式绘制方法。
package page

import (
	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/content"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/image"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// Size 页面尺寸（单位：磅，1/72 英寸）。
type Size struct{ W, H float64 }

// 标准页面尺寸。
var (
	A0      = Size{2383.94, 3370.39}
	A1      = Size{1683.78, 2383.94}
	A2      = Size{1190.55, 1683.78}
	A3      = Size{841.89, 1190.55}
	A4      = Size{595.28, 841.89}
	A5      = Size{419.53, 595.28}
	A6      = Size{297.64, 419.53}
	B4      = Size{708.66, 1000.63}
	B5      = Size{498.90, 708.66}
	Letter  = Size{612, 792}
	Legal   = Size{612, 1008}
	Tabloid = Size{792, 1224}
)

// Landscape 返回横向尺寸。
func (s Size) Landscape() Size {
	if s.W > s.H {
		return s
	}
	return Size{s.H, s.W}
}

// Portrait 返回纵向尺寸。
func (s Size) Portrait() Size {
	if s.W < s.H {
		return s
	}
	return Size{s.H, s.W}
}

// Point 二维点。
type Point struct{ X, Y float64 }

// bezierCircleKappa 四次贝塞尔拟合圆的系数。
const bezierCircleKappa = 0.5522847498307936

// Page PDF 页面（画布）。
type Page struct {
	Size    Size
	Rotate  int // 页面旋转角度：0/90/180/270
	Content *content.Builder

	fonts     map[string]font.Resource
	fontRev   map[font.Resource]string
	images    map[string]*image.Image
	imageRev  map[*image.Image]string
	gstates   map[string]*object.Dict
	gstateKy  map[string]string
	shadings  map[string]*object.Dict
	annots    []annot.Annotation
	annotRefs []object.Ref
	seq       int
}

// New 创建页面。
func New(s Size) *Page {
	return &Page{
		Size:     s,
		Content:  content.New(),
		fonts:    map[string]font.Resource{},
		fontRev:  map[font.Resource]string{},
		images:   map[string]*image.Image{},
		imageRev: map[*image.Image]string{},
		gstates:  map[string]*object.Dict{},
		gstateKy: map[string]string{},
		shadings: map[string]*object.Dict{},
	}
}

func (p *Page) nextName(prefix string) string {
	p.seq++
	return prefix + itoa(p.seq)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// --- 资源登记（供序列化使用） ---

// Fonts 返回页面使用的字体资源（名称 → 字体）。
func (p *Page) Fonts() map[string]font.Resource { return p.fonts }

// Images 返回页面使用的图像资源。
func (p *Page) Images() map[string]*image.Image { return p.images }

// ExtGStates 返回页面使用的扩展图形状态资源。
func (p *Page) ExtGStates() map[string]*object.Dict { return p.gstates }

// Shadings 返回页面使用的渐变资源。
func (p *Page) Shadings() map[string]*object.Dict { return p.shadings }

// Annotations 返回页面批注。
func (p *Page) Annotations() []annot.Annotation { return p.annots }

// AnnotationRefs 返回以间接引用形式登记的批注（表单部件等）。
func (p *Page) AnnotationRefs() []object.Ref { return p.annotRefs }

// AddAnnotationRef 登记间接批注引用，供文档序列化时加入页面 /Annots。
func (p *Page) AddAnnotationRef(r object.Ref) { p.annotRefs = append(p.annotRefs, r) }

func (p *Page) useFont(f font.Resource) string {
	if name, ok := p.fontRev[f]; ok {
		return name
	}
	name := p.nextName("F")
	p.fonts[name] = f
	p.fontRev[f] = name
	return name
}

// --- 文本 ---

// DrawText 在 (x, y) 处绘制一行文本（基线坐标）。
// 字体实现 font.Kerned（如带 kern 表的 CJKFont）时自动以 TJ 数组应用字距。
func (p *Page) DrawText(f font.Resource, size, x, y float64, s string) {
	c := p.Content.BeginText().
		SetFont(p.useFont(f), size).
		TextPosition(x, y)
	if k, ok := f.(font.Kerned); ok {
		if segs := k.EncodeKerned(s); segs != nil {
			c.ShowTextAdjusted(segs...).EndText()
			return
		}
	}
	c.ShowText(f.Encode(s)).EndText()
}

// TextBox 在 (x, yTop) 起、宽度为 w 的区域内自动换行排版文本，
// 自上而下逐行绘制，返回占用的高度。leading 为行距，<=0 时取字体建议行高。
func (p *Page) TextBox(f font.Resource, size, x, yTop, w float64, s string, align text.Alignment, leading float64) float64 {
	lines := text.Wrap(f, size, w, s)
	if leading <= 0 {
		leading = f.LineHeight(size)
	}
	y := yTop - f.Ascent(size)
	for i, line := range lines {
		xoff := text.OffsetX(f, size, w, line, align)
		c := p.Content.BeginText().SetFont(p.useFont(f), size)
		if align == text.AlignJustify && i < len(lines)-1 {
			c.WordSpace(text.JustifyWordSpace(f, size, w, line))
		}
		c.TextPosition(x+xoff, y)
		if k, ok := f.(font.Kerned); ok {
			if segs := k.EncodeKerned(line); segs != nil {
				c.ShowTextAdjusted(segs...).EndText()
				y -= leading
				continue
			}
		}
		c.ShowText(f.Encode(line)).EndText()
		y -= leading
	}
	return f.Ascent(size) - f.Descent(size) + leading*float64(len(lines)-1)
}

// Underline 为单行文本加下划线（根据字体度量自动定位）。
func (p *Page) Underline(f font.Resource, size, x, y float64, s string) {
	thick := size * 0.05
	p.Content.SaveState().LineWidth(thick).
		MoveTo(x, y-size*0.1).LineTo(x+f.TextWidth(s, size), y-size*0.1).Stroke().
		RestoreState()
}

// StrikeThrough 为单行文本加删除线。
func (p *Page) StrikeThrough(f font.Resource, size, x, y float64, s string) {
	thick := size * 0.05
	p.Content.SaveState().LineWidth(thick).
		MoveTo(x, y+f.CapHeight(size)*0.5).LineTo(x+f.TextWidth(s, size), y+f.CapHeight(size)*0.5).Stroke().
		RestoreState()
}

// --- 图形状态便捷方法 ---

// Save 保存图形状态（配合 Restore 使用）。
func (p *Page) Save() *Page { p.Content.SaveState(); return p }

// Restore 恢复图形状态。
func (p *Page) Restore() *Page { p.Content.RestoreState(); return p }

// Translate 平移坐标系。
func (p *Page) Translate(x, y float64) *Page { p.Content.Translate(x, y); return p }

// Scale 缩放坐标系。
func (p *Page) Scale(sx, sy float64) *Page { p.Content.Scale(sx, sy); return p }

// RotateCanvas 旋转坐标系（角度制）。
func (p *Page) RotateCanvas(deg float64) *Page { p.Content.Rotate(deg); return p }

// SetFillColor 设置填充色。
func (p *Page) SetFillColor(c color.Color) *Page { p.Content.SetFillColor(c); return p }

// SetStrokeColor 设置描边色。
func (p *Page) SetStrokeColor(c color.Color) *Page { p.Content.SetStrokeColor(c); return p }

// SetLineWidth 设置线宽。
func (p *Page) SetLineWidth(w float64) *Page { p.Content.LineWidth(w); return p }

// SetLineCap 设置线帽。
func (p *Page) SetLineCap(c content.LineCap) *Page { p.Content.SetLineCap(c); return p }

// SetLineJoin 设置连接样式。
func (p *Page) SetLineJoin(j content.LineJoin) *Page { p.Content.SetLineJoin(j); return p }

// SetDash 设置虚线样式；空 pattern 恢复实线。
func (p *Page) SetDash(pattern []float64, phase float64) *Page {
	p.Content.Dash(pattern, phase)
	return p
}

// SetAlpha 设置填充与描边透明度（0–1），通过扩展图形状态实现。
func (p *Page) SetAlpha(fill, stroke float64) *Page {
	d := object.NewDict().Set("Type", object.Name("ExtGState")).
		Set("ca", object.Real(fill)).Set("CA", object.Real(stroke))
	p.Content.SetExtGState(p.registerGState(d))
	return p
}

// SetBlendMode 设置混合模式（Multiply / Screen / Overlay / Darken / Lighten /
// ColorDodge / ColorBurn / HardLight / SoftLight / Difference / Exclusion ...）。
func (p *Page) SetBlendMode(mode string) *Page {
	d := object.NewDict().Set("Type", object.Name("ExtGState")).Set("BM", object.Name(mode))
	p.Content.SetExtGState(p.registerGState(d))
	return p
}

func (p *Page) registerGState(d *object.Dict) string {
	key := string(d.Encode(nil))
	if name, ok := p.gstateKy[key]; ok {
		return name
	}
	name := p.nextName("GS")
	p.gstates[name] = d
	p.gstateKy[key] = name
	return name
}

// --- 形状 ---

// Line 绘制线段。
func (p *Page) Line(x1, y1, x2, y2 float64) {
	p.Content.MoveTo(x1, y1).LineTo(x2, y2).Stroke()
}

// StrokeRect 描边矩形。
func (p *Page) StrokeRect(x, y, w, h float64) { p.Content.Rect(x, y, w, h).Stroke() }

// FillRect 填充矩形。
func (p *Page) FillRect(x, y, w, h float64) { p.Content.Rect(x, y, w, h).Fill() }

// FillStrokeRect 填充并描边矩形。
func (p *Page) FillStrokeRect(x, y, w, h float64) { p.Content.Rect(x, y, w, h).FillStroke() }

// RoundRectPath 构造圆角矩形路径（不绘制）。
func (p *Page) RoundRectPath(x, y, w, h, r float64) *Page {
	k := bezierCircleKappa
	c := p.Content
	c.MoveTo(x+r, y)
	c.LineTo(x+w-r, y)
	c.CurveTo(x+w-r+r*k, y, x+w, y+r-r*k, x+w, y+r)
	c.LineTo(x+w, y+h-r)
	c.CurveTo(x+w, y+h-r+r*k, x+w-r+r*k, y+h, x+w-r, y+h)
	c.LineTo(x+r, y+h)
	c.CurveTo(x+r-r*k, y+h, x, y+h-r+r*k, x, y+h-r)
	c.LineTo(x, y+r)
	c.CurveTo(x, y+r-r*k, x+r-r*k, y, x+r, y)
	c.ClosePath()
	return p
}

// StrokeRoundRect 描边圆角矩形。
func (p *Page) StrokeRoundRect(x, y, w, h, r float64) {
	p.RoundRectPath(x, y, w, h, r)
	p.Content.Stroke()
}

// FillRoundRect 填充圆角矩形。
func (p *Page) FillRoundRect(x, y, w, h, r float64) { p.RoundRectPath(x, y, w, h, r); p.Content.Fill() }

// CirclePath 构造圆形路径（不绘制）。
func (p *Page) CirclePath(cx, cy, r float64) *Page {
	k := bezierCircleKappa * r
	c := p.Content
	c.MoveTo(cx+r, cy)
	c.CurveTo(cx+r, cy+k, cx+k, cy+r, cx, cy+r)
	c.CurveTo(cx-k, cy+r, cx-r, cy+k, cx-r, cy)
	c.CurveTo(cx-r, cy-k, cx-k, cy-r, cx, cy-r)
	c.CurveTo(cx+k, cy-r, cx+r, cy-k, cx+r, cy)
	c.ClosePath()
	return p
}

// StrokeCircle 描边圆。
func (p *Page) StrokeCircle(cx, cy, r float64) { p.CirclePath(cx, cy, r); p.Content.Stroke() }

// FillCircle 填充圆。
func (p *Page) FillCircle(cx, cy, r float64) { p.CirclePath(cx, cy, r); p.Content.Fill() }

// EllipsePath 构造椭圆路径（不绘制）。
func (p *Page) EllipsePath(cx, cy, rx, ry float64) *Page {
	kx, ky := bezierCircleKappa*rx, bezierCircleKappa*ry
	c := p.Content
	c.MoveTo(cx+rx, cy)
	c.CurveTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	c.CurveTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	c.CurveTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	c.CurveTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	c.ClosePath()
	return p
}

// StrokeEllipse 描边椭圆。
func (p *Page) StrokeEllipse(cx, cy, rx, ry float64) {
	p.EllipsePath(cx, cy, rx, ry)
	p.Content.Stroke()
}

// FillEllipse 填充椭圆。
func (p *Page) FillEllipse(cx, cy, rx, ry float64) { p.EllipsePath(cx, cy, rx, ry); p.Content.Fill() }

// Polygon 折线/多边形路径；closePath 为 true 时闭合。
func (p *Page) Polygon(pts []Point, closePath bool) *Page {
	if len(pts) == 0 {
		return p
	}
	c := p.Content.MoveTo(pts[0].X, pts[0].Y)
	for _, pt := range pts[1:] {
		c.LineTo(pt.X, pt.Y)
	}
	if closePath {
		c.ClosePath()
	}
	return p
}

// StrokePolygon 描边多边形。
func (p *Page) StrokePolygon(pts []Point) { p.Polygon(pts, true); p.Content.Stroke() }

// FillPolygon 填充多边形。
func (p *Page) FillPolygon(pts []Point) { p.Polygon(pts, true); p.Content.Fill() }

// --- 渐变 ---

// FillAxialGradient 以轴向渐变填充矩形区域。
// horizontal 为 true 时从左到右，否则从下到上。
func (p *Page) FillAxialGradient(x, y, w, h float64, c1, c2 color.Color, horizontal bool) {
	var coords object.Array
	if horizontal {
		coords = object.Array{object.Real(x), object.Real(y), object.Real(x + w), object.Real(y)}
	} else {
		coords = object.Array{object.Real(x), object.Real(y), object.Real(x), object.Real(y + h)}
	}
	name := p.registerShading(2, coords, c1, c2)
	p.Content.SaveState().Rect(x, y, w, h).Clip().EndPath().PaintShading(name).RestoreState()
}

// FillRadialGradient 以径向渐变填充圆形区域（从中心 c1 到边缘 c2）。
func (p *Page) FillRadialGradient(cx, cy, r float64, c1, c2 color.Color) {
	coords := object.Array{object.Real(cx), object.Real(cy), object.Real(0),
		object.Real(cx), object.Real(cy), object.Real(r)}
	name := p.registerShading(3, coords, c1, c2)
	p.Content.SaveState()
	p.CirclePath(cx, cy, r)
	p.Content.Clip().EndPath().PaintShading(name).RestoreState()
}

func (p *Page) registerShading(typ int, coords object.Array, c1, c2 color.Color) string {
	// 渐变两端须处于同一色彩空间：统一转为 RGB
	r1, r2 := color.ToRGB(c1), color.ToRGB(c2)
	comps := func(c color.Color) object.Array {
		cs := c.Components()
		arr := make(object.Array, len(cs))
		for i, v := range cs {
			arr[i] = object.Real(v)
		}
		return arr
	}
	fn := object.NewDict().
		Set("FunctionType", object.Int(2)).
		Set("Domain", object.Array{object.Real(0), object.Real(1)}).
		Set("C0", comps(r1)).
		Set("C1", comps(r2)).
		Set("N", object.Real(1))
	d := object.NewDict().
		Set("ShadingType", object.Int(typ)).
		Set("ColorSpace", object.Name(r1.Space())).
		Set("Coords", coords).
		Set("Function", fn).
		Set("Extend", object.Array{object.Bool(true), object.Bool(true)})
	name := p.nextName("Sh")
	p.shadings[name] = d
	return name
}

// --- 图像 ---

// DrawImage 以指定位置与尺寸绘制图像。
func (p *Page) DrawImage(im *image.Image, x, y, w, h float64) {
	p.Content.DrawImage(p.useImage(im), x, y, w, h)
}

// DrawImageNatural 以原始像素尺寸（1 像素 = 1 磅）绘制图像。
func (p *Page) DrawImageNatural(im *image.Image, x, y float64) {
	p.DrawImage(im, x, y, float64(im.Width), float64(im.Height))
}

func (p *Page) useImage(im *image.Image) string {
	if name, ok := p.imageRev[im]; ok {
		return name
	}
	name := p.nextName("Im")
	p.images[name] = im
	p.imageRev[im] = name
	return name
}

// --- 批注 ---

// AddAnnotation 添加批注。
func (p *Page) AddAnnotation(a annot.Annotation) { p.annots = append(p.annots, a) }

// AddURILink 添加 URI 链接热区。
func (p *Page) AddURILink(rect [4]float64, uri string) {
	p.AddAnnotation(annot.LinkURI{Base: annot.Base{Rect: rect}, URI: uri})
}

// AddPageLink 添加页内跳转链接热区。
func (p *Page) AddPageLink(rect [4]float64, dest annot.Destination) {
	p.AddAnnotation(annot.LinkGoTo{Base: annot.Base{Rect: rect}, Dest: dest})
}
