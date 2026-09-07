package pdf_test

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// TestVisibleSignature 可见签名：Rect 非零 → 部件带 /AP 正常外观流，
// 渲染后签名矩形区域内应有墨迹；签名本身照常可验。
func TestVisibleSignature(t *testing.T) {
	doc := pdf.New()
	doc.Info().Title = "Visible Signature Demo"
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "Contract with visible signature")
	doc.SetSignature(&sign.Field{
		Reason:     "合同签署",
		Location:   "Shanghai",
		SignerName: "Zhang San",
		Rect:       [4]float64{72, 600, 272, 660},
	})
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	// 结构断言：外观流与可见标志
	if !bytes.Contains(data, []byte("/AP")) {
		t.Error("可见签名应带 /AP 外观")
	}
	if !bytes.Contains(data, []byte("/Subtype /Form")) {
		t.Error("外观应为 Form XObject")
	}
	if !bytes.Contains(data, []byte("Digitally signed by: Zhang San")) {
		t.Error("外观流应包含签署人名称")
	}

	key, _ := sign.GenerateRSAKey()
	cert, _ := sign.GenerateSelfSigned("Zhang San", key)
	signed, err := sign.Sign(data, sign.Options{Signer: key, Certificate: cert})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("可见签名应有效: %s", res.Message)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "visible.pdf")
	os.WriteFile(path, signed, 0644)

	// pdfsig 外部验签
	if _, err := exec.LookPath("pdfsig"); err == nil {
		out, _ := exec.Command("pdfsig", path).CombinedOutput()
		if !strings.Contains(string(out), "Signature is Valid") {
			t.Errorf("pdfsig 验证失败:\n%s", out)
		}
	}

	// gs 渲染：签名矩形区域内应有非白像素（外观确实绘制）。
	// 注：poppler 不渲染签名单元的批注外观（外部工具行为），故用 ghostscript。
	if _, err := exec.LookPath("gs"); err == nil {
		gsOut := filepath.Join(dir, "gs.png")
		if out, err := exec.Command("gs", "-o", gsOut, "-sDEVICE=png16m", "-r72",
			"-dBATCH", "-dNOPAUSE", "-dShowAnnots=true", path).CombinedOutput(); err != nil {
			t.Fatalf("gs 渲染失败: %v\n%s", err, out)
		}
		f, err := os.Open(gsOut)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		// A4@72dpi ≈ 595×842 像素；PDF y 轴向上 → 图像 y = 842 - pdfY
		// 矩形 [72,600,272,660] → 图像行约 182..242，列 72..272
		ink := 0
		for y := 185; y < 240 && y < img.Bounds().Max.Y; y++ {
			for x := 75; x < 270 && x < img.Bounds().Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
					ink++
				}
			}
		}
		if ink == 0 {
			t.Error("签名矩形区域内未渲染出任何内容（外观未生效）")
		}
		t.Logf("签名区域非白像素数: %d", ink)
	}
}
