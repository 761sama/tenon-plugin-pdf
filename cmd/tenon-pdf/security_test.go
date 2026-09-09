package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/sign"
)

// TestEncryptCmd 加密子命令：生成 → pdfinfo 识别 → 密码提取文本。
func TestEncryptCmd(t *testing.T) {
	out := filepath.Join(t.TempDir(), "enc.pdf")
	if err := cmdEncrypt([]string{"-o", out, "-user", "u123", "-owner", "o456", "-aes256", "-no-copy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo 不可用")
	}
	info, err := exec.Command("pdfinfo", "-upw", "u123", out).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfinfo: %v\n%s", err, info)
	}
	if !strings.Contains(string(info), "AES-256") || !strings.Contains(string(info), "copy:no") {
		t.Errorf("加密参数未生效:\n%s", info)
	}
}

// TestSignExistingCmd 签署既有 PDF（-in）：无签名占位符的文档增量追加签署。
func TestSignExistingCmd(t *testing.T) {
	dir := t.TempDir()
	// 先造一份无签名字段的既有 PDF（本库生成；第三方产物由 sign 包单测覆盖）
	base := filepath.Join(dir, "base.pdf")
	if err := cmdText([]string{"-o", base, filepath.Join("..", "..", "dev.md")}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "signed.pdf")
	if err := cmdSign([]string{"-in", base, "-o", out, "-selfsign", "既有文档签署",
		"-rect", "350,100,560,160", "-signer-name", "Existing Signer"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(data)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Errorf("既有文档签名校验失败: %s", res.Message)
	}
	// 既有字节不被改动（增量修订）
	orig, _ := os.ReadFile(base)
	if len(data) <= len(orig) || string(data[:len(orig)]) != string(orig) {
		t.Error("签署既有文档应为纯增量追加")
	}
}

// TestMultiSignCmd 多重签名 CLI：-multi 3 依次会签 → verify 逐个校验通过。
func TestMultiSignCmd(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "multi.pdf")
	if err := cmdSign([]string{"-o", out, "-selfsign", "甲方", "-multi", "3"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdVerify([]string{out}); err != nil {
		t.Errorf("多重签名校验应通过: %v", err)
	}
}

// TestChainVerifyCmd 证书链 CLI 演示路径：根CA 签发签名证书 →
// sign -cert/-key 签名 → verify -root 链式验证通过；错误根证书 → 失败。
func TestChainVerifyCmd(t *testing.T) {
	dir := t.TempDir()
	rootKey, _ := sign.GenerateRSAKey()
	rootCert, _ := sign.GenerateSelfSigned("cli root CA", rootKey)
	signerKey, _ := sign.GenerateRSAKey()
	signerCert, _ := sign.GenerateSignedCertificate("cli chain signer", signerKey, rootCert, rootKey, false)

	rootPath := filepath.Join(dir, "root.pem")
	os.WriteFile(rootPath, sign.MarshalCertPEM(rootCert), 0644)
	certPath := filepath.Join(dir, "signer.pem")
	os.WriteFile(certPath, sign.MarshalCertPEM(signerCert), 0644)
	keyPath := filepath.Join(dir, "signer.key.pem")
	kb, _ := sign.MarshalKeyPEM(signerKey)
	os.WriteFile(keyPath, kb, 0600)

	out := filepath.Join(dir, "chain-signed.pdf")
	if err := cmdSign([]string{"-o", out, "-cert", certPath, "-key", keyPath}); err != nil {
		t.Fatal(err)
	}
	if err := cmdVerify([]string{"-root", rootPath, out}); err != nil {
		t.Errorf("链式验证应通过: %v", err)
	}

	// 错误根证书 → 链不受信 → 非零退出
	otherKey, _ := sign.GenerateRSAKey()
	otherRoot, _ := sign.GenerateSelfSigned("wrong root", otherKey)
	wrongPath := filepath.Join(dir, "wrong.pem")
	os.WriteFile(wrongPath, sign.MarshalCertPEM(otherRoot), 0644)
	if err := cmdVerify([]string{"-root", wrongPath, out}); err == nil {
		t.Error("错误根证书时链验证必须失败")
	}
}

func TestEncryptPubKeyCmd(t *testing.T) {
	dir := t.TempDir()
	key, err := sign.GenerateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := sign.GenerateSelfSigned("cli recipient", key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "r.pem")
	os.WriteFile(certPath, sign.MarshalCertPEM(cert), 0644)

	out := filepath.Join(dir, "pk.pdf")
	if err := cmdEncrypt([]string{"-o", out, "-recip", certPath}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "/Filter /Adobe.PubSec") || !strings.Contains(s, "/Recipients") {
		t.Error("CLI 公钥加密输出缺少 PubSec 结构")
	}
	if strings.Contains(s, "Encrypted Document Demo") {
		t.Error("内容未加密")
	}
}
func TestSignVerifyCmd(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "signed.pdf")
	if err := cmdSign([]string{"-o", out, "-selfsign", "cli test", "-reason", "测试签署"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sign.Verify(data)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Reason != "测试签署" {
		t.Errorf("签名校验失败: %+v", res)
	}

	// 篡改 → 必须失败
	data[len(data)-30] ^= 0xff
	res2, err := sign.Verify(data)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Valid {
		t.Error("篡改后必须无效")
	}

	// ECDSA 路径
	out2 := filepath.Join(dir, "signed-ec.pdf")
	if err := cmdSign([]string{"-o", out2, "-ecdsa", "-selfsign", "cli ec"}); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(out2)
	res3, err := sign.Verify(data2)
	if err != nil || !res3.Valid {
		t.Errorf("ECDSA 签名校验失败: %v %+v", err, res3)
	}
}
