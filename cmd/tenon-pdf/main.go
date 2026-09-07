// tenon-pdf 是 tenon-plugin-pdf 库的命令行入口。
//
// 用法：
//
//	tenon-pdf demo [-o out.pdf]            生成功能演示 PDF
//	tenon-pdf text [-o out.pdf] <in.txt>   将文本文件转换为 PDF
//	tenon-pdf img  [-o out.pdf] <图片...>  将 JPEG/PNG/GIF 图片合成为 PDF
//	tenon-pdf table [-o out.pdf] [-rows 60]  生成合同样式表格演示 PDF
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func usage() {
	fmt.Fprintf(os.Stderr, `tenon-pdf %s — PDF 生成工具（gopkg.761sama.com/tenon-plugin-pdf）

用法：
  tenon-pdf demo [-o out.pdf]           生成功能演示 PDF（默认 demo.pdf）
  tenon-pdf text [-o out.pdf] [选项] <in.txt>
      -font Helvetica|Times-Roman|Courier  正文字体（默认 Helvetica）
      -size 12                             字号
      -pagesize A4|A5|Letter               页面尺寸
  tenon-pdf img  [-o out.pdf] <图片...>   每张图片一页（JPEG/PNG/GIF）
  tenon-pdf table [-o out.pdf] [-rows 60] 合同样式表格演示（跨页重复表头）
  tenon-pdf cjk [-o out.pdf] [-rows 120] [-font 完整字体.ttf]
                                         中文采购单演示（思源黑体子集嵌入）
  tenon-pdf json [-o out.pdf] <doc.json> 从 JSON 描述生成 PDF（格式见 doc/json-format.md）
      -font id=字体.ttf|字体.ttc@序号      注册字体（可重复；JSON 仅按 id 引用，禁止路径）
      -font id=builtin:Helvetica-Bold    注册标准 14 字体
  tenon-pdf encrypt [-o out.pdf] [-user PW] [-owner PW] [-aes256]
                    [-no-copy] [-no-print] [-no-modify] [-no-annotate]
                    [-recip 收件人证书.pem]...           加密演示（密码或公钥证书 PubSec）
  tenon-pdf sign [-o out.pdf] [-cert c.pem -key k.pem | -selfsign CN]
                    [-reason R] [-ecdsa] [-save-keys 前缀]
                    [-user PW] [-aes256]               与加密组合
                    [-chain ca.pem]...                 嵌入中间证书链
                    [-tsa TSA_URL]                     RFC 3161 时间戳
                    [-multi N]                         多重签名会签演示
                    [-rect x0,y0,x1,y1] [-signer-name NAME]  可见签名外观
                    [-dss] [-crl f.der]... [-ocsp f.der]...  LTV（DSS 验证材料）
                                         数字签名演示（PKCS#7/CMS）
  tenon-pdf verify [-root ca.pem] <signed.pdf>  校验签名（可多签、可验证书链）
  tenon-pdf version                      打印版本
`, version)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "demo":
		err = cmdDemo(os.Args[2:])
	case "text":
		err = cmdText(os.Args[2:])
	case "img":
		err = cmdImg(os.Args[2:])
	case "table":
		err = cmdTable(os.Args[2:])
	case "cjk":
		err = cmdCJK(os.Args[2:])
	case "json":
		err = cmdJSON(os.Args[2:])
	case "encrypt":
		err = cmdEncrypt(os.Args[2:])
	case "sign":
		err = cmdSign(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("tenon-pdf", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n", os.Args[1])
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
