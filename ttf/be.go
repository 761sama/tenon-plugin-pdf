package ttf

import "encoding/binary"

// 偏移式大端读写辅助。

func u16(b []byte, o int) uint16 { return binary.BigEndian.Uint16(b[o:]) }

func u32(b []byte, o int) uint32 { return binary.BigEndian.Uint32(b[o:]) }

func putU16(b []byte, o int, v uint16) { binary.BigEndian.PutUint16(b[o:], v) }

func putU32(b []byte, o int, v uint32) { binary.BigEndian.PutUint32(b[o:], v) }
