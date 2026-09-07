package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCJKDemo 生成中文演示 PDF 并用外部工具验证中文渲染与文本反查。
func TestCJKDemo(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cjk.pdf")
	if err := cmdCJK([]string{"-o", out, "-rows", "80"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("输出文件异常: %v", err)
	}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext 不可用，跳过文本反查验证")
	}
	txt, err := exec.Command("pdftotext", out, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	// 中文文本应可通过 ToUnicode 反查
	for _, want := range []string{"采购订单", "序号", "品名", "内六角螺栓", "不锈钢", "合计", "授权签字"} {
		if !strings.Contains(string(txt), want) {
			t.Errorf("提取文本缺少 %q", want)
		}
	}
	// 80 行应跨页，且续页重复表头
	if n := strings.Count(string(txt), "序号"); n < 2 {
		t.Errorf("表头重复次数 = %d, 应 >= 2", n)
	}
}
