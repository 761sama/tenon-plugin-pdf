package image

import (
	"bytes"
	"fmt"
	stdimage "image"
	_ "image/gif" // 注册 GIF 解码器
	_ "image/png" // 注册 PNG 解码器
)

// decodeRaster 解码 PNG/GIF 为 8 位灰度或 RGB，并提取透明通道为 SMask。
func decodeRaster(data []byte, format string) (*Image, error) {
	src, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image: 解码 %s 失败: %w", format, err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	// 第一遍：检测是否含透明像素、是否纯灰度
	hasAlpha, allGray := false, true
	for y := 0; y < h && (allGray || !hasAlpha); y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if byte(a>>8) != 0xff {
				hasAlpha = true
			}
			if r>>8 != g>>8 || g>>8 != bl>>8 {
				allGray = false
			}
		}
	}

	// 第二遍：生成像素数据
	colorSpace := "DeviceRGB"
	stride := 3
	if allGray {
		colorSpace = "DeviceGray"
		stride = 1
	}
	raw := make([]byte, 0, w*h*stride)
	var alpha []byte
	if hasAlpha {
		alpha = make([]byte, 0, w*h)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if allGray {
				raw = append(raw, byte(r>>8))
			} else {
				raw = append(raw, byte(r>>8), byte(g>>8), byte(bl>>8))
			}
			if hasAlpha {
				alpha = append(alpha, byte(a>>8))
			}
		}
	}

	comp, err := flate(raw)
	if err != nil {
		return nil, err
	}
	im := &Image{
		Width: w, Height: h,
		ColorSpace: colorSpace, BitsPerComponent: 8,
		Filter: "FlateDecode", Data: comp,
	}

	if hasAlpha {
		compA, err := flate(alpha)
		if err != nil {
			return nil, err
		}
		im.SMask = &Image{
			Width: w, Height: h,
			ColorSpace: "DeviceGray", BitsPerComponent: 8,
			Filter: "FlateDecode", Data: compA,
		}
	}
	return im, nil
}
