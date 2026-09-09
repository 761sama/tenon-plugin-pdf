# tenon-plugin-pdf 使用文档

模块路径：`gopkg.761sama.com/tenon-plugin-pdf`（根包名 `pdf`）。

纯 Go 标准库实现，无第三方依赖。

## 作为库导入

```go
import (
    "gopkg.761sama.com/tenon-plugin-pdf"      // 门面包：pdf.Document
    "gopkg.761sama.com/tenon-plugin-pdf/page" // 页面尺寸与画布
    "gopkg.761sama.com/tenon-plugin-pdf/font" // 标准 14 字体
)

doc := pdf.New()
doc.Info().Title = "报告"
doc.Info().Author = "张三"

p := doc.AddPage(page.A4)
p.DrawText(font.HelveticaBold, 24, 72, 760, "Hello PDF")
p.SetFillColor(color.Red)
p.FillCircle(300, 500, 40)
p.FillAxialGradient(72, 300, 200, 80, color.Yellow, color.Blue, true)

doc.Outline().Add("首页", annot.Destination{PageIndex: 0})
err := doc.SaveFile("out.pdf")
```

### 各子包职责

| 包 | 职责 |
|---|---|
| `object` | PDF 对象模型（null/布尔/数值/字符串/名称/数组/字典/流/引用）与序列化 |
| `writer` | 文件结构：间接对象、交叉引用表、trailer、文件头尾 |
| `color` | Gray / RGB / CMYK 颜色模型 |
| `font` | 标准 14 字体、WinAnsi 编码、宽度度量；CJKFont 嵌入子集字体（Type0/Identity-H，支持 TTC、连字、字距） |
| `ttf` | TrueType（glyf）与 TTC 集合解析、子集化重建；kern 字距表与 GSUB liga 连字规则读取 |
| `content` | 内容流构建器：路径、绘制、裁剪、变换、文本操作、XObject |
| `text` | 文本换行与对齐（左/中/右/两端对齐） |
| `image` | JPEG 原样嵌入、PNG/GIF 解码嵌入、SMask 透明蒙版 |
| `page` | 页面尺寸（A 系列/B5/Letter/Legal…）与画布高级绘图 API |
| `annot` | 批注：URI 链接、页内跳转、便签、高亮/下划线/删除线/波浪线 |
| `outline` | 书签大纲（多级、加粗/斜体/颜色、折叠） |
| `metadata` | 文档信息字典 + XMP 元数据流 |
| `form` | AcroForm 交互表单：文本域、复选框 |
| `table` | 表格布局与绘制：定宽/均分/内容自适应列宽、灰底表头、单元格合并（跨列/跨行，含表头行）、边框、跨页重复表头、超高行/跨行块跨页拆分、表头行/单元格颜色覆盖 |
| `jsongen` | 从 JSON 描述生成 PDF（格式见 `doc/json-format.md`）；字体经 `FontRegistry` 代码层注册，JSON 禁止携带字体路径 |
| `security` | 标准安全处理器：用户/所有者密码、权限位、AES-128（R4）/AES-256（R6）加密；公钥证书加密（PubSec，多收件人） |
| `sign` | PKCS#7/CMS 数字签名：ByteRange 回填、RSA/ECDSA、验签、证书链验证、RFC 3161 时间戳、多重签名（增量会签）、第三方既有 PDF 签署、自签名/链式证书 |
| `pdf`（根包） | Document 门面：聚合各组件并序列化完整文件 |

### 表格（table 包）

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
// 空间不足时自动分页并重复灰底表头；newPage 传 nil 则不分页
```

合并单元格与自适应列宽：

```go
tbl.AutoWidth = true // Width 为 0 的列按内容测算（定宽列不变）
tbl.AddRowCells(table.C("Materials").Span(1, 2), table.C("Steel"), table.C("1000.00"))
tbl.AddRowCells(table.C("Cement"), table.C("500.00"))  // 被覆盖列自动跳过
tbl.AddRowCells(table.C("TOTAL").Span(2, 1), table.C("1500.00")) // 跨列
// 跨行合并块放得下时整体移动不拆页；超过整页可用高度时自动拆分续页，
// 文本行不切断、不丢失
```

表头行合并（`HeaderRows` 声明的表头行内用法与数据行一致）：

```go
tbl := table.New(table.Column{Width: 90}, table.Column{Width: 130}, table.Column{Width: 130})
tbl.HeaderRows = 2 // 前 2 行作为表头（跨页重复）
tbl.AddRowCells(table.C("GroupAB").Span(2, 1), table.C("GroupC").Span(1, 2)) // 跨列+跨行
tbl.AddRowCells(table.C("sub-a"), table.C("sub-b"))                          // C 列被占用跳过
// 表头跨行不越过表头区（超出自动截断到 HeaderRows 边界）
```

### 中文 / CJK 字体（font.CJKFont）

```go
f, err := font.LoadCJKFile("NotoSansSC-Regular.ttf") // 任意 glyf 轮廓 TTF
if err != nil { ... }
f.SetName("NotoSansSC")

p := doc.AddPage(page.A4)
p.DrawText(f, 18, 72, 780, "中文标题")          // 与标准 14 字体用法一致
p.TextBox(f, 11, 72, 750, 450, "中文段落……", text.AlignLeft, 0)
tbl := table.New(...)
tbl.Font = f                                     // 表格同样可用
```

TTC 集合（如 simsun.ttc、uming.ttc）：

```go
f, err := font.LoadCJKCollectionFile("uming.ttc", 0) // 第 0 个成员字体
// 或 font.LoadCJKCollection(r, index)
```

- 字体在保存文档时自动**子集化**：仅嵌入实际用到的字形（复合字形自动闭包）
- 自动生成 **ToUnicode CMap**，pdftotext 等工具可正确反查中文
- 文本按 rune 动态分配 CID（2 字节 Identity-H 编码），缺字形显示为 .notdef 并输出警告
- **连字**：源字体带 GSUB liga/rlig 时自动替换（如 fi/ffi），ToUnicode 保留原文多码点映射，
  复制/搜索结果不变；`f.Ligatures = false` 可关闭
- **字距**：源字体带 kern 表（format 0）时自动应用，绘制时输出 TJ 调整数组，
  宽度测量同步计入；`f.Kerning = false` 可关闭
- `font.Resource` 接口统一标准字体与 CJK 字体；任何 glyf TTF/TTC 成员均可加载

### 加密（security 包）

```go
doc := pdf.New()
doc.SetEncryption(security.Options{
    UserPassword:  "user123",   // 打开密码（可为空）
    OwnerPassword: "owner456",  // 权限密码（为空取用户密码）
    Permissions:   security.PermAll &^ security.PermCopy, // 禁止复制
    Level:         security.AES256, // 或 security.AES128
})
// …正常绘制，保存即自动加密
```

权限位：`PermPrint` / `PermModify` / `PermCopy` / `PermAnnotate` / `PermFillForms` /
`PermExtract` / `PermAssemble` / `PermPrintHighQuality`，默认 `PermAll`。

公钥证书加密（PubSec，ISO 32000-1 §7.6.5）：

```go
doc.SetPubKeyEncryption(security.PubKeyOptions{
    Recipients: []security.Recipient{
        {Certificate: certA, Permissions: security.PermAll},
        {Certificate: certB, Permissions: security.PermAll &^ security.PermCopy},
    },
    Level: security.AES128, // 或 AES256（V5/AESV3）
})
// 文件密钥用各收件人证书的 RSA 公钥封装（CMS EnvelopedData），
// 任一收件人持私钥即可解密；每收件人权限独立
```

加密与签名可组合：先 `SetEncryption`/`SetPubKeyEncryption` 再 `SetSignature`，
序列化后照常 `sign.Sign`（签名占位符不参与内容加密）。

> 注：AES-128（R4）的握手算法按规范内部使用 RC4（不用于内容加密）。
> RC4 已被认为不安全；安全敏感场景请用 `AES256`（R6，纯 AES/SHA-2）。

公钥证书加密（PubSec，收件人持私钥解密，无密码）：

```go
doc.SetPubKeyEncryption(security.PubKeyOptions{
    Recipients: []security.Recipient{
        {Certificate: aliceCert},                                     // 权限默认 PermAll
        {Certificate: bobCert, Permissions: security.PermAll &^ security.PermCopy},
    },
    Level: security.AES128, // 或 AES256（V5）
})
// 与 SetEncryption 互斥
```

### 数字签名（sign 包）

```go
// 1. 文档加签名字段（占位符）
doc.SetSignature(&sign.Field{Reason: "合同审批", Location: "Shanghai"})
data, _ := doc.Bytes()

// 2. 签名（两遍法：ByteRange 覆盖除签名值外的全部字节）
key, _ := sign.GenerateRSAKey()                 // 或 GenerateECDSAKey / 读取 PEM
cert, _ := sign.GenerateSelfSigned("张三", key)  // 或 ParseCertPEM 加载真实证书
signed, _ := sign.Sign(data, sign.Options{Signer: key, Certificate: cert})
os.WriteFile("signed.pdf", signed, 0644)

// 3. 验签
res, _ := sign.Verify(signed)
res.Valid // true；篡改任意字节后为 false
```

签署第三方既有 PDF（不经过本库生成、无签名占位符）：

```go
data, _ := os.ReadFile("third-party.pdf")          // 任意来源的既有 PDF
signed, _ := sign.SignExisting(data,               // 增量追加签名字段并签署
    &sign.Field{Reason: "合同审批"},               // field 传 nil 亦可
    sign.Options{Signer: key, Certificate: cert})
// 既有字节一概不动（标准增量修订）；可见签名用 Field.Rect 指定
// 限制：经典交叉引用表布局（非纯 xref 流/对象流）、未加密
```

证书链、时间戳、多重签名、可见签名与 LTV：

```go
// 证书链嵌入 + 信任链验证
signed, _ := sign.Sign(data, sign.Options{
    Signer: key, Certificate: cert,
    Chain: []*x509.Certificate{interCA}, // 中间证书随 CMS 嵌入
})
res, _ := sign.VerifyWithOptions(signed, &sign.VerifyOptions{
    Roots: rootPool,          // 受信根池；nil 用系统根池
    Intermediates: extraPool, // 可选附加中间证书
})
res.ChainValid // 证书链是否构建到受信根

// RFC 3161 时间戳（签名时刻由 TSA 背书）
signed, _ = sign.Sign(data, sign.Options{
    Signer: key, Certificate: cert,
    TSA: &sign.TSAOptions{URL: "http://tsa.example.com"},
})
res.Timestamp // *TimestampInfo{Time, ImprintValid}

// 多重签名（会签）：第 2..N 次签署自动追加增量修订段，前序签名保持有效
doc.SetSignature(&sign.Field{Reason: "甲方签署"})   // 首个字段在生成期声明
s1, _ := sign.Sign(data0, opts1)
s2, _ := sign.Sign(s1, opts2) // 自动 AppendSignatureField + 回填
// 也可显式控制字段属性：
s2f, _ := sign.AppendSignatureField(s1, &sign.Field{Reason: "乙方会签"})
s2, _ = sign.Sign(s2f, opts2)
results, _ := sign.VerifyAll(s2) // 逐个校验全部签名

// 可见签名：Rect 非零即在指定位置/尺寸渲染外观
doc.SetSignature(&sign.Field{
    Rect: [4]float64{72, 600, 272, 660},
    SignerName: "Zhang San", Reason: "合同签署",
})

// LTV：DSS 字典嵌入验证材料（离线长期验证）
doc.SetDSS(pdf.DSSData{
    Certs: []*x509.Certificate{interCA, rootCA},
    CRLs:  [][]byte{crlDER},   // x509.CreateRevocationList 或 CA 获取
    OCSPs: [][]byte{ocspDER},  // DER 编码的 OCSP 响应
})
```

> 边界：可见签名外观文本限 WinAnsi 字符（中文等显示为 '?'）；
> 第三方 PDF 签署不支持纯 xref 流/对象流布局与加密文档（增量解析器为最小字节级实现）；
> 加密文档不支持增量多重签名（追加修订段需持文件密钥加密）；
> LTV 中 CRL/OCSP 的在线获取由调用方负责。

签名选项（`sign.Options`）：

```go
sign.Options{
    Signer:      key,
    Certificate: cert,
    Chain:       []*x509.Certificate{interCA},      // 中间证书链（随 CMS 嵌入）
    TSA:         &sign.TSAOptions{URL: tsaURL},      // RFC 3161 时间戳服务器
}
```

证书信任链验证（Verify 只验签名本身，不验链）：

```go
roots := x509.NewCertPool()
roots.AddCert(rootCA)
res, _ := sign.VerifyWithOptions(signed, &sign.VerifyOptions{Roots: roots})
res.ChainValid // 链能否构建到受信根
```

多重签名（会签，增量修订语义——前序签名不被破坏）：

```go
doc.SetSignature(&sign.Field{Reason: "甲方签署"})
signed, _ := sign.Sign(data, optsA)                        // 第 1 人
doc2, _ := sign.AppendSignatureField(signed, &sign.Field{Reason: "乙方签署"})
signed2, _ := sign.Sign(doc2, optsB)                       // 第 2 人
results, _ := sign.VerifyAll(signed2)                      // 逐个校验
```

可见签名（Rect 非零即在页面渲染外观）：

```go
doc.SetSignature(&sign.Field{
    Rect:       [4]float64{350, 100, 560, 160}, // 左下-右上坐标
    SignerName: "Zhang San",                     // 外观中显示的签署人
    Reason:     "合同签署",
})
```

LTV（部分）：`doc.SetDSS(pdf.DSSData{Certs: chainCerts, CRLs: ..., OCSPs: ...})`
在 Catalog 嵌入 DSS 字典；CRL/OCSP 的在线获取由调用方负责。

加密 + 签名组合：直接同时 `SetEncryption`（或 `SetPubKeyEncryption`）+
`SetSignature` 即可，签名占位符不参与内容加密。

## 独立调用（命令行）

```sh
go install gopkg.761sama.com/tenon-plugin-pdf/cmd/tenon-pdf@latest

tenon-pdf demo -o demo.pdf              # 功能演示 PDF（4 页）
tenon-pdf text -o out.pdf input.txt     # 文本文件 → PDF（自动换行分页）
tenon-pdf text -font Courier -size 10 -pagesize Letter -o out.pdf in.txt
tenon-pdf img -o out.pdf a.jpg b.png    # 图片 → PDF（每张一页）
tenon-pdf table -o table.pdf -rows 120  # 合同样式表格演示（跨页重复表头）
tenon-pdf cjk -o cjk.pdf                # 中文采购单演示（思源黑体子集嵌入）
tenon-pdf cjk -font NotoSansSC-Regular.ttf -o cjk.pdf  # 使用完整字体文件
tenon-pdf cjk -font simsun.ttc -fontindex 0 -o cjk.pdf # 使用 TTC 集合成员
tenon-pdf json -o out.pdf data/contract.json \         # 从 JSON 描述生成 PDF
  -font song=C:\Windows\Fonts\simsun.ttc@0 \           # 注册 TTC 成员字体（id=路径@序号）
  -font hei=C:\Windows\Fonts\simhei.ttf \              # 注册 TTF（id=路径）
  -font mono=builtin:Courier                           # 注册标准 14 字体（id=builtin:名）
tenon-pdf encrypt -o enc.pdf -user u123 -owner o456 -aes256 -no-copy
tenon-pdf encrypt -o enc.pdf -recip alice.pem -recip bob.pem # 公钥证书加密
tenon-pdf sign -o signed.pdf -selfsign "张三" -reason "合同审批"
tenon-pdf sign -in contract.pdf -o signed.pdf -selfsign "张三"  # 签署既有/第三方 PDF
tenon-pdf sign -in contract.pdf -o s.pdf -selfsign A -rect 350,100,560,160  # 第三方+可见签名
tenon-pdf sign -o s.pdf -selfsign A -rect 350,100,560,160   # 可见签名
tenon-pdf sign -o s.pdf -selfsign A -tsa http://tsa.example # RFC 3161 时间戳
tenon-pdf sign -o s.pdf -selfsign A -chain ca.pem -dss      # 证书链 + DSS（LTV）
tenon-pdf sign -o s.pdf -selfsign A -multi 3                # 三重会签
tenon-pdf sign -o s.pdf -selfsign A -user u1                # 加密+签名组合
tenon-pdf verify signed.pdf             # 校验签名（多签名逐个校验）
tenon-pdf verify -root root.pem s.pdf   # 附证书链验证
tenon-pdf version
```

> `cjk` 子命令默认使用内置的思源黑体演示子集字体
> （`cmd/tenon-pdf/assets/NotoSansSC-Subset.ttf`，OFL 协议，
> 由 `tools/fontsubset` 从完整 Noto Sans SC 生成，字符集见 `assets/charset.txt`）。
> 需要任意中文文本时请用 `-font` 指定完整字体文件。
```

## 测试与验证

```sh
go test ./...      # 单元测试 + 集成测试（pdfinfo/pdftotext 可用时做外部验证）
go vet ./...
```

集成测试会生成覆盖全部功能的 PDF 并用 poppler 工具验证结构、页数与文本提取。
