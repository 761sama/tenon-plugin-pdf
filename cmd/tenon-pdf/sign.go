package main

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"flag"
	"fmt"
	"os"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/security"
	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// cmdSign 生成演示文档并进行数字签名。
func cmdSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	out := fs.String("o", "signed-demo.pdf", "输出文件")
	in := fs.String("in", "", "签署既有 PDF 文件（含第三方生成；为空则生成演示文档）")
	certPath := fs.String("cert", "", "PEM 证书路径（缺省自动生成自签名证书）")
	keyPath := fs.String("key", "", "PEM 私钥路径")
	selfSign := fs.String("selfsign", "", "生成自签名证书的 CN（缺省 demo signer）")
	reason := fs.String("reason", "Document approval", "签名原因")
	location := fs.String("location", "", "签名地点")
	ecdsa := fs.Bool("ecdsa", false, "使用 ECDSA P-256（默认 RSA 2048）")
	saveKeys := fs.String("save-keys", "", "将自签名证书/私钥保存为指定前缀的 .pem 文件")
	user := fs.String("user", "", "用户密码（设置后文档同时加密）")
	owner := fs.String("owner", "", "所有者密码（设置后文档同时加密）")
	aes256 := fs.Bool("aes256", false, "加密时使用 AES-256（默认 AES-128）")
	var chainPaths []string
	fs.Func("chain", "中间证书 PEM 路径（可重复，嵌入 CMS 供链式验证）",
		func(s string) error { chainPaths = append(chainPaths, s); return nil })
	tsaURL := fs.String("tsa", "", "RFC 3161 时间戳服务器 URL（嵌入签名时间戳令牌）")
	multi := fs.Int("multi", 0, "多重签名演示：共签署 N 人（第 1 人用 -cert/-key 或自签名，其余自动生成会签人）")
	rectStr := fs.String("rect", "", "可见签名矩形 x0,y0,x1,y1（如 350,100,560,160），非空即渲染可见外观")
	signerName := fs.String("signer-name", "", "可见签名外观中显示的签署人名称")
	dss := fs.Bool("dss", false, "LTV：DSS 字典嵌入签名证书与 -chain 链（CRL/OCSP 可用 -crl/-ocsp 追加）")
	var crlPaths, ocspPaths []string
	fs.Func("crl", "DER 编码 CRL 文件路径（可重复，嵌入 DSS）", func(s string) error { crlPaths = append(crlPaths, s); return nil })
	fs.Func("ocsp", "DER 编码 OCSP 响应文件路径（可重复，嵌入 DSS）", func(s string) error { ocspPaths = append(ocspPaths, s); return nil })
	fs.Parse(args)

	cert, key, err := loadOrGenerateKeys(*certPath, *keyPath, *selfSign, *ecdsa)
	if err != nil {
		return err
	}
	if *saveKeys != "" {
		if err := os.WriteFile(*saveKeys+".cert.pem", sign.MarshalCertPEM(cert), 0644); err != nil {
			return err
		}
		kb, err := sign.MarshalKeyPEM(key)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*saveKeys+".key.pem", kb, 0600); err != nil {
			return err
		}
		fmt.Println("证书与私钥已保存:", *saveKeys+".cert.pem,", *saveKeys+".key.pem")
	}

	doc := pdf.New()
	doc.Info().Title = "Signed Demo"
	doc.Info().Creator = "tenon-pdf sign"
	p := doc.AddPage(page.A4)
	p.DrawText(font.HelveticaBold, 20, 72, 770, "Digitally Signed Document")
	p.DrawText(font.Helvetica, 11, 72, 740, "This PDF carries a PKCS#7/CMS detached signature (adbe.pkcs7.detached).")
	p.DrawText(font.Helvetica, 11, 72, 724, "ByteRange covers the entire document except the signature value.")
	p.DrawText(font.Helvetica, 11, 72, 708, "Verify with: tenon-pdf verify "+*out)
	sigField := &sign.Field{Reason: *reason, Location: *location, SignerName: *signerName}
	if *rectStr != "" {
		var r [4]float64
		if _, err := fmt.Sscanf(*rectStr, "%g,%g,%g,%g", &r[0], &r[1], &r[2], &r[3]); err != nil {
			return fmt.Errorf("-rect 格式应为 x0,y0,x1,y1: %w", err)
		}
		sigField.Rect = r
		if sigField.SignerName == "" {
			sigField.SignerName = cert.Subject.CommonName
		}
	}

	signOpts := sign.Options{Signer: key, Certificate: cert}
	for _, cp := range chainPaths {
		pemData, err := os.ReadFile(cp)
		if err != nil {
			return err
		}
		chain, err := sign.ParseCertChainPEM(pemData)
		if err != nil {
			return fmt.Errorf("解析中间证书 %s 失败: %w", cp, err)
		}
		signOpts.Chain = append(signOpts.Chain, chain...)
	}
	if *tsaURL != "" {
		signOpts.TSA = &sign.TSAOptions{URL: *tsaURL}
	}

	// -in：签署既有 PDF（含第三方生成），增量修订追加签名字段
	if *in != "" {
		if *user != "" || *owner != "" || *dss {
			return fmt.Errorf("-in 模式不支持加密与 DSS（既有文档不重写）")
		}
		data, err := os.ReadFile(*in)
		if err != nil {
			return err
		}
		signed, err := sign.SignExisting(data, sigField, signOpts)
		if err != nil {
			return err
		}
		for i := 2; i <= *multi; i++ {
			signed, err = sign.SignExisting(signed, &sign.Field{Reason: fmt.Sprintf("会签人 %d", i)},
				sign.Options{Signer: key, Certificate: cert})
			if err != nil {
				return err
			}
			fmt.Printf("会签人 %d 已签署\n", i)
		}
		return os.WriteFile(*out, signed, 0644)
	}

	doc.SetSignature(sigField)
	if *user != "" || *owner != "" {
		level := security.AES128
		if *aes256 {
			level = security.AES256
		}
		doc.SetEncryption(security.Options{
			UserPassword:  *user,
			OwnerPassword: *owner,
			Level:         level,
		})
	}

	// DSS（LTV 证书部分）须在序列化前嵌入
	if *dss {
		d := pdf.DSSData{Certs: append([]*x509.Certificate{cert}, signOpts.Chain...)}
		for _, cp := range crlPaths {
			b, err := os.ReadFile(cp)
			if err != nil {
				return err
			}
			d.CRLs = append(d.CRLs, b)
		}
		for _, op := range ocspPaths {
			b, err := os.ReadFile(op)
			if err != nil {
				return err
			}
			d.OCSPs = append(d.OCSPs, b)
		}
		doc.SetDSS(d)
	}
	data, err := doc.Bytes()
	if err != nil {
		return err
	}
	signed, err := sign.Sign(data, signOpts)
	if err != nil {
		return err
	}
	// 多重签名：增量修订依次追加字段并签署
	for i := 2; i <= *multi; i++ {
		signed, err = sign.AppendSignatureField(signed, &sign.Field{Reason: fmt.Sprintf("会签人 %d", i)})
		if err != nil {
			return err
		}
		k, err := sign.GenerateRSAKey()
		if err != nil {
			return err
		}
		c, err := sign.GenerateSelfSigned(fmt.Sprintf("会签人 %d", i), k)
		if err != nil {
			return err
		}
		signed, err = sign.Sign(signed, sign.Options{Signer: k, Certificate: c})
		if err != nil {
			return err
		}
		fmt.Printf("会签人 %d 已签署\n", i)
	}
	return os.WriteFile(*out, signed, 0644)
}

// cmdVerify 校验 PDF 数字签名。
func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	rootPath := fs.String("root", "", "受信根证书 PEM（设置后启用证书信任链验证）")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("需要一个待校验的 PDF 文件")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	// 多重签名：逐个校验
	if bytes.Count(data, []byte("/ByteRange")) > 1 {
		return cmdVerifyAll(data, *rootPath)
	}
	var res *sign.VerifyResult
	if *rootPath != "" {
		pemData, err := os.ReadFile(*rootPath)
		if err != nil {
			return err
		}
		roots, err := sign.ParseCertChainPEM(pemData)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		for _, c := range roots {
			pool.AddCert(c)
		}
		res, err = sign.VerifyWithOptions(data, &sign.VerifyOptions{Roots: pool})
	} else {
		res, err = sign.Verify(data)
	}
	if err != nil {
		return err
	}
	fmt.Println("签名者:  ", res.Signer)
	fmt.Println("签名时间:", res.SigningTime)
	if res.Reason != "" {
		fmt.Println("签名原因:", res.Reason)
	}
	if res.ChainChecked {
		fmt.Println("证书链:  ", res.ChainMessage)
	}
	if res.Timestamp != nil {
		fmt.Println("时间戳:  ", res.Timestamp.Time, "(TSA)")
		if !res.Timestamp.ImprintValid {
			fmt.Println("警告: 时间戳 imprint 与签名值摘要不一致")
		}
	}
	if res.Valid && (!res.ChainChecked || res.ChainValid) {
		fmt.Println("校验结果: 签名有效，文档完整")
		return nil
	}
	if !res.Valid {
		fmt.Println("校验结果: 签名无效 ——", res.Message)
		return fmt.Errorf("签名校验失败")
	}
	fmt.Println("校验结果: 签名有效但证书链不受信 ——", res.ChainMessage)
	return fmt.Errorf("证书链验证失败")
}

// cmdVerifyAll 逐个校验多重签名（会签）。
func cmdVerifyAll(data []byte, rootPath string) error {
	var results []*sign.VerifyResult
	var err error
	if rootPath != "" {
		pemData, err2 := os.ReadFile(rootPath)
		if err2 != nil {
			return err2
		}
		roots, err2 := sign.ParseCertChainPEM(pemData)
		if err2 != nil {
			return err2
		}
		pool := x509.NewCertPool()
		for _, c := range roots {
			pool.AddCert(c)
		}
		results, err = sign.VerifyAllWithOptions(data, &sign.VerifyOptions{Roots: pool})
	} else {
		results, err = sign.VerifyAll(data)
	}
	if err != nil {
		return err
	}
	fmt.Printf("共 %d 个签名：\n", len(results))
	allOK := true
	for i, r := range results {
		status := "有效"
		if !r.Valid {
			status = "无效 —— " + r.Message
			allOK = false
		}
		if r.ChainChecked && !r.ChainValid {
			status += "（证书链不受信）"
			allOK = false
		}
		fmt.Printf("  #%d %s  %s  %s\n", i+1, r.Signer, r.SigningTime.Format("2006-01-02 15:04:05"), status)
	}
	if !allOK {
		return fmt.Errorf("存在无效签名")
	}
	return nil
}

// loadOrGenerateKeys 加载或生成证书与私钥。
func loadOrGenerateKeys(certPath, keyPath, selfSignCN string, useECDSA bool) (*x509.Certificate, crypto.Signer, error) {
	if certPath != "" && keyPath != "" {
		cb, err := os.ReadFile(certPath)
		if err != nil {
			return nil, nil, err
		}
		kb, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, nil, err
		}
		cert, err := sign.ParseCertPEM(cb)
		if err != nil {
			return nil, nil, err
		}
		key, err := sign.ParseKeyPEM(kb)
		if err != nil {
			return nil, nil, err
		}
		return cert, key, nil
	}
	// 自动生成自签名证书
	var key crypto.Signer
	var err error
	if useECDSA {
		key, err = sign.GenerateECDSAKey()
	} else {
		key, err = sign.GenerateRSAKey()
	}
	if err != nil {
		return nil, nil, err
	}
	if selfSignCN == "" {
		selfSignCN = "tenon-pdf demo signer"
	}
	cert, err := sign.GenerateSelfSigned(selfSignCN, key)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}
