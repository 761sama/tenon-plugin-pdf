package security_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf"
	"gopkg.761sama.com/tenon-plugin-pdf/font"
	"gopkg.761sama.com/tenon-plugin-pdf/page"
	"gopkg.761sama.com/tenon-plugin-pdf/security"
)

func buildEncryptedDoc(t *testing.T, level security.Level, user, owner string, perms security.Permission) []byte {
	t.Helper()
	doc := pdf.New()
	doc.Info().Title = "Encrypted Doc"
	doc.SetEncryption(security.Options{
		UserPassword:  user,
		OwnerPassword: owner,
		Permissions:   perms,
		Level:         level,
	})
	p := doc.AddPage(page.A4)
	p.DrawText(font.Helvetica, 18, 72, 760, "Secret Content 42")
	data, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEncryptDictStructure(t *testing.T) {
	for _, level := range []security.Level{security.AES128, security.AES256} {
		data := buildEncryptedDoc(t, level, "user123", "owner456", security.PermAll&^security.PermCopy)
		var marker []string
		if level == security.AES128 {
			marker = []string{"/Filter /Standard", "/V 4", "/R 4", "/Length 128", "AESV2"}
		} else {
			marker = []string{"/Filter /Standard", "/V 5", "/R 6", "/Length 256", "AESV3", "/OE", "/UE", "/Perms"}
		}
		for _, m := range marker {
			if !bytes.Contains(data, []byte(m)) {
				t.Errorf("level %v: missing %q", level, m)
			}
		}
		// 内容流必须被加密（明文不可见）
		if bytes.Contains(data, []byte("Secret Content")) {
			t.Errorf("level %v: content not encrypted", level)
		}
		// /Encrypt 在 trailer 中
		if !bytes.Contains(data, []byte("/Encrypt")) {
			t.Errorf("level %v: trailer missing /Encrypt", level)
		}
	}
}

// TestEncryptWithPoppler 用 poppler 工具外部验证：识别加密、密码解析、权限。
func TestEncryptWithPoppler(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo 不可用")
	}
	cases := []struct {
		name              string
		level             security.Level
		user, owner       string
		perms             security.Permission
		wantPermsFragment []string
	}{
		{"AES128", security.AES128, "user123", "owner456", security.PermAll &^ security.PermCopy, []string{"copy:no"}},
		{"AES256", security.AES256, "user123", "owner456", security.PermAll &^ security.PermCopy, []string{"copy:no"}},
		{"AES256只设所有者", security.AES256, "", "owner456", security.PermAll &^ security.PermPrint, []string{"print:no"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildEncryptedDoc(t, c.level, c.user, c.owner, c.perms)
			path := filepath.Join(t.TempDir(), "enc.pdf")
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}

			out, err := exec.Command("pdfinfo", "-upw", c.user, "-opw", c.owner, path).CombinedOutput()
			if err != nil {
				t.Fatalf("pdfinfo: %v\n%s", err, out)
			}
			info := string(out)
			encIdx := strings.Index(info, "Encrypted:")
			if encIdx < 0 || !strings.Contains(info[encIdx:], "yes") {
				t.Errorf("pdfinfo 未识别加密:\n%s", info)
			}
			for _, frag := range c.wantPermsFragment {
				if !strings.Contains(info, frag) {
					t.Errorf("pdfinfo 权限应包含 %q:\n%s", frag, info)
				}
			}

			// 正确密码可提取文本（用户密码或所有者密码均可）
			pw, flag := c.user, "-upw"
			if pw == "" {
				pw, flag = c.owner, "-opw"
			}
			txt, err := exec.Command("pdftotext", flag, pw, path, "-").CombinedOutput()
			if err != nil {
				t.Fatalf("pdftotext: %v\n%s", err, txt)
			}
			if !strings.Contains(string(txt), "Secret Content 42") {
				t.Errorf("正确密码未能提取文本: %s", txt)
			}
			// 错误密码必须失败
			if _, err := exec.Command("pdftotext", "-upw", "wrongpw", "-opw", "wrongpw", path, "-").CombinedOutput(); err == nil {
				t.Error("错误密码应该失败")
			}
		})
	}
}
