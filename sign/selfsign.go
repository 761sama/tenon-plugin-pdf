package sign

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// GenerateRSAKey 生成 RSA 2048 私钥。
func GenerateRSAKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// GenerateECDSAKey 生成 ECDSA P-256 私钥。
func GenerateECDSAKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// GenerateSelfSigned 生成自签名证书（测试/演示用途）。
func GenerateSelfSigned(commonName string, key crypto.Signer) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"tenon-plugin-pdf"},
			Country:      []string{"CN"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, key.Public(), key)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

// GenerateSignedCertificate 用 CA 证书签发证书（测试/演示用链式信任场景）。
// isCA 为 true 时生成中间 CA 证书（可继续签发下级）。
func GenerateSignedCertificate(commonName string, key crypto.Signer,
	ca *x509.Certificate, caKey crypto.Signer, isCA bool) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"tenon-plugin-pdf"},
			Country:      []string{"CN"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	if isCA {
		tpl.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageAny}
	} else {
		tpl.KeyUsage = x509.KeyUsageDigitalSignature
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageAny}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, ca, key.Public(), caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

// MarshalCertChainPEM 将证书链依次编码为一个 PEM 文件（多个 CERTIFICATE 块）。
func MarshalCertChainPEM(certs ...*x509.Certificate) []byte {
	var out []byte
	for _, c := range certs {
		out = append(out, MarshalCertPEM(c)...)
	}
	return out
}

// ParseCertChainPEM 解析含多个 CERTIFICATE 块的 PEM 数据。
func ParseCertChainPEM(data []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for {
		blk, rest := pem.Decode(data)
		if blk == nil {
			break
		}
		data = rest
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sign: PEM 中未找到证书")
	}
	return out, nil
}

// MarshalCertPEM 将证书编码为 PEM。
func MarshalCertPEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// MarshalKeyPEM 将私钥编码为 PEM（PKCS#8）。
func MarshalKeyPEM(key crypto.Signer) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParseCertPEM 解析 PEM 证书。
func ParseCertPEM(data []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(data)
	if blk == nil {
		return nil, fmt.Errorf("sign: 非法 PEM 证书")
	}
	return x509.ParseCertificate(blk.Bytes)
}

// ParseKeyPEM 解析 PEM 私钥（PKCS#8 或 PKCS#1 或 SEC1）。
func ParseKeyPEM(data []byte) (crypto.Signer, error) {
	blk, _ := pem.Decode(data)
	if blk == nil {
		return nil, fmt.Errorf("sign: 非法 PEM 私钥")
	}
	if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
		s, ok := k.(crypto.Signer)
		if ok {
			return s, nil
		}
	}
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("sign: 无法解析私钥")
}
