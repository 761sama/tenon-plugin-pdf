package image

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func makePNG(t *testing.T, withAlpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			a := uint8(255)
			if withAlpha && x == 0 {
				a = 128
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodePNG(t *testing.T) {
	im, err := Decode(bytes.NewReader(makePNG(t, false)))
	if err != nil {
		t.Fatal(err)
	}
	if im.Width != 4 || im.Height != 3 {
		t.Errorf("size = %dx%d", im.Width, im.Height)
	}
	if im.Filter != "FlateDecode" || im.ColorSpace != "DeviceRGB" {
		t.Errorf("filter=%s cs=%s", im.Filter, im.ColorSpace)
	}
	if im.SMask != nil {
		t.Error("opaque PNG should have no SMask")
	}
}

func TestDecodePNGWithAlpha(t *testing.T) {
	im, err := Decode(bytes.NewReader(makePNG(t, true)))
	if err != nil {
		t.Fatal(err)
	}
	if im.SMask == nil {
		t.Fatal("transparent PNG should have SMask")
	}
	if im.SMask.ColorSpace != "DeviceGray" || im.SMask.Width != 4 {
		t.Errorf("bad SMask: %+v", im.SMask)
	}
}

func TestDecodeGrayPNG(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	im, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if im.ColorSpace != "DeviceGray" {
		t.Errorf("gray PNG color space = %s", im.ColorSpace)
	}
}

func TestDecodeJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	im, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if im.Width != 16 || im.Height != 8 {
		t.Errorf("jpeg size = %dx%d", im.Width, im.Height)
	}
	if im.Filter != "DCTDecode" || im.ColorSpace != "DeviceRGB" {
		t.Errorf("filter=%s cs=%s", im.Filter, im.ColorSpace)
	}
	// JPEG 数据应原样保留
	if !bytes.Equal(im.Data, data) {
		t.Error("JPEG data not preserved")
	}
}

func TestStreamDict(t *testing.T) {
	im, err := Decode(bytes.NewReader(makePNG(t, true)))
	if err != nil {
		t.Fatal(err)
	}
	st := im.Stream(testRef{})
	s := string(st.Encode(nil))
	for _, want := range []string{
		"/Type /XObject", "/Subtype /Image", "/Width 4", "/Height 3",
		"/ColorSpace /DeviceRGB", "/BitsPerComponent 8", "/Filter /FlateDecode", "/SMask 9 0 R",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("stream dict missing %q", want)
		}
	}
}

type testRef struct{}

func (testRef) Encode(dst []byte) []byte { return append(dst, "9 0 R"...) }

func TestDecodeUnknown(t *testing.T) {
	if _, err := Decode(strings.NewReader("not an image at all")); err == nil {
		t.Error("expected error for unknown format")
	}
}
