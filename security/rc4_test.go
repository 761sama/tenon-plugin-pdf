package security

import (
	"bytes"
	"testing"
)

func TestRC4Vector(t *testing.T) {
	// RC4 已知测试向量：Key="Key"，明文 "Plaintext"
	got := rc4([]byte("Key"), []byte("Plaintext"))
	want := []byte{0xBB, 0xF3, 0x16, 0xE8, 0xD9, 0x40, 0xAF, 0x0A, 0xD3}
	if !bytes.Equal(got, want) {
		t.Errorf("RC4 = %x, want %x", got, want)
	}
	// 第二组向量：Key="Wiki"，明文 "pedia"
	got2 := rc4([]byte("Wiki"), []byte("pedia"))
	want2 := []byte{0x10, 0x21, 0xBF, 0x04, 0x20}
	if !bytes.Equal(got2, want2) {
		t.Errorf("RC4 = %x, want %x", got2, want2)
	}
}

func TestPadPassword(t *testing.T) {
	p := padPassword("abc")
	if len(p) != 32 || string(p[:3]) != "abc" || p[3] != 0x28 {
		t.Errorf("padPassword wrong: %x", p)
	}
	if len(padPassword(string(make([]byte, 64)))) != 32 {
		t.Error("长密码应截断为 32 字节")
	}
}

func TestPValue(t *testing.T) {
	h := &Handler{opts: Options{Permissions: PermAll}}
	if h.pValue() != -4 {
		t.Errorf("PermAll P = %d, want -4", h.pValue())
	}
	h2 := &Handler{opts: Options{Permissions: PermAll &^ PermCopy}}
	if h2.pValue() != -4&^int32(0x10) {
		t.Errorf("no-copy P = %d, want %d", h2.pValue(), -4&^int32(0x10))
	}
}
