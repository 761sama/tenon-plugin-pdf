# tenon-plugin-pdf 设计文档

面向续作者与协作者：读完本文即可掌握架构分层、模块切分、关键算法的取舍与设计理由，
无需重读全部代码。API 用法见 `doc/usage.md`，功能清单与验证方式见 `SUMMARY.md`。

## 1. 总览与设计原则

tenon-plugin-pdf 是纯 Go 标准库、零第三方依赖的 PDF 1.7 生成库。
三条核心原则贯穿始终：

1. **高内聚低耦合，单一职责**：每个包只做一件事，包间依赖单向无环。
2. **双模式**：既是可导入的库（根包 `pdf`），又自带 CLI（`cmd/tenon-pdf`）。
3. **自底向上、确定性输出**：对象模型 → 序列化 → 文件结构，逐层纯粹，可测试。

## 2. 架构分层与模块划分

### 2.1 四层结构

```
┌─────────────────────────────────────────────────────────┐
│ 门面层   pdf（根包：Document 装配与序列化）              │
├─────────────────────────────────────────────────────────┤
│ 组件层   page table form outline annot metadata          │
│          security sign jsongen（JSON 描述 → 文档）       │
├─────────────────────────────────────────────────────────┤
│ 表现层   content（内容流） font/ttf（字体） image（图像） │
│          text（排版） color（颜色）                      │
├─────────────────────────────────────────────────────────┤
│ 基础层   object（对象模型） writer（文件结构）            │
└─────────────────────────────────────────────────────────┘
```

jsongen 是组件层的文档模板引擎：解析 JSON（页面/样式/内容块），
用 page + table + text 完成流式排版与自动分页。安全约束：字体只能经
`FontRegistry` 在代码层注册（JSON 按 id 引用，禁止路径注入）；段距采用
CSS 式外边距折叠（相邻 spaceAfter/spaceBefore 取较大者）。

### 2.2 依赖方向（实测自代码 import，无环）

```
object  ←  writer（写文件结构）
object  ←  content ← page
object  ←  image
object  ←  annot（+color） ← outline（+writer）
object  ←  metadata
color   ←  content、annot、outline、table、page、form
ttf     ←  font（CJKFont）
font    ←  text ← table；font ← page；font → writer（BuildDict 登记字体文件流）
content ←  page、form
page    ←  table、form（字段挂到页）、pdf
security→  仅 object
sign    →  零项目内依赖（纯标准库 + 字节处理）
pdf（根）→  annot font form image metadata object outline page security sign writer
jsongen →  pdf color font page table text（组件层末端，无人依赖）
```

关键点：

- **object / writer / content 三层分离的理由**：
  - `object` 只关心「PDF 值怎么表示与序列化」（§7.3），不知道自己在哪个文件里；
  - `writer` 只关心「文件物理结构」（§7.5）：间接对象编号、xref 偏移表、trailer。
    它不理解任何对象语义，只做 `Alloc/Add/Set/WriteTo`，加密通过
    `SetEncrypt(ref, Encryptor)` 钩子注入——加密器只实现
    `EncryptObject(num, obj) object.Object` 一个方法，writer 无需懂安全算法；
  - `content` 只关心「内容流操作符」（§7.8/§8/§9）：把 Go 调用翻译成
    `q/rg/re/f/Tj` 等字节。它不管理资源（字体/图像的注册在 page 层），
    因此可以独立测试操作符序列。
  三者各管一层抽象（值 → 文件 → 页面绘制），任何一层替换实现不影响其他层。

- **为什么 page 是组件层的中心**：页面是 PDF 的资源汇聚点（/Resources 挂在页面上）。
  `page.Page` 聚合 content.Builder + 字体/图像/ExtGState/Shading 资源登记表 +
  批注列表，向上提供画布 API。table 只依赖 page（画布）与 font/text（度量），
  不依赖根包 pdf——分页通过调用方回调 `newPage func() (*page.Page, float64)`
  完成，因此 table 可以在不构造 Document 的情况下独立测试。

- **为什么 security 只依赖 object**：加密发生在序列化时（writer 写出对象前逐对象
  加密字符串与流），它只操作对象模型。文档装配（pdf 根包）通过 `SetEncryption`
  持有选项，writer 通过接口调用，security 不知道页面的存在。

- **为什么 sign 零依赖**：签名是纯字节操作——定位占位符、算 SHA-256、构建 CMS、
  回填。输入输出都是 `[]byte`。这带来两个好处：sign 可以签任何来源的 PDF
  字节（不限于本库生成），且可以单独演化。

- **反向依赖为何禁止**：例如 font → writer 的方向（`BuildDict(w)`）是允许的
  （字体需要登记 FontFile2 间接流），而 writer → font 绝不允许，否则基础层
  被组件层污染。整个依赖图是有向无环图（DAG），新增组件只能「向下依赖」。

### 2.3 根包 pdf 的定位

根包只做**装配**：它不实现任何绘制/排版/加密/签名逻辑，而是把各组件的产物
（页面对象、字体字典、图像流、批注、大纲、表单、Encrypt 字典、签名字段）
按正确顺序登记进 writer，并补全交叉引用（页面 ref ↔ 大纲/批注/表单目标）。

## 3. 关键算法与设计权衡

### 3.1 表格列宽测算（table/autowidth.go）

`AutoWidth` 开启后，宽度为 0 的列按内容测算。算法是类 CSS 的 **min/pref 双测量**：

- 每列测两个值：`minW`（最长单词宽度 + 内边距，表格不至于压烂内容的底线）与
  `prefW`（单行全文宽度 + 内边距，完全舒展的理想宽度）。表头也参与测量，
  避免出现「表头比内容宽却按内容定宽」的截断。
- 分配：pref 总和 ≤ 可用宽度 → 按 pref 分配，剩余均分（表格填满总宽）；
  超出 → 按 pref 比例压缩，但**不低于 minW**。
- 跨列单元格：先不计入单列测量；其后计算「单元格 pref − 所跨列 pref 之和」的
  超额，均摊到所跨的自动列（定宽列不承担）。

**权衡**：
- 为什么不做「迭代收敛式」分配（压到 min 后把余量再分给别人）？
  一次比例压缩 + min 钳制在绝大多数文档里视觉差异不可辨，代码量小一个数量级。
  代价：极端窄表（所有列都触底 minW）下总宽可能溢出——已在文档明示。
- 为什么剩余宽度均分而不是按 pref 比例分？均分行为与既有「Width=0 均分」的
  默认语义一致，用户预期稳定；且对中文等宽字形列差异极小。
- 中文等无空格文本：最长单词 = 全文，min = pref，即中文列几乎不可压缩——
  这是符合 CJK 排版直觉的（中文本来就不靠空格断行）。

### 3.2 表格单元格合并与分页（table/table.go）

**网格展开（resolveGrid）**：行数据按声明顺序展开成网格。每行从左到右放置单元格，
遇到被上方 rowspan 覆盖的列自动跳过；跨度超出列数/行数自动截断（防御性，
不产生非法区域）。展开时同时记录 `breakBefore[]`——跨行块内部边界标记为
「不可分页」。

**行高（rowHeights）**：先按单行单元格定行高（内容换行后的最大行数），
再处理跨行单元格：需求高度超出所跨行高之和时，把差额补到**所跨末行**。
（为什么补末行而不是均摊？均摊会改变其他行的文字基线位置，视觉抖动；
补末行只影响块底部留白，最稳定。）

**分页整体回溯**：行逐一累加，放不下时从当前行向前回溯到最近的
「安全边界」（不在任何跨行块内部）；块起点到当前行的已分配行整体挪到下一页。

**超高块窗口化拆分（splitRuns + renderSplitRun）**：回溯到页首仍放不下
（块比整页可用高度还高）时，不再溢出绘制，而是把块拆成若干「窗口片段」
跨页续绘：窗口坐标为相对块起点的累计高度，每个片段页渲染落在窗口内的
单元格部分（背景/边框按可见片段画矩形，文本行按行框顶部归属窗口、
每行只在一页输出，并以片段矩形裁剪防溢出）。切点选择（`cleanCut`）：
候选为行边界与所有单元格的文本行边界，取 ≤ 页容量且不切断**任何**单元格
文本行的最大点——跨行合并单元格的长文本因此能在行边界处干净续页；
找不到干净切点（如单行文本行高超页）退化为硬切，由裁剪兜底。
表头行合并：HeaderRows 内单元格走与数据行相同的网格展开（`resolveGrid`），
跨列/跨行均可，仅跨行范围截断到表头区（`HeaderRows` 边界），
保证表头/数据边界始终是安全分页边界，跨页重复表头不被破坏。

**权衡**：
- 为什么不拆分跨行块（部分行留本页）？合同类表格里跨行合并通常是
  「同一类别/同一当事人」，拆开阅读语义断裂；能整体放下时整体移动是唯一
  不出错的选择。只有块超过整页（整体移动也无解）才拆分——此时按
  「不切断文本行」的窗口续绘，阅读语义与文本层提取都保持完整。
- 为什么用「声明式网格 + 两遍（先布局后渲染）」而不是流式绘制？
  因为跨行块的分页决策需要回看已分配行。代价是要在内存中保存行高数组
  （O(行数)，可忽略）。
- 已知边界（诚实声明）：合并仅矩形区域；合并单元格水平对齐取起始列设置，
  rowspan>1 时垂直居中；表头块本身超过整页时仍溢出绘制（罕见）；
  硬切兜底时被切断的文本行损失少量像素（文本提取不丢失）。

### 3.3 字体子集化（font/cjk.go + ttf/）

**编码侧（font.CJKFont）**：绘制文本先经 token 化管线——逐 rune 查 cmap 得字形，
再做 GSUB liga/rlig **连字贪心最长匹配**（可关）；每个输出 token（普通字符或连字）
动态分配 CID（从 1 连续），内容流写 2 字节大端 CID（Identity-H 编码）。
CID 键是 token 的 Unicode 文本（连字为多码点串），同时记录 CID→Unicode、CID→gid、
CID→宽度三张表。布局测量与绘制共用同一管线（连字与字距对两侧一致生效）。

**字距（kern 表）**：源字体带 kern format 0 子表时，`CJKFont` 实现可选接口
`font.Kerned`——`EncodeKerned` 返回 TJ 数组段（文本段与千分 em 调整值交替），
page 层检测到该接口即以 `TJ` 操作符输出；全部字距为零时返回 nil 回退 `Tj`。
符号约定：TJ 数值 n 使后一字形移动 −n/1000 em，kern 修正 k 要求移动 +k，故 n=−k。
GPOS 型字距未实现（需完整特性/脚本/定位规则解析，复杂度数倍）。

**集合支持（ttf/collection.go）**：TTC 文件 = 'ttcf' 头 + 各成员 sfnt 目录偏移数组。
表目录内偏移本身即相对文件起始，故解析只需把目录读取起点换成成员偏移
（`parseAt(data, off)`），其余逻辑零改动。`font.LoadCJKCollection(r, index)`
加载指定成员（如 simsun.ttc）。

**子集化（ttf.Subset）**：保存文档时（`BuildDict`）才做——因为只有此刻才知道
「实际用到哪些字」。步骤：

1. **组件闭包**：TrueType 复合字形（如 é = e + ´）引用其他字形，
   必须递归纳入，否则渲染缺笔；
2. 按 CID 顺序重建 glyf/loca/hmtx：新字形号 = CID，因此
   `/CIDToGIDMap` 直接用 `/Identity`，省掉一张映射表；
   重建时重写复合字形里的组件引用（旧 gid → 新 gid）；
3. 重建 cmap：format 4（连续码点分段 + glyphIdArray）覆盖 BMP；
   存在非 BMP 码点（扩展汉字/emoji）时附加 **format 12** 子表
   （码点与 gid 双连续才合并成组）。修补 maxp/hhea/head
   （含 checkSumAdjustment 全校验和重算）；name/OS/2/gasp 原样保留；
   GSUB/GPOS/GDEF/变体表直接丢弃（连字在编码侧已物化为独立字形，
   子集内无需保留规则）；
4. 生成 **ToUnicode CMap**（bfchar，CID → UTF-16BE Unicode），
   连字 CID 映射为多码点序列（如 "fi"→<00660069>），
   这是 pdftotext 能反查中文与连字原文的关键——没有它，
   渲染正常但复制/搜索全是乱码。

**权衡**：
- 为什么限定 glyf 轮廓 TTF？CFF（OTF）子集化要解析 Type2 charstring 与
  CID-keyed 结构，复杂度数倍。glyf 的「表复制 + 偏移重建」模型简单可靠。
  代价：思源黑体官方 OTF、CFF 版 TTC（如 Noto Serif CJK）不能用——
  glyf 版 TTC（simsun.ttc、uming.ttc）已支持，OTF 可获取对应 TTF 版规避。
- 连字为什么只做 liga/rlig 而不做完整 shaping？拉丁连字是独立替换，
  不涉及重排/上下文分解；阿拉伯/印度语系的正确 shaping 需要完整
  OpenType 管线（ccmp/init/medi/fina/定位），远超本库范围。
  连字默认启用（OpenType 默认特性语义），ToUnicode 保证文本层不失真。
- 为什么子集化放在 BuildDict 而不是绘制时？绘制是增量的，只有序列化时
  字符集才封闭。CID 分配在绘制时（token 化）先来先得完成，与序列化次数无关，
  因此同一文档多次序列化的字体部分保持一致（文件 ID/日期除外）。
- 为什么 CID 从 1 起连续分配？配合 `/W [1 [w1 w2 ...]]` 紧凑宽度数组，
  且 .notdef（CID 0）天然留给缺字形字符。
- kern 只支持 format 0：它是 MS 与 Apple 两大阵营唯一共同广泛使用的格式；
  format 1/2/3（状态机）在流通字体中近乎绝迹。
- CapHeight 取 OS/2 sCapHeight（v2+）；老字体无此字段时回退上升部 70% 估算。

### 3.4 数字签名（sign/）

**ByteRange 两遍回填**：PDF 签名要求 `/ByteRange` 覆盖除 `/Contents`
签名值之外的全部字节。但签名值长度依赖证书，而 `/ByteRange` 的数字本身
也在被覆盖的区域内——长度变化会移动偏移。解法：

1. 第一遍（`pdf.Document.SetSignature`）：写入**定宽占位符**——
   `/Contents <32768 个 '0'>`（16KB 签名空间）与
   `/ByteRange [0000000000 0000000000 0000000000 0000000000]`。
   定宽是核心：回填后文件长度与所有偏移不变。
2. 第二遍（`sign.Sign`）：定位占位符 → 先回填 ByteRange（定宽 10 位数字）→
   对两段范围算 SHA-256 → 构建 CMS → 十六进制回填进占位区，不足补零。

**CMS 构建**：标准库没有 CMS，手写 DER 编解码（`sign/der.go`）。
签名输入是已认证属性的 **SET OF** 编码（tag 0x31），而属性在 SignerInfo 里
以 `[0] IMPLICIT`（tag 0xA0）存放——标签不同、内容相同，这是 CMS 最经典的坑，
验签时须把属性内容重新包成 SET OF 再验。正确性由 openssl 与 pdfsig 双工具
外部验证（注意 openssl 必须加 `-binary`，否则对内容做 CRLF 规范化导致摘要不符）。

**多重签名（增量修订）**：单修订内的多个占位符无法顺序签署——后一个签名
回填其 /Contents 时改变了前一个签名 ByteRange 覆盖区内的字节。因此多重签名
（会签）走标准增量更新（§7.5.6）：`sign.AppendSignatureField` 在文件末尾
追加修订段（新字段部件对象 + 重定义 AcroForm/页面 /Annots + xref 子段 +
trailer /Prev），既有字节一概不动，前序签名保持有效。为此增量路径内置了一套
最小字节级解析（trailer/对象体/数组引用）。VerifyAll 按 /ByteRange 出现顺序
逐个校验，非末位签名只覆盖到其修订末尾属正常。

**第三方既有 PDF 签署**：增量修订机制与文档来源无关——`AppendSignatureField`
已泛化为支持任意经典 xref 表布局的 PDF：Catalog 无 AcroForm 时新建
`/AcroForm << /Fields [..] /SigFlags 3 >>` 并重定义 Catalog（AcroForm 为内联
字典时原地插入字段引用）；页树支持嵌套 Pages 节点遍历定位目标页
（`findPage` DFS）。`sign.SignExisting` = AppendSignatureField + Sign 一步完成。
签名值字典 /V 一律写为**间接对象**（本库生成路径同样如此）——部分验证器
（如 pyhanko）不接受内联 /V。

**时间戳（RFC 3161）**：`Options.TSA` 配置后，Sign 对签名值取 SHA-256 作为
messageImprint 向 TSA 请求令牌，作为 signature-time-stamp 未认证属性
（id-aa-signatureTimeStampToken，SignerInfo [1]）嵌入。令牌构建含 ESS
signingCertificateV2 属性——openssl ts -verify 强制要求。未认证属性不参与
主签名，带时间戳的文档对旧版验证器保持兼容。

**证书链验证**：`Options.Chain` 把中间证书并入 CMS 的 certificates [0] IMPLICIT
集合（DER 依次拼接）；解析侧逐张读出，`VerifyWithOptions` 用
`x509.Certificate.Verify` 沿链构建到受信根（Roots 缺省系统根池，Intermediates
自动并入 CMS 内嵌证书）。Verify 保持原语义（只验文档完整性与签名本身），
链验证是显式 opt-in——这样无根池环境的基础验签不受影响。

**可见签名**：`Field.Rect` 非零时，装配期为部件生成 /AP /N 正常外观流——
自包含 Form XObject（浅底 + 边框 + 签署人/日期/原因/地点文本行，字号按行数
自适应）。外观字节在签名前写入，属于 ByteRange 覆盖区（外观篡改会使签名失效，
这是正确语义）。生成期路径用字体包注册 Helvetica；增量修订路径（sign 包零依赖）
则内联 `/BaseFont /Helvetica` 直接字典。外观文本限 WinAnsi（Helvetica 能力边界）。

**LTV（DSS）**：`Document.SetDSS` 把验证材料（证书链 DER、CRL DER、OCSP 响应 DER）
各存为独立间接流，挂到 Catalog /DSS（ISO 32000-2 §12.8.4.3）。验证方据此离线
重建证书链与吊销状态。设计上 DSS 只是「材料容器」，库不抓取在线吊销信息——
CRL/OCSP 的获取（CA 应答）与签署后增量追加留给调用方。

**取舍**：
- 增量解析器为最小字节级实现：仅支持经典交叉引用表布局（trailer 字典），
  纯 xref 流 / 对象流（ObjStm）布局的第三方文档报明确错误，不做通用解析器。
- 加密文档不支持增量追加签名字段（追加对象需持文件密钥加密）。
- 占位空间固定 16KB，超大证书链需调库常量。

### 3.5 加密（security/）

标准安全处理器的两级算法都实现了：

- **AES-128（V4/R4，AESV2）**：MD5 派生密钥。按规范，O/U 值握手**必须**用 RC4
  （算法 2/5 的定义如此）——即使内容加密用 AES。所以包里实现了 RC4，
  但只用于握手；内容一律 AES-128-CBC + 随机 IV + PKCS#7。RC4 的不安全性
  在包注释与 SUMMARY 中均已注明。
- **AES-256（V5/R6，AESV3）**：ISO 32000-2 算法 2.B 迭代散列
  （SHA-256/384/512 + AES 反馈循环），O/U/OE/UE/Perms 五元组，
  内容加密直接用 256 位文件密钥（R6 不再派生对象级密钥）。

**权衡与坑**：
- 为什么两个级别都做？R4 兼容面最广（PDF 1.6 时代读者都能开），R6 是
  现行推荐。两者用户 API 只差 `Level` 一个枚举。
- R6 实现中实际踩过的坑（已修复并有回归测试）：2.B 散列的中间态 K 必须保留
  **完整散列长度**（SHA-384→48 字节、SHA-512→64 字节）参与下一轮，
  只在最终截断 32 字节；提前截断会导致密码校验失败。另一个易错点：
  poppler 校验所有者密码要用 `-opw`（`-upw` 只试用户密码）——这是工具用法，
  不影响实现。
- 取舍：不支持 RC4 内容加密（V1–V3，纯遗留）。
- **公钥证书加密（PubSec，§7.6.5）**：`/Filter /Adobe.PubSec` + `adbe.pkcs7.s5`。
  生成 20 字节随机种子，逐收件人将「种子 ‖ 该收件人权限（4 字节大端）」封装为
  CMS EnvelopedData（临时 AES-128 密钥加密内容，临时密钥用收件人 RSA 公钥以
  PKCS#1 v1.5 包裹）；文件密钥 = SHA-1(seed ‖ 各信封 DER)[:16]（V4）。
  注意信封字节本身参与密钥派生，必须先构建全部信封再算文件密钥。
  V4 时 /Recipients 挂在默认密码过滤器（DefaultCryptFilter）内。
  内容加密与 R4 完全相同（AESV2 对象级密钥派生），crypt/EncryptObject 路径复用；
  V5（AESV3）则用 SHA-256 派生 32 字节文件密钥、内容直接用文件密钥。
  仅支持 RSA 收件人证书（PDF 1.7 时代读者普遍仅支持 RSA 密钥传输）。
- **加密 + 签名组合**：签名占位符用 object.Raw 写入（Raw 不参与 EncryptObject
  的字符串/流加密），因此签名字段的 /Contents 与 /ByteRange 天然不被加密，
  序列化后 sign.Sign 在密文文档上照常定位与回填，ByteRange 覆盖加密态字节；
  /Reason、/M 等其余字段正常加密。密码与公钥两种加密均可叠加签名。

## 4. 核心设计决策与理由

### 4.1 纯标准库零依赖

PDF 所需的全部底层件标准库都有：compress/zlib（FlateDecode）、
crypto/aes、crypto/rsa/ecdsa、crypto/x509、image/png|gif|jpeg、
encoding/hex 等。零依赖换来：无供应链风险、`go get` 即得、长期可维护。
例外仅在**构建期工具**：字体度量表由 `font/genmetrics.py` 从系统 Liberation
字体生成（纯 Python 标准库），演示子集字体由 `tools/fontsubset` 生成——
它们不进运行时依赖。

### 4.2 输出 PDF 1.7

1.7 是 ISO 32000-1 的最终版本，覆盖除 AES-256（ISO 32000-2 特性，但读者
普遍向后兼容支持）外的全部已实现功能。选经典 xref 表而非 xref 流：
生成端更简单、调试可读、所有读者兼容。代价：文件略大、无对象流压缩——
对本库的输出规模（KB 级）无感。

### 4.3 双模式

库与 CLI 共享同一套包：CLI（`cmd/tenon-pdf`）只是根包 pdf 的薄封装，
八个子命令（demo/text/img/table/cjk/encrypt/sign/verify）各对应一种能力，
同时充当**活的集成测试**（每个子命令产物都经 poppler/ghostscript 验证）。

### 4.4 为什么是「对象模型 → 序列化 → xref/trailer」

PDF 文件 = 头部 + 间接对象体 + xref 偏移表 + trailer。这套结构天然分两层：
对象**语义**（字典里放什么）与对象**编址**（对象号、字节偏移）。
object 包管语义、writer 包管编址，中间用 `Ref{Num, Gen}` 连接。
`Alloc`（先占位后 `Set`）解决前向引用——页树引用页面、页面引用资源，
大纲条目互相引用（Prev/Next/First/Last），都依赖这个两阶段登记。
替代方案（一次性算好编号再写）要求全局预知对象图，远不如 Alloc/Set 灵活。

## 5. 数据流主线：pdf.New() → SaveFile()

以一次完整调用串起各包协作（`pdf.go` WriteTo 的装配顺序）：

```
用户绘制阶段：
  doc.AddPage(size)          → page.New：Page{Content: content.Builder, 资源表}
  p.DrawText/FillRect/...    → content 追加操作符；page 登记字体/图像/GState/Shading 资源
  p.DrawText(cjkFont, ...)   → font.CJKFont 动态分配 CID，记录字形使用
  doc.Outline().Add(...)     → outline 记录树（目标页用 annot.Destination 描述）
  doc.AddField(...)          → form 字段记录（关联 page）
  doc.SetEncryption(...)     → 暂存选项（此时还不加密！）
  doc.SetSignature(...)      → 暂存签名字段定义

序列化阶段（doc.SaveFile → WriteTo）：
  0. 若启用加密：先生成 16 字节文件 ID（ID 参与密钥派生），
     security.NewHandler(opts, id) 算出 O/U(/OE/UE/Perms) 与文件密钥，
     w.SetEncrypt(w.Add(handler.Dict()), handler)
  1. writer.Alloc 预留页树 ref + 全部页面 ref（前向引用）
  2. 图像去重登记：同一 *image.Image 多页共享一个 XObject 流；
     带透明通道的先登记 SMask 流
  3. 构造 Destination 解析器：页序号 → 页面对象 ref
  4. form.Build：字段字典（兼 Widget 批注）登记，ref 回填到所属页的 /Annots；
     签名字段写入定宽占位符（/Contents 全零 hex + /ByteRange 占位）
  4.5. 字体字典按 font.Resource 缓存：CJKFont 在此刻子集化嵌入（BuildDict）
  5. 逐页装配：资源字典（Font/XObject/ExtGState/Shading）+
     内容流（默认 FlateDecode 压缩）+ /Annots
  6. 页树（/Kids /Count）
  7. outline.Build：大纲树登记为间接对象（Prev/Next/First/Last 双向链）
  8. XMP 元数据流
  9. Catalog（/Pages /Outlines /PageMode /PageLayout /Metadata /AcroForm）
 10. Info 字典
     → writer.WriteTo：逐对象写出（启用加密时，除 Encrypt 字典外，
       每个对象先经 EncryptObject 递归加密字符串与流），
       记录偏移 → xref 表 → trailer（/Size /Root /Info /ID [/Encrypt]）→ %%EOF

签名后处理（可选）：
  sign.Sign(doc.Bytes(), ...) → 定位占位符 → 回填 ByteRange →
  SHA-256 两段范围 → 构建 CMS → 十六进制回填 /Contents → 定稿
```

## 6. 不确定项说明（诚实声明）

- R4 加密的 U 值末 16 字节按规范为「任意值」，当前填随机字节；
  各查看器均不校验它。
- 表格自适应列宽在「全部自动列都触底 minW 仍超宽」时会溢出总宽——
  属设计内取舍（见 3.1），非缺陷但需注意。
- Symbol/ZapfDingbats 无内置宽度表，本地排版按 600/1000 估算；
  查看器用内置度量渲染，不影响最终显示。
