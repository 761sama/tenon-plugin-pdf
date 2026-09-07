# JSON 文档格式说明 — 通过 JSON 生成 PDF

本格式描述一份完整 PDF 文档：页面设置、命名样式、内容流。
设计目标：与本库能力一一对应，可直接映射到 `page` / `font` / `text` / `table` 包的 API。

生成器：`jsongen` 包（`jsongen.Build(jsonData, fontRegistry)`），
CLI：`tenon-pdf json`（见文末）。

> **安全约定：字体路径不允许出现在 JSON 中。**
> 字体一律由调用方在代码层经 `jsongen.FontRegistry` 注册，JSON 样式仅按 `id` 引用；
> 引用未注册 id 直接报错。JSON 中其余可调参数（页面、样式、内容）均允许传入。

- 单位：除特别说明外，所有尺寸均为 **pt**（1/72 英寸）
- 颜色：`"#RRGGBB"` 十六进制字符串（内部映射到 DeviceRGB）
- 文本中的 `\n` 表示强制换行
- 文本中的零宽/格式控制字符（U+200B–U+200F、U+2060、U+FEFF、U+00AD）由生成器自动剔除
- 完整示例见 `data/contract.json`

## 顶层结构

```json
{
  "version": 1,
  "metadata": { ... },
  "page":     { ... },
  "styles":   { ... },
  "content":  [ ... ]
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `version` | int | 是 | 格式版本，当前为 `1` |
| `metadata` | object | 否 | 文档信息字典（标题/作者等） |
| `page` | object | 是 | 页面尺寸与页边距 |
| `styles` | object | 是 | 命名文本样式表（按 `id` 引用已注册字体） |
| `content` | array | 是 | 内容块序列，按顺序排版、自动分页 |

## metadata — 文档元数据

| 字段 | 类型 | 说明 |
|---|---|---|
| `title` | string | 标题 |
| `author` | string | 作者 |
| `subject` | string | 主题 |
| `keywords` | string | 关键词 |
| `creator` | string | 创建工具 |

全部可省略。对应 `doc.Info()`。

## page — 页面设置

```json
"page": {
  "size": "A4",
  "landscape": false,
  "margins": { "top": 72, "right": 60, "bottom": 72, "left": 60 }
}
```

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `size` | string \| object | `"A4"` | 预设名（`A3`/`A4`/`A5`/`B5`/`Letter`/`Legal`）或 `{"width": W, "height": H}` 自定义尺寸（pt） |
| `landscape` | bool | `false` | 横向 |
| `margins` | object | 四边 72 | 页边距（`top`/`right`/`bottom`/`left`）；给出时未写的边为 0 |

## fonts — 字体注册（代码层，不在 JSON 内）

JSON **不包含**字体段。`styles` 中的 `font` 字段引用代码层注册的 `id`：

```go
reg := jsongen.NewFontRegistry()
reg.RegisterCollection("song", `C:\Windows\Fonts\simsun.ttc`, 0) // TTC 集合成员
reg.RegisterFile("hei", `C:\Windows\Fonts\simhei.ttf`)           // TTF
reg.RegisterBuiltin("mono", "Courier")                           // 标准 14 字体
reg.Register("custom", someFontResource)                         // 任意已加载字体

doc, err := jsongen.Build(jsonData, reg)
doc.SaveFile("out.pdf")
```

| 方法 | 用途 |
|---|---|
| `Register(id, resource)` | 注册已加载的 `font.Resource` |
| `RegisterFile(id, path)` | 注册 TTF 文件（.ttc 取成员 0；保存时自动子集化嵌入） |
| `RegisterCollection(id, path, index)` | 注册 TTC 集合成员 |
| `RegisterBuiltin(id, name)` | 注册标准 14 字体：`Helvetica`/`Helvetica-Bold`/`Helvetica-Oblique`/`Helvetica-BoldOblique`、`Times-Roman`/`Times-Bold`/`Times-Italic`/`Times-BoldItalic`、`Courier`/`Courier-Bold`/`Courier-Oblique`/`Courier-BoldOblique`、`Symbol`、`ZapfDingbats`（不嵌入，仅 WinAnsi 字符） |

## styles — 命名样式

`styles` 是「名称 → 文本样式」的映射。内容块通过 `style` 字段引用，并可用同名字段内联覆盖。

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `font` | string | — | 代码层注册的字体 `id`（必填） |
| `size` | number | 12 | 字号（pt） |
| `color` | string | `"#000000"` | 文本颜色 |
| `align` | string | `"left"` | 对齐：`left` / `center` / `right` / `justify`（两端对齐） |
| `lineHeight` | number | 1.5 | 行高倍数（×字号） |
| `indent` | number | 0 | 首行缩进（×字号宽度，`2` 即缩进 2 字符） |
| `spaceBefore` | number | 0 | 段前距（pt） |
| `spaceAfter` | number | 0 | 段后距（pt） |
| `underline` | bool | false | 下划线 |
| `strikeThrough` | bool | false | 删除线 |

## content — 内容块

数组按顺序排版，超出当前页剩余空间时自动切分新页（页边距不变）。

### 1. paragraph — 段落

```json
{ "type": "paragraph", "style": "body", "text": "整段同一样式的文本。" }
```

多样式混排用 `runs`（与 `text` 互斥）；每个 run 可覆盖任意样式字段：

```json
{
  "type": "paragraph", "style": "body",
  "runs": [
    { "text": "单位名称：" },
    { "text": "　　　　　　　　", "color": "#C0C4CC" }
  ]
}
```

块级亦可直接覆盖样式字段，如 `{ "type": "paragraph", "style": "body", "align": "center", ... }`。

### 2. table — 表格

```json
{
  "type": "table",
  "style": "tableCell",
  "columns": [
    { "width": 118, "align": "center" },
    { "width": 0 }
  ],
  "headerRows": 1,
  "header": { "background": "#F5F7FA", "align": "center" },
  "border": { "width": 0.75, "color": "#333333" },
  "padding": 8,
  "rows": [
    [ "项目\n信息", "甲方（委托方）：某某单位（甲方单位）" ],
    [ "单位名称", "某某单位" ],
    [ { "text": "合计", "colSpan": 1, "rowSpan": 2 }, "..." ]
  ]
}
```

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `style` | string | — | 单元格文本默认样式（引用 `styles`） |
| `columns` | array | 是 | 列定义；`width` 为 pt，`0` 表示均分剩余宽度；`align` 为该列默认对齐 |
| `headerRows` | int | 0 | 前 N 行作为表头（底色/居中/垂直居中，跨页自动重复；支持多行文本） |
| `header.background` | string | — | 表头行底色 |
| `header.align` | string | `"center"` | 表头文本对齐 |
| `header.style` | string | — | 表头文本命名样式（仅取其颜色；字号/字体随表格 `style`） |
| `border.width` | number | 0.5 | 边框线宽 |
| `border.color` | string | `"#000000"` | 边框颜色 |
| `padding` | number | 4 | 单元格内边距（pt） |
| `rows` | array | 是 | 数据行；单元格为字符串简写或单元格对象 |

单元格对象字段：

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `text` | string | `""` | 内容（`\n` 强制换行，行高自适应） |
| `colSpan` | int | 1 | 跨列数 |
| `rowSpan` | int | 1 | 跨行数（被覆盖位置留空即可，自动跳过；跨行块分页时整体移动） |
| `align` | string | 列默认 | 覆盖对齐 |
| `color` | string | 样式默认 | 覆盖文本颜色 |
| `background` | string | 白 | 单元格底色（表头行默认取 `header.background`） |
| `style` | string | 表格默认 | 命名样式引用（取其字体/字号/颜色覆盖该单元格） |

### 3. columns — 多栏并排

各栏独立排版（仅支持 `paragraph`/`spacer` 子块），块高取最高栏；
整组放不进当前页剩余空间时整体移至新页（栏内不再分页）：

```json
{
  "type": "columns",
  "gap": 20,
  "widths": [0, 0],
  "children": [
    [ { "type": "paragraph", "style": "sigHead", "text": "甲方（盖章）" } ],
    [ { "type": "paragraph", "style": "sigHead", "text": "乙方（盖章）" } ]
  ]
}
```

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `gap` | number | 0 | 栏间距（pt） |
| `widths` | array | 全 0 | 各栏宽度（pt），`0` 均分剩余宽度 |
| `children` | array | 是 | 每栏一个内容块数组 |

栏首块的段前距与 columns 之前的段后距折叠（同段落规则）。

### 4. spacer — 垂直间隔

```json
{ "type": "spacer", "height": 16 }
```

### 5. pageBreak — 强制分页

```json
{ "type": "pageBreak" }
```

## 与库 API 的映射

| JSON 概念 | 对应 API |
|---|---|
| `page.size` / `margins` | `page.Size`、`doc.AddPage` |
| 字体注册（代码层） | `jsongen.FontRegistry` → `font.LoadCJKFile` / `font.LoadCJKCollectionFile` / 标准 14 字体 |
| 段落对齐 / 换行 | 与 `text` 包同策略（左/中/右/两端对齐，贪心换行、长词硬拆、中文按字断行） |
| `table` 块 | `table.New` + `AddRowCells`（`colSpan`/`rowSpan`、`HeaderRows`、单元格颜色覆盖） |
| `underline` / `strikeThrough` | 文本样式辅助（下划线/删除线） |
| 自动分页 | 段落排版与表格跨页（表头自动重复） |

## 命令行

```sh
tenon-pdf json -o out.pdf \
  -font song=C:\Windows\Fonts\simsun.ttc@0 \
  -font hei=C:\Windows\Fonts\simhei.ttf \
  -font mono=builtin:Courier \
  data/contract.json
```

`-font` 可重复：`id=路径`（TTF）、`id=路径@序号`（TTC 成员）、`id=builtin:标准字体名`。

## 注意事项

- 字体路径只允许经代码层（`FontRegistry` / `-font` 标志）注册，JSON 引用未注册 id 直接报错
- 零宽/格式控制字符由生成器自动剔除；其余字体无字形的字符渲染为 .notdef 并输出警告
- 中文加粗无合成加粗参数：请注册黑体类字体（如 simhei.ttf）作为独立样式
- 表格合并仅支持矩形区域；表头行不参与合并；超过整页的跨行块溢出绘制
- 两端对齐通过拉伸空格实现，无空格的纯中文行按字铺满、最右允许约一字内误差
- 分页按文字实际高度（ascent+descent）判定（与浏览器一致）；单行且段前距 ≥8pt 的
  段落视为标题，启用孤行控制（本页放不下"标题+一行正文"时整段下推）
- 相邻段落的 `spaceAfter`/`spaceBefore` 折叠取较大者（CSS margin collapsing）；
  页首的段前距与 spacer 自动折叠
