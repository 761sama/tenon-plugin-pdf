package sign

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// xref 流 / 对象流（ObjStm）布局支持（ISO 32000-1 §7.5.8 / §7.5.7）：
// 现代生成器（Chrome/Skia、新版 Word、pdfTeX 1.40+、Acrobat 优化保存）
// 默认以 xref 流替代经典交叉引用表，并把小型对象压入对象流。
// 本文件实现增量签名所需的最小解析：从最后一个 startxref 出发沿 /Prev
// 链逐段建立对象索引（经典表与 xref 流可任意混排，含 /XRefStm 混合段），
// 对象按索引定位——普通间接对象按偏移取本体，压缩对象从 ObjStm 解压。
//
// 增量追加侧不引入新格式：修订段仍写经典 xref 表 + trailer（规范允许
// 两种 xref 形态在修订链中混排，poppler/qpdf/pyhanko 均接受）。

// xrefEntry 交叉引用条目（§7.5.8 表 18 的三种类型）。
type xrefEntry struct {
	typ    int // 0=free 1=普通间接对象 2=对象流内压缩对象
	offset int // typ=1：对象字节偏移
	gen    int // typ=1：世代号
	stmNum int // typ=2：所在对象流的对象号
	stmIdx int // typ=2：对象流内序号
}

// docIndex 整篇文档的对象索引（xref 链合并结果）与最新 trailer 字段。
type docIndex struct {
	entries    map[int]xrefEntry
	trailer    *trailerInfo
	objstmObjs map[int]map[int]string // ObjStm 对象号 → (成员对象号 → 本体)
}

// buildIndex 从最后一个 startxref 出发沿 /Prev 链建立对象索引。
// 返回索引与上一个 xref 段的位置（供新 trailer 的 /Prev 使用）。
func buildIndex(doc []byte) (*docIndex, int, error) {
	prevXref, err := lastStartxref(doc)
	if err != nil {
		return nil, 0, err
	}
	idx := &docIndex{
		entries:    map[int]xrefEntry{},
		trailer:    &trailerInfo{},
		objstmObjs: map[int]map[int]string{},
	}
	pos := prevXref
	for depth := 0; pos > 0; depth++ {
		if depth > 64 {
			return nil, 0, fmt.Errorf("sign: xref 链过长（/Prev 循环或损坏）")
		}
		sec, err := parseXrefSection(doc, pos)
		if err != nil {
			return nil, 0, err
		}
		// 沿链从新到旧，先见者为准（新修订覆盖旧条目）
		for num, e := range sec.entries {
			if _, ok := idx.entries[num]; !ok {
				idx.entries[num] = e
			}
		}
		mergeTrailer(idx.trailer, sec.trailer)
		// 混合段（§7.5.8.1）：经典表 trailer 里的 /XRefStm 补充压缩对象条目，
		// 与同段表条目不重叠；若重叠以经典表为准
		if sec.xrefStm > 0 {
			sub, err := parseXrefStreamAt(doc, sec.xrefStm)
			if err != nil {
				return nil, 0, err
			}
			for num, e := range sub.entries {
				if _, ok := sec.entries[num]; !ok {
					if _, ok2 := idx.entries[num]; !ok2 {
						idx.entries[num] = e
					}
				}
			}
		}
		pos = sec.prev
	}
	if idx.trailer.size == 0 {
		return nil, 0, fmt.Errorf("sign: trailer 缺少 /Size")
	}
	return idx, prevXref, nil
}

// mergeTrailer 把旧修订段的 trailer 字段并入（新段已有的键不覆盖）。
func mergeTrailer(dst *trailerInfo, src *trailerInfo) {
	if dst.size == 0 {
		dst.size = src.size
	}
	if dst.root == 0 {
		dst.root = src.root
	}
	if dst.info == 0 {
		dst.info = src.info
	}
	if dst.encrypt == 0 {
		dst.encrypt = src.encrypt
	}
	if dst.id == "" {
		dst.id = src.id
	}
}

// xrefSection 一个 xref 段（经典表或 xref 流）的解析结果。
type xrefSection struct {
	entries map[int]xrefEntry
	trailer *trailerInfo
	prev    int // /Prev 偏移，0 表示链尾
	xrefStm int // /XRefStm 偏移（混合段），0 表示无
}

// parseXrefSection 解析位于 pos 的 xref 段（自动判别经典表 / xref 流）。
func parseXrefSection(doc []byte, pos int) (*xrefSection, error) {
	if pos < 0 || pos+4 > len(doc) {
		return nil, fmt.Errorf("sign: xref 段偏移 %d 越界（文件损坏或截断）", pos)
	}
	if bytes.HasPrefix(doc[pos:], []byte("xref")) &&
		(pos+4 == len(doc) || isWhiteOrDelim(doc[pos+4])) {
		return parseXrefTable(doc, pos)
	}
	return parseXrefStreamAt(doc, pos)
}

func isWhiteOrDelim(b byte) bool {
	switch b {
	case 0, 9, 10, 12, 13, 32, '/', '<', '>', '[', ']', '(', ')', '{', '}', '%':
		return true
	}
	return false
}

// --- 经典交叉引用表（§7.5.4） ---

// xrefEntryRe 匹配一条 xref 表条目（定宽数字 + n/f + 行尾，行尾容忍常见变体）。
var xrefEntryRe = regexp.MustCompile(`^(\d{10}) (\d{5}) ([nf])(?:\r\n| \r\n| \n| \r|\n|\r| )`)

func parseXrefTable(doc []byte, pos int) (*xrefSection, error) {
	sec := &xrefSection{entries: map[int]xrefEntry{}, trailer: &trailerInfo{}}
	p := pos + 4 // 跳过 "xref"
	for {
		p = skipWhite(doc, p)
		if p >= len(doc) {
			return nil, fmt.Errorf("sign: xref 表不完整（文件截断）")
		}
		if bytes.HasPrefix(doc[p:], []byte("trailer")) {
			p += len("trailer")
			break
		}
		// 子段头 "start count"
		start, count, next, err := readTwoInts2(doc, p)
		if err != nil {
			return nil, fmt.Errorf("sign: xref 子段头解析失败（偏移 %d）", p)
		}
		p = next
		for i := 0; i < count; i++ {
			// 标准条目定长 20 字节（EOL 为 SP CR / SP LF / CR LF），
			// 容忍部分生成器写出的 "n \r\n"（21 字节）变体
			m := xrefEntryRe.FindSubmatch(doc[p:])
			if m == nil {
				return nil, fmt.Errorf("sign: xref 表条目格式错误或文件截断（偏移 %d）", p)
			}
			if m[3][0] == 'n' {
				off, _ := strconv.Atoi(string(m[1]))
				gen, _ := strconv.Atoi(string(m[2]))
				sec.entries[start+i] = xrefEntry{typ: 1, offset: off, gen: gen}
			}
			p += len(m[0])
		}
	}
	// trailer 字典
	dict, err := readDictText(doc, p)
	if err != nil {
		return nil, fmt.Errorf("sign: trailer 字典解析失败: %v", err)
	}
	fillTrailer(sec.trailer, dict)
	sec.prev = intValue(dict, "Prev")
	sec.xrefStm = intValue(dict, "XRefStm")
	return sec, nil
}

// --- xref 流（§7.5.8） ---

// parseXrefStreamAt 解析位于 pos 的 xref 流对象。
func parseXrefStreamAt(doc []byte, pos int) (*xrefSection, error) {
	num, dict, raw, err := readIndirectStream(doc, pos, nil)
	if err != nil {
		return nil, fmt.Errorf("sign: 偏移 %d 处不是有效的 xref 流或经典表: %v", pos, err)
	}
	if !regexp.MustCompile(`/Type\s*/XRef\b`).MatchString(dict) {
		return nil, fmt.Errorf("sign: 偏移 %d 处的对象 %d 不是 xref 流（/Type /XRef 缺失）", pos, num)
	}
	data, err := decodeStreamData(dict, raw)
	if err != nil {
		return nil, fmt.Errorf("sign: xref 流解码失败: %v", err)
	}
	w := intArray(dict, "W")
	if len(w) != 3 || w[0] < 0 || w[1] < 0 || w[2] < 0 {
		return nil, fmt.Errorf("sign: xref 流 /W 非法: %v", w)
	}
	size := intValue(dict, "Size")
	indexPairs := intArray(dict, "Index")
	if len(indexPairs) == 0 {
		if size == 0 {
			return nil, fmt.Errorf("sign: xref 流缺少 /Size 与 /Index")
		}
		indexPairs = []int{0, size}
	}
	if len(indexPairs)%2 != 0 {
		return nil, fmt.Errorf("sign: xref 流 /Index 项数必须为偶数")
	}
	sec := &xrefSection{entries: map[int]xrefEntry{}, trailer: &trailerInfo{}}
	fillTrailer(sec.trailer, dict)
	sec.prev = intValue(dict, "Prev")
	sec.xrefStm = intValue(dict, "XRefStm")

	rowLen := w[0] + w[1] + w[2]
	off := 0
	for k := 0; k+1 < len(indexPairs); k += 2 {
		start, count := indexPairs[k], indexPairs[k+1]
		if start < 0 || count < 0 {
			return nil, fmt.Errorf("sign: xref 流 /Index 含负数")
		}
		for i := 0; i < count; i++ {
			if off+rowLen > len(data) {
				return nil, fmt.Errorf("sign: xref 流数据不足（声明 %d 条，实际仅解析出 %d 条）", totalCount(indexPairs), off/rowLen)
			}
			row := data[off : off+rowLen]
			off += rowLen
			f0 := readBE(row[:w[0]])
			f1 := readBE(row[w[0] : w[0]+w[1]])
			f2 := readBE(row[w[0]+w[1]:])
			typ := 1 // w[0]==0 时缺省为 1（§7.5.8 表 17）
			if w[0] > 0 {
				typ = f0
			}
			e := xrefEntry{typ: typ}
			switch typ {
			case 1:
				e.offset = f1
				e.gen = f2
			case 2:
				e.stmNum = f1
				e.stmIdx = f2
			}
			sec.entries[start+i] = e
		}
	}
	return sec, nil
}

func totalCount(pairs []int) int {
	n := 0
	for k := 1; k < len(pairs); k += 2 {
		n += pairs[k]
	}
	return n
}

// readBE 按大端读取最多 8 字节整数（宽 0 返回 0）。
func readBE(b []byte) int {
	n := 0
	for _, c := range b {
		n = n<<8 | int(c)
	}
	return n
}

// intArray 提取 /Key [n n n ...] 整数数组。
func intArray(body, key string) []int {
	arr := arrayBody(body, key)
	if arr == "" {
		return nil
	}
	var out []int
	for _, tok := range strings.Fields(arr) {
		v, err := strconv.Atoi(tok)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// --- 流对象读取与解码 ---

var indObjHeaderRe = regexp.MustCompile(`^(\d+)\s+(\d+)\s+obj\b`)

// readIndirectStream 读取 pos 处的间接流对象，返回对象号、字典文本与原始流数据。
// idx 用于解析间接 /Length（可为 nil，此时仅支持直接 /Length）。
func readIndirectStream(doc []byte, pos int, idx *docIndex) (num int, dict string, raw []byte, err error) {
	if pos < 0 || pos >= len(doc) {
		return 0, "", nil, fmt.Errorf("偏移越界")
	}
	m := indObjHeaderRe.FindSubmatch(doc[pos:])
	if m == nil {
		return 0, "", nil, fmt.Errorf("缺少 \"N G obj\" 头")
	}
	num, _ = strconv.Atoi(string(m[1]))
	p := pos + len(m[0])
	// 字典
	p = skipWhite(doc, p)
	dictEnd, err := dictEndAt(doc, p)
	if err != nil {
		return 0, "", nil, err
	}
	dict = string(doc[p:dictEnd])
	p = skipWhite(doc, dictEnd)
	if !bytes.HasPrefix(doc[p:], []byte("stream")) {
		return 0, "", nil, fmt.Errorf("对象 %d 不是流对象（缺少 stream 关键字）", num)
	}
	p += len("stream")
	// stream 后必须跟一个 EOL（\r\n 或 \n）
	if p+1 < len(doc) && doc[p] == '\r' && doc[p+1] == '\n' {
		p += 2
	} else if p < len(doc) && (doc[p] == '\n' || doc[p] == '\r') {
		p++
	} else {
		return 0, "", nil, fmt.Errorf("对象 %d stream 关键字后缺少换行", num)
	}
	length := intValue(dict, "Length")
	if length == 0 && strings.Contains(dict, "/Length") {
		// 间接 /Length N 0 R
		if idx != nil {
			if lnum := refValueStr(dict, "Length"); lnum > 0 {
				if lbody, lerr := idx.body(doc, lnum); lerr == nil {
					length, _ = strconv.Atoi(strings.TrimSpace(lbody))
				}
			}
		}
		if length == 0 {
			return 0, "", nil, fmt.Errorf("对象 %d 的 /Length 无法解析", num)
		}
	}
	if length < 0 || p+length > len(doc) {
		return 0, "", nil, fmt.Errorf("对象 %d 流数据越界（/Length %d，文件损坏或截断）", num, length)
	}
	raw = doc[p : p+length]
	return num, dict, raw, nil
}

// decodeStreamData 按 /Filter（支持 FlateDecode）与 /DecodeParms /Predictor 解码流数据。
func decodeStreamData(dict string, raw []byte) ([]byte, error) {
	filters := filterNames(dict)
	data := raw
	for _, f := range filters {
		switch f {
		case "FlateDecode", "Fl":
			r, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("FlateDecode 初始化失败: %v", err)
			}
			dec, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				return nil, fmt.Errorf("FlateDecode 解压失败: %v", err)
			}
			data = dec
		default:
			return nil, fmt.Errorf("不支持的 /Filter /%s", f)
		}
	}
	// PNG/TIFF predictor（xref 流常见 /Predictor 12 + /Columns）
	if dp := dictValue(dict, "DecodeParms"); dp != "" {
		predictor := intValue(dp, "Predictor")
		if predictor > 1 {
			columns := intValue(dp, "Columns")
			if columns <= 0 {
				columns = 1
			}
			return applyPredictor(data, predictor, columns)
		}
	}
	return data, nil
}

// filterNames 提取 /Filter 名（单名或数组），去斜杠。
func filterNames(dict string) []string {
	i := strings.Index(dict, "/Filter")
	if i < 0 {
		return nil
	}
	rest := strings.TrimLeft(dict[i+len("/Filter"):], " \t\r\n")
	if strings.HasPrefix(rest, "[") {
		// 数组形态 [/FlateDecode ...]
		end := strings.Index(rest, "]")
		if end < 0 {
			return nil
		}
		var names []string
		for _, tok := range strings.Fields(rest[1:end]) {
			names = append(names, strings.TrimPrefix(tok, "/"))
		}
		return names
	}
	// 单名形态 /FlateDecode（下一个名字即别的键，只取一个）
	if strings.HasPrefix(rest, "/") {
		name := rest[1:]
		if j := strings.IndexAny(name, " \t\r\n<>[]/"); j >= 0 {
			name = name[:j]
		}
		return []string{name}
	}
	return nil
}

// dictValue 提取 /Key << ... >> 内联字典文本（含定界符）。
func dictValue(body, key string) string {
	i := strings.Index(body, "/"+key)
	if i < 0 {
		return ""
	}
	open := strings.Index(body[i:], "<<")
	if open < 0 {
		return ""
	}
	open += i
	depth := 0
	for j := open; j+1 < len(body); j++ {
		if body[j] == '<' && body[j+1] == '<' {
			depth++
			j++
		} else if body[j] == '>' && body[j+1] == '>' {
			depth--
			j++
			if depth == 0 {
				return body[open : j+1]
			}
		}
	}
	return ""
}

// applyPredictor 应用 PNG（10–15）/ TIFF（2）预测器解码（colors=1, bpc=8）。
func applyPredictor(data []byte, predictor, columns int) ([]byte, error) {
	if predictor == 2 {
		// TIFF：逐字节差分
		for i := columns; i < len(data); i++ {
			data[i] += data[i-columns]
		}
		return data, nil
	}
	if predictor < 10 || predictor > 15 {
		return nil, fmt.Errorf("不支持的 /Predictor %d", predictor)
	}
	rowLen := columns + 1
	if len(data)%rowLen != 0 {
		return nil, fmt.Errorf("PNG predictor 数据长度 %d 不是行宽 %d 的整数倍", len(data), rowLen)
	}
	out := make([]byte, 0, len(data))
	prev := make([]byte, columns)
	for p := 0; p < len(data); p += rowLen {
		ft := data[p]
		row := append([]byte(nil), data[p+1:p+rowLen]...)
		switch ft {
		case 0: // None
		case 1: // Sub
			for i := 1; i < columns; i++ {
				row[i] += row[i-1]
			}
		case 2: // Up
			for i := 0; i < columns; i++ {
				row[i] += prev[i]
			}
		case 3: // Average
			for i := 0; i < columns; i++ {
				a := 0
				if i > 0 {
					a = int(row[i-1])
				}
				row[i] += byte((a + int(prev[i])) / 2)
			}
		case 4: // Paeth
			for i := 0; i < columns; i++ {
				a, b, c := 0, int(prev[i]), 0
				if i > 0 {
					a = int(row[i-1])
					c = int(prev[i-1])
				}
				row[i] += byte(paeth(a, b, c))
			}
		default:
			return nil, fmt.Errorf("PNG predictor 未知行滤波器 %d", ft)
		}
		out = append(out, row...)
		copy(prev, row)
	}
	return out, nil
}

func paeth(a, b, c int) int {
	p := a + b - c
	pa, pb, pc := abs(p-a), abs(p-b), abs(p-c)
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// --- 对象解析 ---

// body 按索引取对象本体文本（普通对象按偏移取字节，压缩对象从 ObjStm 解压）。
func (d *docIndex) body(doc []byte, num int) (string, error) {
	e, ok := d.entries[num]
	if !ok || e.typ == 0 {
		return "", fmt.Errorf("sign: 对象 %d 不存在", num)
	}
	if e.typ == 2 {
		return d.objstmObject(doc, e.stmNum, num)
	}
	if e.offset <= 0 || e.offset >= len(doc) {
		return "", fmt.Errorf("sign: 对象 %d 偏移 %d 越界（文件损坏或截断）", num, e.offset)
	}
	m := indObjHeaderRe.FindSubmatch(doc[e.offset:])
	if m == nil {
		return "", fmt.Errorf("sign: 对象 %d 偏移处缺少 \"N G obj\" 头（索引与内容不一致）", num)
	}
	if n, _ := strconv.Atoi(string(m[1])); n != num {
		return "", fmt.Errorf("sign: 对象 %d 偏移处实际是对象 %d（索引与内容不一致）", num, n)
	}
	start := e.offset + len(m[0])
	// 找与 obj 头对应的 endobj（流数据内可能出现 endobj 文本，按 /Length 跳过流区）
	end := start
	if isStream, dataEnd := streamRegion(doc, d, num, start); isStream {
		end = dataEnd
	}
	i := bytes.Index(doc[end:], []byte("endobj"))
	if i < 0 {
		return "", fmt.Errorf("sign: 对象 %d 缺少 endobj（文件截断）", num)
	}
	return strings.TrimSpace(string(doc[start : end+i])), nil
}

// streamRegion 若对象从 start 起包含流，返回 true 与流数据结束位置。
func streamRegion(doc []byte, d *docIndex, num, start int) (bool, int) {
	p := skipWhite(doc, start)
	dictEnd, err := dictEndAt(doc, p)
	if err != nil {
		return false, 0
	}
	dict := string(doc[p:dictEnd])
	q := skipWhite(doc, dictEnd)
	if !bytes.HasPrefix(doc[q:], []byte("stream")) {
		return false, 0
	}
	q += len("stream")
	if q+1 < len(doc) && doc[q] == '\r' && doc[q+1] == '\n' {
		q += 2
	} else if q < len(doc) && (doc[q] == '\n' || doc[q] == '\r') {
		q++
	} else {
		return false, 0
	}
	length := intValue(dict, "Length")
	if length == 0 {
		if lnum := refValueStr(dict, "Length"); lnum > 0 && d != nil {
			if lbody, err := d.body(doc, lnum); err == nil {
				length, _ = strconv.Atoi(strings.TrimSpace(lbody))
			}
		}
	}
	if length <= 0 || q+length > len(doc) {
		return false, 0
	}
	return true, q + length
}

// objstmObject 从对象流 stmNum 中取出成员对象 num 的本体。
func (d *docIndex) objstmObject(doc []byte, stmNum, num int) (string, error) {
	objs, err := d.loadObjStm(doc, stmNum)
	if err != nil {
		return "", err
	}
	body, ok := objs[num]
	if !ok {
		return "", fmt.Errorf("sign: 对象 %d 不在对象流 %d 的声明中（索引损坏）", num, stmNum)
	}
	return body, nil
}

// loadObjStm 解码对象流并解析成员对象（结果缓存）。
func (d *docIndex) loadObjStm(doc []byte, stmNum int) (map[int]string, error) {
	if objs, ok := d.objstmObjs[stmNum]; ok {
		return objs, nil
	}
	e, ok := d.entries[stmNum]
	if !ok || e.typ != 1 {
		return nil, fmt.Errorf("sign: 对象流 %d 不存在或被压缩（不允许）", stmNum)
	}
	_, dict, raw, err := readIndirectStream(doc, e.offset, d)
	if err != nil {
		return nil, fmt.Errorf("sign: 对象流 %d 读取失败: %v", stmNum, err)
	}
	n := intValue(dict, "N")
	first := intValue(dict, "First")
	if n <= 0 || first <= 0 {
		return nil, fmt.Errorf("sign: 对象流 %d 缺少 /N 或 /First", stmNum)
	}
	data, err := decodeStreamData(dict, raw)
	if err != nil {
		return nil, fmt.Errorf("sign: 对象流 %d 解码失败: %v", stmNum, err)
	}
	if first >= len(data) {
		return nil, fmt.Errorf("sign: 对象流 %d /First 越界", stmNum)
	}
	header := strings.Fields(string(data[:first]))
	if len(header) < 2*n {
		return nil, fmt.Errorf("sign: 对象流 %d 头部声明不完整（应有 %d 对）", stmNum, n)
	}
	objs := map[int]string{}
	type seg struct{ num, off int }
	segs := make([]seg, 0, n)
	for i := 0; i < n; i++ {
		on, err1 := strconv.Atoi(header[2*i])
		oo, err2 := strconv.Atoi(header[2*i+1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("sign: 对象流 %d 头部解析失败", stmNum)
		}
		segs = append(segs, seg{on, oo})
	}
	bodyStart := first
	for i, s := range segs {
		end := len(data) - bodyStart
		if i+1 < len(segs) {
			end = segs[i+1].off
		}
		if s.off < 0 || end < s.off || bodyStart+s.off > len(data) {
			return nil, fmt.Errorf("sign: 对象流 %d 成员偏移越界", stmNum)
		}
		objs[s.num] = strings.TrimSpace(string(data[bodyStart+s.off : bodyStart+end]))
	}
	d.objstmObjs[stmNum] = objs
	return objs, nil
}

// --- 字典/空白扫描小工具 ---

// skipWhite 跳过 PDF 空白与注释。
func skipWhite(doc []byte, p int) int {
	for p < len(doc) {
		c := doc[p]
		if c == '%' {
			for p < len(doc) && doc[p] != '\n' && doc[p] != '\r' {
				p++
			}
			continue
		}
		if c == 0 || c == 9 || c == 10 || c == 12 || c == 13 || c == 32 {
			p++
			continue
		}
		break
	}
	return p
}

// dictEndAt 返回从 p（应指向 "<<"）开始、括号配对的字典结束位置（">>\" 之后）。
func dictEndAt(doc []byte, p int) (int, error) {
	if p+1 >= len(doc) || doc[p] != '<' || doc[p+1] != '<' {
		return 0, fmt.Errorf("缺少字典起始 <<")
	}
	depth := 0
	for i := p; i+1 < len(doc); i++ {
		// 跳过字面量字符串（其中可能含 < >）
		if doc[i] == '(' {
			j := skipLiteralString(doc, i)
			if j < 0 {
				return 0, fmt.Errorf("字典内字符串未闭合")
			}
			i = j - 1
			continue
		}
		if doc[i] == '<' && doc[i+1] == '<' {
			depth++
			i++
		} else if doc[i] == '>' && doc[i+1] == '>' {
			depth--
			i++
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("字典未闭合（文件截断）")
}

// skipLiteralString 返回 ')'' 之后的位置（处理转义与嵌套括号）。
func skipLiteralString(doc []byte, p int) int {
	depth := 0
	for i := p; i < len(doc); i++ {
		switch doc[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// readTwoInts2 读取 "start count" 两个整数，返回第二个整数之后的偏移。
func readTwoInts2(doc []byte, p int) (a, b, next int, err error) {
	re := regexp.MustCompile(`^(\d+)\s+(\d+)\s*`)
	m := re.FindSubmatch(doc[p:])
	if m == nil {
		return 0, 0, 0, fmt.Errorf("缺少整数对")
	}
	a, _ = strconv.Atoi(string(m[1]))
	b, _ = strconv.Atoi(string(m[2]))
	return a, b, p + len(m[0]), nil
}

// readDictText 读取 p 处（跳过空白后）的字典文本。
func readDictText(doc []byte, p int) (string, error) {
	p = skipWhite(doc, p)
	end, err := dictEndAt(doc, p)
	if err != nil {
		return "", err
	}
	return string(doc[p:end]), nil
}

// fillTrailer 从字典文本提取 trailer 字段。
func fillTrailer(t *trailerInfo, dict string) {
	t.size = intValue(dict, "Size")
	t.root = refValueStr(dict, "Root")
	t.info = refValueStr(dict, "Info")
	t.encrypt = refValueStr(dict, "Encrypt")
	t.id = arrayText(dict, "ID")
}
