package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/table"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// cjkSubsetFont 是随仓库提交的思源黑体（Noto Sans SC, OFL）子集字体，
// 仅含演示所需字符；由 tools/fontsubset 生成（见 assets/charset.txt）。
//
//go:embed assets/NotoSansSC-Subset.ttf
var cjkSubsetFont []byte

// cmdCJK 生成中文演示 PDF：中文采购单表格（子集嵌入字体、跨页重复表头）。
func cmdCJK(args []string) error {
	fs := flag.NewFlagSet("cjk", flag.ExitOnError)
	out := fs.String("o", "cjk-demo.pdf", "输出文件")
	rows := fs.Int("rows", 120, "内容行数")
	fontPath := fs.String("font", "", "完整 TTF/TTC 字体路径（默认使用内置子集字体）")
	fontIndex := fs.Int("fontindex", 0, "TTC 集合中的字体索引（.ttc 文件时有效）")
	fs.Parse(args)

	cjkFont, err := loadCJKFont(*fontPath, *fontIndex)
	if err != nil {
		return err
	}

	doc := pdf.New()
	doc.Info().Title = "采购订单 - 中文演示"
	doc.Info().Creator = "tenon-pdf cjk"

	const (
		margin   = 50.0
		topY     = 622.0
		bottomY  = 60.0
		contTopY = 790.0
	)
	tableW := page.A4.W - 2*margin

	p := doc.AddPage(page.A4)

	// 抬头
	p.SetFillColor(color.Hex(0x8a1f1f))
	p.DrawText(cjkFont, 20, margin, 800, "采购订单")
	p.SetFillColor(color.Black)
	p.DrawText(cjkFont, 10, margin, 780, "订单编号：PO-2026-0904")
	p.DrawText(cjkFont, 10, margin, 766, "日期：2026-09-04")
	p.DrawText(cjkFont, 10, 320, 780, "供应商：某精密工业用品有限公司")
	p.DrawText(cjkFont, 10, 320, 766, "收货地址：761 仓库 3 号月台")
	p.SetStrokeColor(color.Hex(0x8a1f1f)).SetLineWidth(1.5)
	p.Line(margin, 754, page.A4.W-margin, 754)
	p.TextBox(cjkFont, 9.5, margin, 748, tableW,
		"请按照合同 CT-2026-081 的条款与条件供应以下物品。所有物品须附出厂检验报告，包装须防潮防震，并于交货期内送达指定地点。",
		text.AlignLeft, 0)

	// 中文表格
	tbl := table.New(
		table.Column{Title: "序号", Width: 36, Align: text.AlignCenter},
		table.Column{Title: "品名", Width: 0},
		table.Column{Title: "规格", Width: 0},
		table.Column{Title: "数量", Width: 44, Align: text.AlignRight},
		table.Column{Title: "单价（元）", Width: 72, Align: text.AlignRight},
		table.Column{Title: "金额（元）", Width: 80, Align: text.AlignRight},
	)
	tbl.Font = cjkFont
	tbl.HeaderFont = cjkFont

	items := []struct {
		name, spec string
		price      float64
	}{
		{"内六角螺栓", "M8×30mm 不锈钢304", 0.12},
		{"六角螺母", "M8 不锈钢304", 0.08},
		{"平垫圈", "M8 不锈钢304", 0.03},
		{"深沟球轴承", "6204-2RS 20×47×14mm", 2.45},
		{"液压软管", "1/2英寸 3000PSI 长2米", 18.90},
		{"压力表", "0-250bar 表盘63mm", 12.75},
		{"电磁阀", "24VDC 1/4英寸NPT", 34.50},
		{"PLC模块", "16入16出 24VDC", 189.00},
		{"接近开关", "M12 PNP常开 4mm", 8.60},
		{"电缆接头", "M20×1.5 IP68", 1.15},
		{"接线端子", "2.5平方毫米 DIN导轨", 0.42},
		{"断路器", "16A 2P C曲线", 6.80},
	}
	total := 0.0
	for i := 1; i <= *rows; i++ {
		it := items[(i-1)%len(items)]
		qty := float64((i*7)%50 + 1)
		amount := qty * it.price
		total += amount
		tbl.AddRow(
			fmt.Sprintf("%d", i),
			it.name,
			it.spec,
			fmt.Sprintf("%.0f", qty),
			fmt.Sprintf("%.2f", it.price),
			fmt.Sprintf("%.2f", amount),
		)
	}
	tbl.AddRow("", "合计", "", "", "", fmt.Sprintf("%.2f", total))

	newPage := func() (*page.Page, float64) {
		return doc.AddPage(page.A4), contTopY
	}
	last, endY := tbl.Draw(p, margin, topY, tableW, bottomY, newPage)

	// 末页落款
	last.SetFillColor(color.Black)
	last.DrawText(cjkFont, 10, margin, endY-30, "授权签字：______________")
	last.DrawText(cjkFont, 10, 320, endY-30, "日期：______________")
	last.DrawText(cjkFont, 8, margin, endY-50,
		"本文件由 tenon-pdf cjk 生成：思源黑体（Noto Sans SC, OFL）子集嵌入，跨页自动重复表头。")

	return doc.SaveFile(*out)
}

// loadCJKFont 加载 CJK 字体：指定路径则加载完整字体（TTC 集合按 index 取成员），
// 否则用内置子集字体。
func loadCJKFont(path string, index int) (*font.CJKFont, error) {
	var f *font.CJKFont
	var err error
	if path != "" {
		f, err = font.LoadCJKCollectionFile(path, index)
		if err != nil {
			return nil, fmt.Errorf("加载字体 %s[%d]: %w", path, index, err)
		}
	} else {
		f, err = font.LoadCJK(bytes.NewReader(cjkSubsetFont))
		if err != nil {
			return nil, fmt.Errorf("内置子集字体损坏: %w", err)
		}
		f.SetName("NotoSansSC")
	}
	return f, nil
}
