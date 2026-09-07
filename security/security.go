// Package security 实现 PDF 标准安全处理器（Standard Security Handler，
// ISO 32000-1 §7.6）：用户/所有者密码、权限位、AES-128（R4）与
// AES-256（R6，ISO 32000-2）内容加密。
//
// 注意：R4 握手算法内部使用 RC4（规范要求，仅用于密钥派生，不用于内容加密），
// RC4 已被认为不安全；对安全敏感的场景请使用 LevelAES256（AES-256 R6）。
package security

import (
	"crypto/rand"
	"fmt"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

// Level 加密算法级别。
type Level int

const (
	// AES128 使用 V4/R4 + AESV2（AES-128-CBC 内容加密，兼容 PDF 1.6+）。
	AES128 Level = iota
	// AES256 使用 V5/R6 + AESV3（AES-256-CBC 内容加密，ISO 32000-2，推荐）。
	AES256
)

// Permission 权限位（Encrypt 字典 /P，ISO 32000-1 表 22）。
type Permission int32

// 权限标志（1 表示允许）。
const (
	PermPrint            Permission = 1 << 2  // bit 3：打印
	PermModify           Permission = 1 << 3  // bit 4：修改文档
	PermCopy             Permission = 1 << 4  // bit 5：复制/提取内容
	PermAnnotate         Permission = 1 << 5  // bit 6：添加/修改批注
	PermFillForms        Permission = 1 << 8  // bit 9：填写表单
	PermExtract          Permission = 1 << 9  // bit 10：无障碍提取
	PermAssemble         Permission = 1 << 10 // bit 11：页面组装
	PermPrintHighQuality Permission = 1 << 11 // bit 12：高质量打印
)

// PermAll 允许全部权限。
const PermAll = Permission(0x0FFC)

// Options 加密选项（标准密码安全处理器）。
// 公钥证书加密见 PubKeyOptions / NewPubKeyHandler。
type Options struct {
	UserPassword  string     // 用户密码（打开密码），可为空
	OwnerPassword string     // 所有者密码（权限密码），为空时取用户密码
	Permissions   Permission // 权限位（用户密码打开时生效），零值表示 PermAll
	Level         Level      // AES128 或 AES256（默认 AES128）
}

// Handler 安全处理器（标准密码处理器或公钥证书处理器），实现 writer.Encryptor。
type Handler struct {
	opts   Options
	key    []byte // 文件加密密钥
	dict   *object.Dict
	aes256 bool
}

// NewHandler 创建安全处理器。fileID 为文档 ID[0]（16 字节）。
func NewHandler(opts Options, fileID []byte) (*Handler, error) {
	if len(fileID) != 16 {
		return nil, fmt.Errorf("security: 文件 ID 必须为 16 字节")
	}
	if opts.Permissions == 0 {
		opts.Permissions = PermAll
	}
	if opts.OwnerPassword == "" && opts.UserPassword != "" {
		opts.OwnerPassword = opts.UserPassword
	}
	h := &Handler{opts: opts, aes256: opts.Level == AES256}
	if h.aes256 {
		if err := h.initR6(fileID); err != nil {
			return nil, err
		}
	} else {
		h.initR4(fileID)
	}
	return h, nil
}

// Dict 返回 Encrypt 字典（该字典自身不加密）。
func (h *Handler) Dict() *object.Dict { return h.dict }

// pValue 计算 /P 值：bit 1-2 为 0，bit 3-12 按权限，bit 13-32 置 1。
func (h *Handler) pValue() int32 {
	return int32(uint32(h.opts.Permissions) | 0xFFFFF000)
}

// EncryptObject 实现 writer.Encryptor：递归加密对象中的字符串与流。
func (h *Handler) EncryptObject(num int, obj object.Object) object.Object {
	switch v := obj.(type) {
	case object.String:
		return h.encryptBytes(num, v)
	case object.HexString:
		return h.encryptBytes(num, v)
	case *object.Dict:
		nd := object.NewDict()
		for _, k := range v.Keys() {
			val, _ := v.Get(k)
			nd.Set(k, h.EncryptObject(num, val))
		}
		return nd
	case object.Array:
		na := make(object.Array, len(v))
		for i, item := range v {
			na[i] = h.EncryptObject(num, item)
		}
		return na
	case *object.Stream:
		enc := h.crypt(num, v.Data)
		ns := object.NewStream(enc)
		for _, k := range v.Dict.Keys() {
			val, _ := v.Dict.Get(k)
			if k == "Length" {
				continue // NewStream 会自动设置
			}
			ns.Dict.Set(k, h.EncryptObject(num, val))
		}
		return ns
	default:
		return obj
	}
}

// encryptBytes 加密字符串字节，输出为十六进制字符串对象。
func (h *Handler) encryptBytes(num int, data []byte) object.Object {
	return object.HexString(h.crypt(num, data))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
