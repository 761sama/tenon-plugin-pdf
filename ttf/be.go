package ttf

import "encoding/binary"

// 偏移式大端读写辅助。

// 读取 b[o:] 处大端 uint16。
func u16(b []byte, o int) uint16 { return binary.BigEndian.Uint16(b[o:]) }

// 读取 b[o:] 处大端 uint32。
func u32(b []byte, o int) uint32 { return binary.BigEndian.Uint32(b[o:]) }

// 在 b[o:] 处写入大端 uint16。
func putU16(b []byte, o int, v uint16) { binary.BigEndian.PutUint16(b[o:], v) }

// 在 b[o:] 处写入大端 uint32。
func putU32(b []byte, o int, v uint32) { binary.BigEndian.PutUint32(b[o:], v) }
