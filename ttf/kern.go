package ttf

// kern 表（字距调整）解析。
//
// 只实现应用最广的 format 0（左右字形对的步进修正值列表）；
// 其他 format（状态机等）罕见且复杂，直接忽略。MS 版（version 0，
// 子表头 6 字节，format 在 coverage 高字节）与 Apple 版（version 1.0，
// 子表头 8 字节，format 在 coverage 低字节）布局不同，分别处理。
//
// 注：GPOS 型字距（kern 表的现代替代品，基于特性/脚本/定位规则）未实现——
// 解析复杂度高出数倍；多数常见字体（DejaVu/Liberation/Lato/思源等）
// 仍带 kern 表，覆盖面足够。

// parseKern 解析 kern 表；无表或无支持的子表时 kernPairs 为 nil。
func (f *Font) parseKern() {
	d, err := f.table("kern")
	if err != nil || len(d) < 4 {
		return
	}
	version := u32(d, 0)
	switch {
	case version>>16 == 0: // MS version 0：uint16 version + uint16 nTables
		f.parseKernSubtables(d, 4, int(u16(d, 2)), false)
	case version == 0x00010000: // Apple version 1.0：uint32 nTables
		if len(d) >= 8 {
			f.parseKernSubtables(d, 8, int(u32(d, 4)), true)
		}
	}
}

// parseKernSubtables 遍历子表，仅收取 format 0 的横排（非 cross-stream）子表。
func (f *Font) parseKernSubtables(d []byte, start, nTables int, apple bool) {
	p := start
	for i := 0; i < nTables && p < len(d); i++ {
		var hdrLen, length, coverage int
		if apple {
			if p+8 > len(d) {
				return
			}
			hdrLen, length, coverage = 8, int(u32(d, p)), int(u16(d, p+4))
		} else {
			if p+6 > len(d) {
				return
			}
			hdrLen, length, coverage = 6, int(u16(d, p+2)), int(u16(d, p+4))
		}
		if length <= 0 || p+length > len(d) {
			return
		}
		var format int
		var horizontal bool
		if apple {
			format = coverage & 0xFF
			horizontal = coverage&0x4000 == 0 // 0x4000 = cross-stream
		} else {
			format = coverage >> 8
			horizontal = coverage&0x1 != 0
		}
		if format == 0 && horizontal {
			f.parseKernFormat0(d[p+hdrLen : p+length])
		}
		p += length
	}
}

// parseKernFormat0 读取 format 0 子表（nPairs 起）的字距对。
func (f *Font) parseKernFormat0(d []byte) {
	if len(d) < 8 {
		return
	}
	nPairs := int(u16(d, 0))
	d = d[8:] // 跳过 nPairs/searchRange/entrySelector/rangeShift
	if len(d) < nPairs*6 {
		nPairs = len(d) / 6
	}
	if f.kernPairs == nil {
		f.kernPairs = make(map[uint32]int16, nPairs)
	}
	for i := 0; i < nPairs; i++ {
		o := i * 6
		key := uint32(u16(d, o))<<16 | uint32(u16(d, o+2))
		f.kernPairs[key] = int16(u16(d, o+4))
	}
}

// Kern 返回左右两字形的字距修正值（字体单位，通常 ≤ 0）。
// 无 kern 表或无该字距对时返回 0。
func (f *Font) Kern(left, right uint16) int {
	if f.kernPairs == nil {
		return 0
	}
	return int(f.kernPairs[uint32(left)<<16|uint32(right)])
}

// HasKerning 报告字体是否带可用的 kern 字距表。
func (f *Font) HasKerning() bool { return len(f.kernPairs) > 0 }
