package color

import "testing"

func TestColorOps(t *testing.T) {
	cases := []struct {
		c                             Color
		operands, fill, stroke, space string
	}{
		{Gray(0.5), "0.5", "g", "G", "DeviceGray"},
		{RGB{1, 0, 0.5}, "1 0 0.5", "rg", "RG", "DeviceRGB"},
		{CMYK{0, 1, 1, 0}, "0 1 1 0", "k", "K", "DeviceCMYK"},
	}
	for _, c := range cases {
		if c.c.Operands() != c.operands || c.c.FillOp() != c.fill || c.c.StrokeOp() != c.stroke || c.c.Space() != c.space {
			t.Errorf("%v: got %q %q %q %q", c.c, c.c.Operands(), c.c.FillOp(), c.c.StrokeOp(), c.c.Space())
		}
	}
}

func TestConstructors(t *testing.T) {
	if RGB255(255, 0, 128).Operands() != "1 0 0.5019607843137255" {
		t.Errorf("RGB255 = %q", RGB255(255, 0, 128).Operands())
	}
	if Hex(0xFF0000) != (RGB{1, 0, 0}) {
		t.Errorf("Hex = %v", Hex(0xFF0000))
	}
	// 越界分量被截断
	if (RGB{2, -1, 0}).Operands() != "2 -1 0" { // RGB 结构体本身不截断
		t.Errorf("raw RGB not clamped by design")
	}
	if RGBf(2, -1, 0).Operands() != "1 0 0" {
		t.Errorf("RGBf clamp = %q", RGBf(2, -1, 0).Operands())
	}
	if Gray(2).Operands() != "1" {
		t.Errorf("Gray clamp = %q", Gray(2).Operands())
	}
}
