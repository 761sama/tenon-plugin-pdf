// Package image 实现 PDF 图像 XObject（ISO 32000-1 §8.9）：
// JPEG 直接嵌入（DCTDecode），PNG/GIF 解码后以 FlateDecode 嵌入，
// 透明通道自动生成 SMask。
package image

import (
	"bytes"
	"compress/zlib"
	"fmt"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Image 一幅可嵌入 PDF 的图像。
type Image struct {
	Width  int // 像素宽
	Height int // 像素高
	// ColorSpace：DeviceGray / DeviceRGB / DeviceCMYK
	ColorSpace string
	// BitsPerComponent 每分量位数（当前恒为 8）。
	BitsPerComponent int
	// Filter 数据编码：DCTDecode（JPEG 原样）或 FlateDecode。
	Filter string
	// Data 已按 Filter 编码的图像数据。
	Data []byte
	// Decode 反解码数组（用于 Adobe CMYK JPEG），可为空。
	Decode []float64
	// SMask 透明蒙版（灰度图），可为空。
	SMask *Image
}

// Decode 从字节流识别并解码图像，支持 JPEG、PNG 与 GIF。
func Decode(r interface{ Read([]byte) (int, error) }) (*Image, error) {
	data, err := readAll(r)
	if err != nil {
		return nil, err
	}
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return decodeJPEG(data)
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return decodeRaster(data, "png")
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return decodeRaster(data, "gif")
	}
	return nil, fmt.Errorf("image: 无法识别的图像格式")
}

// Stream 生成图像 XObject 流；smask 为蒙版的间接引用，无蒙版传 nil。
func (im *Image) Stream(smask object.Object) *object.Stream {
	st := object.NewStream(im.Data)
	st.Dict.Set("Type", object.Name("XObject"))
	st.Dict.Set("Subtype", object.Name("Image"))
	st.Dict.Set("Width", object.Int(im.Width))
	st.Dict.Set("Height", object.Int(im.Height))
	st.Dict.Set("ColorSpace", object.Name(im.ColorSpace))
	st.Dict.Set("BitsPerComponent", object.Int(im.BitsPerComponent))
	if im.Filter != "" {
		st.Dict.Set("Filter", object.Name(im.Filter))
	}
	if len(im.Decode) > 0 {
		arr := make(object.Array, len(im.Decode))
		for i, v := range im.Decode {
			arr[i] = object.Real(v)
		}
		st.Dict.Set("Decode", arr)
	}
	if smask != nil {
		st.Dict.Set("SMask", smask)
	}
	return st
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}

// flate 以 zlib（FlateDecode）压缩数据。
func flate(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
