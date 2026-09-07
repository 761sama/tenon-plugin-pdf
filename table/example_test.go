package table_test

import (
	"fmt"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/table"
	"gopkg.761sama.com/tenon-plugin-pdf/text"
)

// ExampleTable_mergedCells 演示单元格合并（跨列/跨行）与内容自适应列宽。
func ExampleTable_mergedCells() {
	doc := pdf.New()
	p := doc.AddPage(page.A4)

	tbl := table.New(
		table.Column{Title: "Category"},                       // 自动列宽
		table.Column{Title: "Item"},                           // 自动列宽
		table.Column{Title: "Amount", Align: text.AlignRight}, // 自动列宽
	)
	tbl.AutoWidth = true // 按内容测算列宽

	// rowspan：Materials 跨两行；colspan：TOTAL 跨两列
	tbl.AddRowCells(table.C("Materials").Span(1, 2), table.C("Steel"), table.C("1000.00"))
	tbl.AddRowCells(table.C("Cement"), table.C("500.00"))
	tbl.AddRowCells(table.C("Labor"), table.C("Site work"), table.C("800.00"))
	tbl.AddRowCells(table.C("TOTAL").Span(2, 1), table.C("2300.00"))

	newPage := func() (*page.Page, float64) { return doc.AddPage(page.A4), 780 }
	_, endY := tbl.Draw(p, 72, 700, 450, 72, newPage)
	fmt.Println("table ends at y =", int(endY))
	// Output: table ends at y = 602
}
