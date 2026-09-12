# tenon-plugin-pdf

Go 语言 PDF 生成库：**纯标准库实现、零第三方依赖**，输出 PDF 1.7。
既可作为库被其他项目导入，也提供 `tenon-pdf` 命令行工具独立使用。

模块路径：`gopkg.761sama.com/tenon-plugin-pdf`（根包名 `pdf`）。

## 功能特性

按包划分，每个组件单一职责：

| 包 | 功能 |
|---|---|
| `object` | PDF 对象模型（null/布尔/数值/字符串/名称/数组/字典/流/引用）与序列化 |
| `writer` | 文件结构：间接对象、交叉引用表、trailer、文件头尾 |
| `color` | DeviceGray / DeviceRGB / DeviceCMYK 颜色模型 |
| `font` | 标准 14 字体 + WinAnsi 编码 + 精确字宽表；`CJKFont` 中文嵌入字体（Type0/Identity-H，支持 TTC、连字、字距） |
| `ttf` | TrueType（glyf）字体与 TTC 集合解析、子集化重建（复合字形闭包、校验和修正）、kern/GSUB liga 读取 |
| `content` | 内容流构建器：路径、绘制、裁剪、变换、全部文本操作、XObject |
| `text` | 文本换行与对齐（左/中/右/两端对齐） |
| `image` | JPEG 原样嵌入（DCTDecode）、PNG/GIF 解码嵌入（FlateDecode）、SMask 透明蒙版 |
| `page` | 页面尺寸（A 系列/B5/Letter/Legal…）与画布 API：形状、渐变、透明度、混合模式 |
| `annot` | 批注：URI 链接、页内跳转、便签、高亮/下划线/删除线/波浪线 |
| `outline` | 书签大纲（多级、加粗/斜体/颜色、折叠） |
| `metadata` | 文档信息字典 + XMP 元数据流 |
| `form` | AcroForm 交互表单：文本域、复选框 |
| `table` | 表格：定宽/均分/内容自适应列宽、灰底加粗表头、单元格合并（跨列/跨行，含表头行）、边框内边距、跨页自动重复表头、超高行/跨行块跨页拆分 |
| `security` | 标准安全处理器：用户/所有者密码、权限位、AES-128（R4）/AES-256（R6）；公钥证书加密（PubSec，多收件人、每收件人独立权限） |
| `sign` | PKCS#7/CMS 数字签名：ByteRange 回填、RSA/ECDSA、RFC 3161 时间戳、证书链验证、多重签名（增量修订）、第三方既有 PDF 签署、验签、自签名/链式证书 |

## 安装与导入

```sh
go get gopkg.761sama.com/tenon-plugin-pdf
```

```go
import (
    "gopkg.761sama.com/tenon-plugin-pdf"      // 门面包：pdf.Document
    "gopkg.761sama.com/tenon-plugin-pdf/page" // 页面尺寸与画布
    "gopkg.761sama.com/tenon-plugin-pdf/font" // 字体
)
```

## 库 API 快速上手

### 普通文字与图形

```go
package main

import (
    "gopkg.761sama.com/tenon-plugin-pdf"
    "gopkg.761sama.com/tenon-plugin-pdf/annot"
    "gopkg.761sama.com/tenon-plugin-pdf/color"
    "gopkg.761sama.com/tenon-plugin-pdf/font"
    "gopkg.761sama.com/tenon-plugin-pdf/page"
    "gopkg.761sama.com/tenon-plugin-pdf/text"
)

func main() {
    doc := pdf.New()
    doc.Info().Title = "报告"
    doc.Info().Author = "张三"

    p := doc.AddPage(page.A4)
    p.DrawText(font.HelveticaBold, 24, 72, 760, "Hello PDF")
    p.SetFillColor(color.RGB{R: 0.2, G: 0.4, B: 0.8})
    p.TextBox(font.TimesRoman, 11, 72, 730, 450,
        "Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
        text.AlignJustify, 0)
    p.SetFillColor(color.Red)
    p.FillCircle(300, 500, 40)
    p.FillAxialGradient(72, 300, 200, 80, color.Yellow, color.Blue, true)

    doc.Outline().Add("首页", annot.Destination{PageIndex: 0})
    if err := doc.SaveFile("out.pdf"); err != nil {
        panic(err)
    }
}
```

### 表格（合同/采购单）

```go
tbl := table.New(
    table.Column{Title: "No.", Width: 36, Align: text.AlignCenter},
    table.Column{Title: "Item"}, // Width 0 → 自动均分剩余宽度
    table.Column{Title: "Qty", Width: 44, Align: text.AlignRight},
)
tbl.AddRow("1", "Hex Bolt", "100")

newPage := func() (*page.Page, float64) {
    return doc.AddPage(page.A4), 790 // 续页表格起始 y
}
lastPage, endY := tbl.Draw(p, 50, 700, 495, 60, newPage)
// 空间不足自动分页并重复灰底表头；newPage 传 nil 则不分页
```

单元格合并（跨列/跨行）与内容自适应列宽：

```go
tbl := table.New(
    table.Column{Title: "Category"},
    table.Column{Title: "Item"},
    table.Column{Title: "Amount", Align: text.AlignRight},
)
tbl.AutoWidth = true // Width 为 0 的列按内容测算列宽

// rowspan：Materials 跨两行（后续行被覆盖的列自动跳过）
tbl.AddRowCells(table.C("Materials").Span(1, 2), table.C("Steel"), table.C("1000.00"))
tbl.AddRowCells(table.C("Cement"), table.C("500.00"))
// colspan：合计行跨两列
tbl.AddRowCells(table.C("TOTAL").Span(2, 1), table.C("1500.00"))

tbl.Draw(p, 50, 700, 495, 60, newPage)
```

表头行合并（`HeaderRows` 表头内 `Span` 用法与数据行一致）与超高行拆分：

```go
tbl := table.New(table.Column{Width: 90}, table.Column{Width: 130}, table.Column{Width: 130})
tbl.HeaderRows = 2 // 前 2 行为表头，跨页自动重复
tbl.AddRowCells(table.C("GroupAB").Span(2, 1), table.C("GroupC").Span(1, 2))
tbl.AddRowCells(table.C("sub-a"), table.C("sub-b"))
// 超高行/跨行块超过整页可用高度时自动拆分续页：文本行不切断、不丢失
```

### 中文（CJK 嵌入字体）

```go
f, err := font.LoadCJKFile("NotoSansSC-Regular.ttf") // 任意 glyf 轮廓 TTF
// TTC 集合（如 simsun.ttc）：
// f, err := font.LoadCJKCollectionFile("uming.ttc", 0)
if err != nil {
    panic(err)
}
f.SetName("NotoSansSC")

p := doc.AddPage(page.A4)
p.DrawText(f, 18, 72, 780, "中文标题") // 与标准 14 字体用法一致
p.TextBox(f, 11, 72, 750, 450, "中文段落……", text.AlignLeft, 0)
```

- 保存文档时自动**子集化嵌入**：仅含实际用到的字形（复合字形自动闭包）
- 自动生成 **ToUnicode CMap**，pdftotext 可正确反查中文
- 支持 TTC 集合（`LoadCJKCollection`）；源字体带 kern 表时自动应用字距（TJ 输出），
  带 GSUB liga/rlig 时自动连字（`f.Ligatures`/`f.Kerning` 可关闭）
- 缺字形显示为 .notdef 并输出警告；`font.Resource` 接口统一标准字体与 CJK 字体

### 加密

```go
doc := pdf.New()
doc.SetEncryption(security.Options{
    UserPassword:  "user123",   // 打开密码（可为空）
    OwnerPassword: "owner456",  // 权限密码（为空取用户密码）
    Permissions:   security.PermAll &^ security.PermCopy, // 禁止复制
    Level:         security.AES256, // 或 security.AES128
})
// 正常绘制，保存即自动加密
```

权限位：`PermPrint` / `PermModify` / `PermCopy` / `PermAnnotate` / `PermFillForms` /
`PermExtract` / `PermAssemble` / `PermPrintHighQuality`，默认 `PermAll`。

公钥证书加密（PubSec，多收件人）：

```go
rcpt1, _ := sign.ParseCertPEM(pem1) // 收件人证书（RSA）
rcpt2, _ := sign.ParseCertPEM(pem2)
doc.SetPubKeyEncryption(security.PubKeyOptions{
    Recipients: []security.Recipient{
        {Certificate: rcpt1, Permissions: security.PermAll},
        {Certificate: rcpt2, Permissions: security.PermAll &^ security.PermCopy},
    },
    Level: security.AES128, // 或 AES256
})
// 任一收件人持对应私钥即可解密；加密可与签名叠加使用
```

> 注：AES-128（R4）握手按规范内部使用 RC4（不用于内容加密）；RC4 已被认为不安全，
> 安全敏感场景请用 `security.AES256`（R6，纯 AES/SHA-2）。

### 数字签名

```go
// 1. 文档加签名字段（占位符）
doc.SetSignature(&sign.Field{Reason: "合同审批", Location: "Shanghai"})
data, _ := doc.Bytes()

// 2. 签名（两遍法：ByteRange 覆盖除签名值外的全部字节）
key, _ := sign.GenerateRSAKey()                // 或 GenerateECDSAKey / ParseKeyPEM
cert, _ := sign.GenerateSelfSigned("张三", key) // 或 ParseCertPEM 加载真实证书
signed, _ := sign.Sign(data, sign.Options{Signer: key, Certificate: cert})
os.WriteFile("signed.pdf", signed, 0644)

// 3. 验签（篡改任意字节后 Valid 为 false）
res, _ := sign.Verify(signed)
fmt.Println(res.Valid, res.Signer, res.Reason)
```

进阶能力：

```go
// 证书链：嵌入中间证书，验证方沿链构建到受信根
signed, _ = sign.Sign(data, sign.Options{
    Signer: key, Certificate: cert, Chain: []*x509.Certificate{interCA},
})
res, _ := sign.VerifyWithOptions(signed, &sign.VerifyOptions{Roots: rootPool})
res.ChainValid // 证书信任链是否有效

// RFC 3161 时间戳（TSA 背书签名时刻）
signed, _ = sign.Sign(data, sign.Options{
    Signer: key, Certificate: cert,
    TSA: &sign.TSAOptions{URL: "http://tsa.example.com"},
})
res.Timestamp.Time // TSA 签发的时间戳时间

// 多重签名（会签）：第 2..N 次先追加增量修订字段再签署，前序签名保持有效
doc2, _ := sign.AppendSignatureField(signed, &sign.Field{Reason: "乙方签署"})
s2, _ := sign.Sign(doc2, sign.Options{Signer: key2, Certificate: cert2})
results, _ := sign.VerifyAll(s2) // 逐个校验全部签名

// 可见签名：指定矩形即渲染外观（签署人/日期/原因）
doc.SetSignature(&sign.Field{
    Rect: [4]float64{72, 600, 272, 660}, SignerName: "Zhang San", Reason: "合同签署",
})

// LTV：DSS 字典嵌入验证材料（证书链 + CRL + OCSP 响应）
doc.SetDSS(pdf.DSSData{Certs: chain, CRLs: crlDERs, OCSPs: ocspDERs})

// 签署第三方既有 PDF（任意来源、无占位符）：增量修订追加签名字段后签署，
// 既有字节不动；支持经典 xref 表与 xref 流/对象流布局，限未加密文档
data, _ := os.ReadFile("third-party.pdf")
signed, _ = sign.SignExisting(data, &sign.Field{Reason: "合同审批"},
    sign.Options{Signer: key, Certificate: cert})
```

## 命令行工具

```sh
go install gopkg.761sama.com/tenon-plugin-pdf/cmd/tenon-pdf@latest
```

```
tenon-pdf demo [-o out.pdf]                          功能演示 PDF（4 页）
tenon-pdf text [-o out.pdf] [-font Helvetica|Times-Roman|Courier]
              [-size 12] [-pagesize A4|A5|A3|Letter|Legal|B5] <in.txt>
                                                     文本文件 → PDF（自动换行分页）
tenon-pdf img  [-o out.pdf] <图片...>                JPEG/PNG/GIF 合成 PDF（每张一页）
tenon-pdf table [-o out.pdf] [-rows 120]             合同样式表格演示（跨页重复表头）
tenon-pdf cjk [-o out.pdf] [-rows 120] [-font 完整字体.ttf|.ttc] [-fontindex 0]
                                                      中文采购单演示（思源黑体子集嵌入）
tenon-pdf encrypt [-o out.pdf] [-user PW] [-owner PW] [-aes256]
              [-no-copy] [-no-print] [-no-modify] [-no-annotate]
              [-recip 收件人证书.pem]...                加密演示（密码或公钥证书 PubSec）
tenon-pdf sign [-o out.pdf] [-cert c.pem -key k.pem | -selfsign CN]
              [-in 既有.pdf]                              签署第三方/既有 PDF（增量修订）
              [-reason R] [-location L] [-ecdsa] [-save-keys 前缀]
              [-user PW] [-owner PW] [-aes256]          与加密组合
              [-chain ca.pem]... [-root 见 verify]      嵌入中间证书链
              [-tsa TSA_URL]                            RFC 3161 时间戳
              [-multi N]                                多重签名会签演示
              [-rect x0,y0,x1,y1] [-signer-name NAME]   可见签名外观
              [-dss] [-crl f.der]... [-ocsp f.der]...   LTV（DSS 验证材料）
                                                     数字签名演示（PKCS#7/CMS）
tenon-pdf verify [-root ca.pem] <signed.pdf>           校验签名（可多签、可验证书链）
tenon-pdf version                                    打印版本
```

> `cjk` 子命令默认使用内置的思源黑体演示子集字体
> （`cmd/tenon-pdf/assets/NotoSansSC-Subset.ttf`，OFL 协议，由 `tools/fontsubset`
> 从完整 Noto Sans SC 生成，字符集见 `assets/charset.txt`）。
> 需要任意中文文本时请用 `-font` 指定完整字体文件；TTC 集合用 `-fontindex` 选成员。

## 测试与验证

```sh
go test ./...   # 全部包的单元测试 + 集成测试
go vet ./...
go run ./cmd/tenon-pdf demo -o build/demo.pdf
```

集成测试在外部工具可用时自动启用交叉验证：pdfinfo/pdftotext（结构与文本提取）、
pdfsig 与 openssl cms（签名）、Ghostscript（渲染）。

## 已知限制

- **表格**：合并仅支持矩形区域（ColSpan×RowSpan），超出列数/行数自动截断；
  表头行合并的跨行不越过表头区（自动截断）；表头块本身超过整页时仍溢出绘制；
  自适应列宽按比例压缩时不低于最长单词宽（极端窄表仍可能溢出）
- **字体**：仅支持 glyf 轮廓的 TrueType 子集嵌入（TTF 及 TTC 集合成员）；
  OTF/CFF（CIDFontType0）、变量字体实例化（需先用 fonttools 等工具静态化）、
  竖排（Identity-V）未实现；OpenType 特性仅支持 GSUB liga/rlig 连字，
  字距仅支持 kern 表（GPOS 型字距未实现），复杂文字 shaping（阿拉伯/印度语系重排）
  超出范围
- **加密**：不支持 RC4 内容加密（V1–V3，纯遗留）；PubSec 仅支持 RSA 收件人证书；
  加密文档不支持增量多重签名（追加修订段需持文件密钥加密）
- **签名**：第三方既有 PDF 签署支持经典交叉引用表与 xref 流/对象流（ObjStm）布局
  （含两者混排的多段修订链、/XRefStm 混合引用段），限未加密文档
  （加密文档追加签名字段会给出明确错误）；LTV 中 CRL/OCSP 的在线获取与签署后
  增量追加由调用方负责；可见签名外观文本限 WinAnsi 字符（非 ASCII 显示为 '?'）；
  签名占位空间固定 16KB（超大证书链需调库常量）
- **PDF/A、PDF/X、PDF/UA** 合规性与标记 PDF（Tagged PDF）
- **高级着色**：函数类型 0/3/4（采样/拼接/PostScript）、网格渐变（Type 4–7）、
  平铺/图案 Pattern
- **可选内容组**（图层 OCG）、透明度组 XObject
- **表单**：单选按钮、下拉/列表框、JavaScript 动作（签名字段已支持）
- **其他批注**：FreeText、Stamp、Ink、FileAttachment、3D/多媒体
- **文件结构**：生成端只写经典 xref 表（对象流/xref 流输出未实现）；
  增量更新仅用于追加签名字段修订段（读取侧支持经典表与 xref 流/对象流混排输入）；
  线性化（web 优化）未实现
- **图像**：JBIG2/JPX（JPEG2000）解码、16 位通道、ICC 色彩配置
- **Symbol/ZapfDingbats** 未内置宽度表，本地排版时按 600/1000 估算
  （查看器使用内置度量，渲染不受影响）

## 更多文档

- `doc/design.md` — 设计文档：架构分层、模块依赖、关键算法权衡与设计决策
- `doc/usage.md` — 详细使用文档与各子包职责
- `SUMMARY.md` — 已实现的 PDF 组件/格式/样式完整清单与验证方式
- `dev.md` — 开发规范
