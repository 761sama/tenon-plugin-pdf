package ttf

import "fmt"

// Type 2 charstring（CFF 字形程序）的执行式解析与重编码。
// 用途：CFF 子集化时对局部/全局 Subrs 做调用闭包分析（callsubr/callgsubr），
// 仅保留被引用的子程序并按新编号改写调用操作数。
//
// 解析必须模拟执行（跟随子程序调用）：stem 声明（hstem/vstem 等）可出现在
// 子程序内部，hintmask/cntrmask 的掩码字节长度取决于调用点累积的 stem 数，
// 不跟踪执行就无法确定掩码边界（思源宋体等大量依赖此模式）。
// 合法字体的同一子程序在不同调用点的掩码边界必然一致，故首次执行的解析结果
// 即为该子程序的规范指令序列，后续调用直接沿用（仅重放 stem 与调用副作用）。

// csInstr 是一条 charstring 指令：操作数 + 操作符 + 掩码字节（仅 hintmask/cntrmask）。
type csInstr struct {
	op     uint16  // 单字节 0..31（28/255 除外），双字节为 0x0C00|第二字节
	args   []csNum // 本程序块内的操作数（不含经栈继承自调用方的值）
	mask   []byte  // hintmask/cntrmask 的掩码原始字节
	callee int     // 调用指令解析出的子程序索引（非调用指令为 -1）
	global bool    // 调用目标是否为全局子程序
}

// csNum 是 charstring 操作数：整数或 16.16 定点（255 编码）。
type csNum struct {
	fixed bool  // true 表示 16.16 定点，i 为原始 32 位（按 int32 解释）
	i     int64 // 整数值（fixed 时为重编码用的原始位型）
}

// Type 2 charstring 操作符（本实现用到的子集）。
const (
	csOpHStem     = 1
	csOpVStem     = 3
	csOpCallSubr  = 10
	csOpReturn    = 11
	csOpEndChar   = 14
	csOpHStemHM   = 18
	csOpHintMask  = 19
	csOpCntrMask  = 20
	csOpVStemHM   = 23
	csOpCallGSubr = 29
)

// 单个 charstring 字节块（字形主程序或子程序）的最大调用深度。
const csMaxDepth = 32

// csParser charstring 执行式解析器：跟踪跨子程序的 stem 计数，
// 记录各子程序的指令序列（Prog）与被调用集合（Used）。
type csParser struct {
	globals [][]byte          // 全局 Subrs 成员
	locals  [][]byte          // 当前 FD 的局部 Subrs 成员（可为空）
	stems   int               // 已声明 stem 数（跨子程序累积）
	GProg   map[int][]csInstr // 已解析的全局子程序指令序列
	LProg   map[int][]csInstr // 已解析的局部子程序指令序列
	UsedG   map[int]bool      // 被调用的全局子程序索引
	UsedL   map[int]bool      // 被调用的局部子程序索引
	activeG map[int]bool      // 调用栈上的全局子程序（防环）
	activeL map[int]bool      // 调用栈上的局部子程序（防环）
}

// 创建解析器。
// 入参: globals 为全局 Subrs 成员，locals 为当前 FD 的局部 Subrs 成员（可空）
func newCSParser(globals, locals [][]byte) *csParser {
	return &csParser{
		globals: globals,
		locals:  locals,
		GProg:   map[int][]csInstr{},
		LProg:   map[int][]csInstr{},
		UsedG:   map[int]bool{},
		UsedL:   map[int]bool{},
		activeG: map[int]bool{},
		activeL: map[int]bool{},
	}
}

// 解析一个字形的主 charstring：返回其指令序列；
// 沿途递归解析被调用的子程序（结果存于 GProg/LProg，索引集存于 UsedG/UsedL）。
func (p *csParser) ParseGlyph(blob []byte) ([]csInstr, error) {
	p.stems = 0
	instrs, _, err := p.exec(blob, 0, nil)
	return instrs, err
}

// 执行 blob 指向的 charstring 字节，返回指令序列与返回时栈上遗留的操作数。
// 操作数栈跨子程序调用保持连续：callsubr/callgsubr 只弹出子程序号，
// 其余栈值作为子程序的操作数传入，子程序 return 时的栈遗留交还调用方。
// 入参: depth 为当前子程序调用深度（0 表示字形主程序）；stack 为进入时的操作数栈
func (p *csParser) exec(blob []byte, depth int, stack []csNum) ([]csInstr, []csNum, error) {
	if depth > csMaxDepth {
		return nil, nil, fmt.Errorf("ttf: charstring 子程序调用深度超过 %d", csMaxDepth)
	}
	var instrs []csInstr
	var seg []csNum // 本程序块内自上一操作符以来读入的操作数（字节级归属）
	i := 0
	for i < len(blob) {
		b0 := blob[i]
		switch {
		case b0 == 28: // int16
			if i+3 > len(blob) {
				return nil, nil, fmt.Errorf("charstring int16 截断")
			}
			stack = append(stack, csNum{i: int64(int16(u16(blob, i+1)))})
			seg = append(seg, stack[len(stack)-1])
			i += 3
		case b0 == 255: // 16.16 定点
			if i+5 > len(blob) {
				return nil, nil, fmt.Errorf("charstring 定点数截断")
			}
			stack = append(stack, csNum{fixed: true, i: int64(int32(u32(blob, i+1)))})
			seg = append(seg, stack[len(stack)-1])
			i += 5
		case b0 >= 32 && b0 <= 246:
			stack = append(stack, csNum{i: int64(b0) - 139})
			seg = append(seg, stack[len(stack)-1])
			i++
		case b0 >= 247 && b0 <= 250:
			if i+2 > len(blob) {
				return nil, nil, fmt.Errorf("charstring 整数截断")
			}
			stack = append(stack, csNum{i: int64(b0-247)*256 + int64(blob[i+1]) + 108})
			seg = append(seg, stack[len(stack)-1])
			i += 2
		case b0 >= 251 && b0 <= 254:
			if i+2 > len(blob) {
				return nil, nil, fmt.Errorf("charstring 整数截断")
			}
			stack = append(stack, csNum{i: -int64(b0-251)*256 - int64(blob[i+1]) - 108})
			seg = append(seg, stack[len(stack)-1])
			i += 2
		default: // b0 ≤ 31（28 已除外）：操作符
			op := uint16(b0)
			i++
			if b0 == 12 {
				if i >= len(blob) {
					return nil, nil, fmt.Errorf("charstring 转义操作符截断")
				}
				op = 0x0C00 | uint16(blob[i])
				i++
			}
			instr := csInstr{op: op, args: seg, callee: -1}
			seg = nil
			var err error
			switch op {
			case csOpHStem, csOpVStem, csOpHStemHM, csOpVStemHM:
				p.stems += len(stack) / 2 // 含可选 width 时整除结果不变
				stack = stack[:0]
			case csOpHintMask, csOpCntrMask:
				p.stems += len(stack) / 2 // 栈上带 stem 参数时视为隐含 vstemhm
				stack = stack[:0]
				ml := (p.stems + 7) / 8
				if i+ml > len(blob) {
					return nil, nil, fmt.Errorf("charstring 掩码截断")
				}
				instr.mask = blob[i : i+ml]
				i += ml
			case csOpCallSubr, csOpCallGSubr:
				stack, err = p.call(&instr, stack, depth)
				if err != nil {
					return nil, nil, err
				}
			case csOpReturn, csOpEndChar:
				instrs = append(instrs, instr)
				return instrs, stack, nil
			default: // 绘图等操作符：消费整个操作数栈
				stack = stack[:0]
			}
			instrs = append(instrs, instr)
		}
	}
	return instrs, stack, nil // 无 return/endchar 收尾（防御：按返回处理）
}

// 处理子程序调用指令：解析目标子程序（必要时），执行/重放其副作用。
// 入参: instr 为 callsubr/callgsubr 指令，stack 为调用时的操作数栈（含栈顶子程序号），
// depth 为调用深度
// 出参: 子程序返回后的操作数栈（弹出子程序号、带入子程序的栈遗留）
func (p *csParser) call(instr *csInstr, stack []csNum, depth int) ([]csNum, error) {
	if len(stack) == 0 || stack[len(stack)-1].fixed {
		return nil, fmt.Errorf("ttf: charstring 子程序调用缺操作数")
	}
	v := int(stack[len(stack)-1].i)
	rest := stack[:len(stack)-1]
	var idx int
	if instr.op == csOpCallGSubr {
		idx = v + subrBias(len(p.globals))
		instr.global = true
	} else {
		idx = v + subrBias(len(p.locals))
	}
	instr.callee = idx
	if instr.global {
		if idx < 0 || idx >= len(p.globals) {
			return nil, fmt.Errorf("ttf: callgsubr 索引 %d 越界", idx)
		}
		p.UsedG[idx] = true
	} else {
		if idx < 0 || idx >= len(p.locals) {
			return nil, fmt.Errorf("ttf: callsubr 索引 %d 越界", idx)
		}
		p.UsedL[idx] = true
	}
	active, progs, blobs := p.activeL, p.LProg, p.locals
	if instr.global {
		active, progs, blobs = p.activeG, p.GProg, p.globals
	}
	if active[idx] {
		return rest, nil // 环：合法字体不存在，跳过以避免死循环
	}
	sub, ok := progs[idx]
	if !ok {
		active[idx] = true
		var err error
		sub, rest, err = p.exec(blobs[idx], depth+1, rest)
		delete(active, idx)
		if err != nil {
			return nil, err
		}
		progs[idx] = sub
		return rest, nil
	}
	return p.replay(sub, depth+1, rest)
}

// 重放已解析子程序的副作用（同一子程序再次调用时）：stem 计数与嵌套调用，
// 操作数栈语义与 exec 一致。
// 出参: 子程序返回后的操作数栈
func (p *csParser) replay(instrs []csInstr, depth int, stack []csNum) ([]csNum, error) {
	if depth > csMaxDepth {
		return nil, fmt.Errorf("ttf: charstring 子程序调用深度超过 %d", csMaxDepth)
	}
	for j := range instrs {
		in := &instrs[j]
		stack = append(stack, in.args...)
		switch in.op {
		case csOpHStem, csOpVStem, csOpHStemHM, csOpVStemHM, csOpHintMask, csOpCntrMask:
			p.stems += len(stack) / 2
			stack = stack[:0]
		case csOpCallSubr, csOpCallGSubr:
			var err error
			stack, err = p.call(in, stack, depth)
			if err != nil {
				return nil, err
			}
		case csOpReturn, csOpEndChar:
			return stack, nil
		default:
			stack = stack[:0]
		}
	}
	return stack, nil
}

// 重编码指令序列，并按映射改写子程序调用操作数（新旧偏置不同）。
// 入参: instrs 为指令序列；remap 提供旧局部/全局子程序索引 → 新索引与新旧偏置；
// remap 为 nil 时（无调用）原样重编码
// 出参: 重编码后的 charstring 字节
func encodeCharstring(instrs []csInstr, remap *subrRemap) ([]byte, error) {
	out := make([]byte, 0, 64)
	for _, in := range instrs {
		args := in.args
		if remap != nil && (in.op == csOpCallSubr || in.op == csOpCallGSubr) {
			if len(args) == 0 || args[len(args)-1].fixed {
				return nil, fmt.Errorf("ttf: charstring 子程序号经栈继承自调用方程序块，不支持改写")
			}
			args = append([]csNum(nil), args...)
			v := args[len(args)-1].i
			var ni int
			var ok bool
			if in.op == csOpCallSubr {
				ni, ok = remap.locals[int(v)+remap.oldLocalBias]
				v = int64(ni - remap.newLocalBias)
			} else {
				ni, ok = remap.globals[int(v)+remap.oldGlobalBias]
				v = int64(ni - remap.newGlobalBias)
			}
			if !ok {
				return nil, fmt.Errorf("ttf: charstring 引用了未纳入闭包的子程序")
			}
			args[len(args)-1] = csNum{i: v}
		}
		for _, a := range args {
			out = encodeCSNum(out, a)
		}
		if in.op&0x0C00 != 0 {
			out = append(out, 12, byte(in.op&0xFF))
		} else {
			out = append(out, byte(in.op))
		}
		out = append(out, in.mask...)
	}
	return out, nil
}

// 追加编码一个 charstring 操作数（整数取最短形式，定点按 255 原样重编码）。
func encodeCSNum(out []byte, n csNum) []byte {
	if n.fixed {
		v := int32(n.i)
		return append(out, 255, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
	v := n.i
	switch {
	case v >= -107 && v <= 107:
		return append(out, byte(v+139))
	case v >= 108 && v <= 1131:
		v -= 108
		return append(out, byte(v/256+247), byte(v%256))
	case v >= -1131 && v <= -108:
		v = -v - 108
		return append(out, byte(v/256+251), byte(v%256))
	case v >= -32768 && v <= 32767:
		return append(out, 28, byte(int16(v)>>8), byte(int16(v)))
	default: // 超出 int16 的整数按 16.16 定点编码
		f := int32(v) << 16
		return append(out, 255, byte(f>>24), byte(f>>16), byte(f>>8), byte(f))
	}
}

// 返回 Subrs 数量为 count 时 charstring 调用操作数的偏置。
func subrBias(count int) int {
	switch {
	case count < 1240:
		return 107
	case count < 33900:
		return 1131
	default:
		return 32768
	}
}

// subrRemap 记录子程序重编号映射与各 Subrs 集合的新旧偏置。
type subrRemap struct {
	locals        map[int]int // 旧局部子程序索引 → 新索引（按所属 FD 区分使用）
	globals       map[int]int
	oldLocalBias  int
	oldGlobalBias int
	newLocalBias  int
	newGlobalBias int
}

// 返回集合键的升序序号映射（新索引按旧索引升序分配）。
func sortKeys(set map[int]bool) map[int]int {
	keys := make([]int, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	m := make(map[int]int, len(keys))
	for i, k := range keys {
		m[k] = i
	}
	return m
}
