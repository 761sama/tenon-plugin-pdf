package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/security"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// cmdEncrypt 生成加密演示 PDF。
func cmdEncrypt(args []string) error {
	fs := flag.NewFlagSet("encrypt", flag.ExitOnError)
	out := fs.String("o", "encrypted-demo.pdf", "输出文件")
	user := fs.String("user", "", "用户密码（打开密码）")
	owner := fs.String("owner", "", "所有者密码（权限密码）")
	aes256 := fs.Bool("aes256", false, "使用 AES-256（默认 AES-128）")
	noCopy := fs.Bool("no-copy", false, "禁止复制/提取内容")
	noPrint := fs.Bool("no-print", false, "禁止打印")
	noModify := fs.Bool("no-modify", false, "禁止修改")
	noAnnotate := fs.Bool("no-annotate", false, "禁止批注")
	var recipPaths []string
	fs.Func("recip", "收件人 PEM 证书路径（可重复；设置后改用公钥证书加密 PubSec）",
		func(s string) error { recipPaths = append(recipPaths, s); return nil })
	fs.Parse(args)

	perms := security.PermAll
	if *noCopy {
		perms &^= security.PermCopy
	}
	if *noPrint {
		perms &^= security.PermPrint
	}
	if *noModify {
		perms &^= security.PermModify
	}
	if *noAnnotate {
		perms &^= security.PermAnnotate
	}
	level := security.AES128
	if *aes256 {
		level = security.AES256
	}

	doc := pdf.New()
	doc.Info().Title = "Encrypted Demo"
	doc.Info().Creator = "tenon-pdf encrypt"

	mode := "密码"
	if len(recipPaths) > 0 {
		var rcpts []security.Recipient
		for _, p := range recipPaths {
			pemData, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			cert, err := sign.ParseCertPEM(pemData)
			if err != nil {
				return fmt.Errorf("解析收件人证书 %s 失败: %w", p, err)
			}
			rcpts = append(rcpts, security.Recipient{Certificate: cert, Permissions: perms})
		}
		doc.SetPubKeyEncryption(security.PubKeyOptions{
			Recipients: rcpts,
			Level:      level,
		})
		mode = fmt.Sprintf("公钥证书（%d 个收件人）", len(rcpts))
	} else {
		doc.SetEncryption(security.Options{
			UserPassword:  *user,
			OwnerPassword: *owner,
			Permissions:   perms,
			Level:         level,
		})
	}
	p := doc.AddPage(page.A4)
	p.DrawText(font.HelveticaBold, 20, 72, 770, "Encrypted Document Demo")
	p.DrawText(font.Helvetica, 11, 72, 740, "This PDF is encrypted with the Standard Security Handler.")
	alg := "AES-128 (R4)"
	if *aes256 {
		alg = "AES-256 (R6)"
	}
	if len(recipPaths) > 0 {
		alg = "AES-128 (PubSec adbe.pkcs7.s5)"
		if *aes256 {
			alg = "AES-256 (PubSec adbe.pkcs7.s5)"
		}
	}
	p.DrawText(font.Helvetica, 11, 72, 724, "Algorithm: "+alg+", mode: "+mode)
	p.DrawText(font.Helvetica, 11, 72, 708, "Open with the user password; permissions enforced by the owner password.")
	return doc.SaveFile(*out)
}
