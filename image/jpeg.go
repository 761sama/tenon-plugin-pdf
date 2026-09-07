package image

import "fmt"

// decodeJPEG 解析 JPEG 帧头获取尺寸与颜色分量，数据原样嵌入（DCTDecode）。
func decodeJPEG(data []byte) (*Image, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, fmt.Errorf("image: 非法 JPEG 数据")
	}
	im := &Image{Filter: "DCTDecode", BitsPerComponent: 8, Data: data}
	pos := 2
	adobeTransform := -1 // APP14 中的 transform 标志
	for pos+4 <= len(data) {
		if data[pos] != 0xff {
			pos++
			continue
		}
		marker := data[pos+1]
		pos += 2
		// 无长度段标记
		if marker == 0xd8 || marker == 0xd9 || (marker >= 0xd0 && marker <= 0xd7) || marker == 0x01 {
			continue
		}
		if pos+2 > len(data) {
			break
		}
		segLen := int(data[pos])<<8 | int(data[pos+1])
		if segLen < 2 || pos+segLen > len(data) {
			return nil, fmt.Errorf("image: JPEG 段损坏")
		}
		switch {
		case marker == 0xee && segLen >= 14 && string(data[pos+2:pos+7]) == "Adobe":
			adobeTransform = int(data[pos+2+11])
		case isSOF(marker):
			if segLen < 8 {
				return nil, fmt.Errorf("image: JPEG SOF 段过短")
			}
			im.Height = int(data[pos+3])<<8 | int(data[pos+4])
			im.Width = int(data[pos+5])<<8 | int(data[pos+6])
			components := int(data[pos+7])
			switch components {
			case 1:
				im.ColorSpace = "DeviceGray"
			case 3:
				im.ColorSpace = "DeviceRGB"
			case 4:
				im.ColorSpace = "DeviceCMYK"
				if adobeTransform == 0 {
					// Adobe 未做色彩变换的 CMYK 需要反解码
					im.Decode = []float64{1, 0, 1, 0, 1, 0, 1, 0}
				}
			default:
				return nil, fmt.Errorf("image: 不支持的 JPEG 分量数 %d", components)
			}
			return im, nil
		}
		pos += segLen
	}
	return nil, fmt.Errorf("image: JPEG 中未找到 SOF 段")
}

func isSOF(m byte) bool {
	switch m {
	case 0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf:
		return true
	}
	return false
}
