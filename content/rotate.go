package content

import "math"

// sinCos 拆分出来便于测试与复用。
func sinCos(rad float64) (sin, cos float64) {
	sin, cos = math.Sincos(rad)
	// 消除浮点噪声，保证常见角度输出干净
	const eps = 1e-12
	if math.Abs(sin) < eps {
		sin = 0
	}
	if math.Abs(cos) < eps {
		cos = 0
	}
	return sin, cos
}
