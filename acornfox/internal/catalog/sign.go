package catalog

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Signing format:
//
//   - index.json.sig is ed25519(private key, the exact bytes of index.json),
//     base64url without padding; readers ignore surrounding whitespace;
//   - a public key is the raw 32-byte ed25519 key, base64url without padding;
//   - a private key file is PEM "PRIVATE KEY" (PKCS#8), mode 0600.

var b64 = base64.RawURLEncoding

// releaseKeys are the public halves of the catalog signing keys, base64url
// without padding (acornfox catalog keygen prints them). The private keys
// never enter any repository.
var releaseKeys = []string{
	"rmsb4bs1vi_mWI4lPEOYcIaF6bBO74bzZNm4Qwb14yE", // catalog signing key 1 (in use)
	"oB1a9rUgmg_FrN6ELWrBnXzwgYf3KpnQkCVUypMQch8", // catalog signing key 2 (offline spare; only an encrypted backup exists)
}

// TrustedKeys are the public keys this program accepts for the published
// catalog. More than one is allowed so a key can be rotated: ship the new key
// next to the old one, switch signing, then drop the old one. Tests replace
// it.
var TrustedKeys = mustParseKeys(releaseKeys)

func mustParseKeys(ss []string) []ed25519.PublicKey {
	out := make([]ed25519.PublicKey, 0, len(ss))
	for _, s := range ss {
		k, err := ParsePublicKey(s)
		if err != nil {
			panic("catalog: bad built-in public key: " + err.Error())
		}
		out = append(out, k)
	}
	return out
}

// EncodePublicKey renders pub as base64url without padding.
func EncodePublicKey(pub ed25519.PublicKey) string { return b64.EncodeToString(pub) }

// ParsePublicKey reads a key printed by EncodePublicKey (CAT-KEY-04).
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := b64.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, newErr(CodeKeyPublic, err, "公钥要是 base64url（无填充）编码的 32 字节 ed25519 公钥")
	}
	return ed25519.PublicKey(raw), nil
}

// errWindowsKey refuses private keys on Windows: file modes there do not
// say who can read the file, so the 0600 rule cannot be enforced.
func errWindowsKey() error {
	return newErr(CodeKeyPerm, nil, "Windows 上查不了私钥文件的权限，不在 Windows 上生成或使用签名私钥；请在 WSL、Linux 或 Mac 上运行")
}

// GenerateKeyFile creates a new ed25519 key at path (PKCS#8 PEM, 0600) and
// returns its public key. It never overwrites: an existing path fails with
// CAT-KEY-03. On Windows it refuses (CAT-KEY-02).
func GenerateKeyFile(path string) (ed25519.PublicKey, error) {
	if runtime.GOOS == "windows" {
		return nil, errWindowsKey()
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, newErr(CodeKeyRead, err, "生成钥匙失败")
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, newErr(CodeKeyRead, err, "编码私钥失败")
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, newErr(CodeKeyExists, nil, path+" 已存在，不覆盖")
		}
		return nil, newErr(CodeKeyRead, err, "建不了私钥文件")
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(path)
		return nil, newErr(CodeKeyRead, err, "写私钥失败")
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, newErr(CodeKeyRead, err, "写私钥失败")
	}
	_ = os.Chmod(path, 0o600)
	return pub, nil
}

// LoadSigningKey reads a private key written by GenerateKeyFile. It refuses
// (CAT-KEY-02) a file that group or others can access, and refuses to run on
// Windows, where that cannot be checked.
func LoadSigningKey(path string) (ed25519.PrivateKey, error) {
	if runtime.GOOS == "windows" {
		return nil, errWindowsKey()
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, newErr(CodeKeyRead, err, "读不了私钥文件")
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, newErr(CodeKeyPerm, nil, fmt.Sprintf("私钥文件 %s 的权限是 %04o，比 0600 宽，先 chmod 600", path, st.Mode().Perm()))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, newErr(CodeKeyRead, err, "读不了私钥文件")
	}
	blk, _ := pem.Decode(data)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		return nil, newErr(CodeKeyRead, nil, "私钥文件不是 PEM PRIVATE KEY（PKCS#8）")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, newErr(CodeKeyRead, err, "私钥解析失败")
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, newErr(CodeKeyRead, nil, "私钥不是 ed25519")
	}
	return priv, nil
}

// errBadSignature is returned by Verify; callers attach their own code.
var errBadSignature = errors.New("签名和受信公钥都对不上")

// Verify checks sig (the content of index.json.sig) over index against each
// trusted key and succeeds when one matches.
func Verify(index, sig []byte, trusted []ed25519.PublicKey) error {
	if len(trusted) == 0 {
		return errors.New("没有受信公钥")
	}
	raw, err := b64.DecodeString(string(bytes.TrimSpace(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errBadSignature
	}
	for _, k := range trusted {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, index, raw) {
			return nil
		}
	}
	return errBadSignature
}

// VerifyResult is what VerifyDir found.
type VerifyResult struct {
	Serial   int64 `json:"serial"`
	Apps     int   `json:"apps"`
	Recipes  int   `json:"recipes"`
	Packages int   `json:"packages"`
}

// VerifyDir checks a published directory without changing it: signature,
// version.txt against the signed serial, and every package's sha256 and size.
func VerifyDir(dir string, trusted []ed25519.PublicKey) (VerifyResult, error) {
	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, newErr(CodeVerifyFiles, err, "缺 "+name)
		}
		return b, nil
	}
	ver, err := read(VersionFile)
	if err != nil {
		return VerifyResult{}, err
	}
	idxB, err := read(IndexFile)
	if err != nil {
		return VerifyResult{}, err
	}
	sig, err := read(SigFile)
	if err != nil {
		return VerifyResult{}, err
	}
	if err := Verify(idxB, sig, trusted); err != nil {
		return VerifyResult{}, newErr(CodeVerifySig, err, "index.json 的签名验不过")
	}
	idx, err := ParseIndex(idxB)
	if err != nil {
		return VerifyResult{}, newErr(CodeVerifyFiles, err, "index.json 格式不对")
	}
	serial, err := parseSerial(ver)
	if err != nil {
		return VerifyResult{}, newErr(CodeVerifyFiles, err, "version.txt 不是正整数")
	}
	if serial != idx.Serial {
		return VerifyResult{}, newErr(CodeVerifySerial, nil, fmt.Sprintf("version.txt 是 %d，index.json 是 %d", serial, idx.Serial))
	}
	for _, a := range idx.Apps {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(a.Package.Path)))
		if err != nil {
			return VerifyResult{}, newErr(CodeVerifyPackage, err, "缺应用包 "+a.Package.Path)
		}
		if int64(len(b)) != a.Package.Size || sha256Hex(b) != a.Package.SHA256 {
			return VerifyResult{}, newErr(CodeVerifyPackage, nil, a.Package.Path+" 的 sha256 或大小和 index.json 不一致")
		}
	}
	return VerifyResult{Serial: idx.Serial, Apps: len(idx.Apps), Recipes: len(idx.Recipes), Packages: len(idx.Apps)}, nil
}

func parseSerial(b []byte) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("序号 %d 不是正整数", n)
	}
	return n, nil
}
