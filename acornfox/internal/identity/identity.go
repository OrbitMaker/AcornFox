// Package identity is this machine's download-gateway identity: a random
// ECDSA P-256 key that acornfox generates once and keeps in the data
// directory. Nothing in it comes from the hardware; resetting it makes the
// machine look new to the gateway.
//
// Files (under <data dir>/identity/, directory 0700):
//
//	machine-key.pem       the private key, PKCS#8 PEM, 0600 (SEC1 "EC PRIVATE KEY" is read too)
//	machine-key.pem.prev  the key before the last Reset (kept for rollback)
//	install.json          install ID and public key (not secret; for people and scripts)
//	identity.log          one line per create / reset (time, action, install ID)
//
// Wire formats (shared with the gateway and the PowerShell / shell clients,
// see the test vectors in testdata/):
//
//   - public key: the uncompressed point 0x04||X||Y (65 bytes), base64url, no padding;
//   - install ID: "afx-" + the first 20 characters of lower-case base32 (no
//     padding) of SHA-256(the 65-byte public key);
//   - string to sign: "AFX-GW-1\n<METHOD>\n<path>\n<unix ts>\n<nonce>\n<hex sha256 of body>";
//   - signature: ECDSA-P256-SHA256, r||s (32 bytes each, big-endian), base64url.
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is the first line of the string to sign. A new wire format gets a
// new version on both sides.
const Version = "AFX-GW-1"

const (
	dirName  = "identity"
	keyName  = "machine-key.pem"
	infoName = "install.json"
	logName  = "identity.log"
)

var b64 = base64.RawURLEncoding

// Identity is a loaded machine key with its derived public values.
type Identity struct {
	Key       *ecdsa.PrivateKey
	PublicKey string // base64url of the 65-byte uncompressed point
	InstallID string // "afx-" + 20 base32 characters
	KeyPath   string
	Created   bool // true when LoadOrCreate generated the key just now
}

// Dir is <dataDir>/identity.
func Dir(dataDir string) string { return filepath.Join(dataDir, dirName) }

// KeyPath is <dataDir>/identity/machine-key.pem.
func KeyPath(dataDir string) string { return filepath.Join(Dir(dataDir), keyName) }

// ErrNoKey is returned by Load when the machine has no key yet.
var ErrNoKey = errors.New("这台机器还没有生成安装编号（第一次通过下载网关下载时会自动生成）")

// Load reads the existing key without creating one.
func Load(dataDir string) (*Identity, error) {
	path := KeyPath(dataDir)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("读取机器钥匙失败：%w", err)
	}
	key, err := ParsePrivateKeyPEM(b)
	if err != nil {
		return nil, fmt.Errorf("机器钥匙 %s 格式不对（可以用 acornfox identity reset --yes 重新生成）：%w", path, err)
	}
	return fromKey(key, path), nil
}

// LoadOrCreate returns the machine's key, generating and saving a new one
// the first time. Two processes calling it at once end up with the same key.
func LoadOrCreate(dataDir string) (*Identity, error) {
	id, err := Load(dataDir)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, ErrNoKey) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(Dir(dataDir), 0o700); err != nil {
		return nil, fmt.Errorf("创建 %s 失败：%w", Dir(dataDir), err)
	}
	tmp, err := writeTemp(dataDir, key)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	path := KeyPath(dataDir)
	// Link fails when another process created the key meanwhile; then use
	// that one so both agree.
	if err := os.Link(tmp, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return Load(dataDir)
		}
		if err := os.Rename(tmp, path); err != nil {
			return nil, fmt.Errorf("保存机器钥匙失败：%w", err)
		}
	}
	id = fromKey(key, path)
	id.Created = true
	writeInfo(dataDir, id)
	appendLog(dataDir, "create", id.InstallID, "")
	return id, nil
}

// Reset replaces the key with a new random one (a new install ID). The old
// key is kept as machine-key.pem.prev; to undo, move it back.
func Reset(dataDir string) (*Identity, error) {
	old, oldErr := Load(dataDir)
	if oldErr != nil && !errors.Is(oldErr, ErrNoKey) {
		// An unreadable key is replaced too: that is what reset is for.
		old = nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(Dir(dataDir), 0o700); err != nil {
		return nil, fmt.Errorf("创建 %s 失败：%w", Dir(dataDir), err)
	}
	path := KeyPath(dataDir)
	if b, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".prev", b, 0o600); err != nil {
			return nil, fmt.Errorf("备份旧钥匙失败：%w", err)
		}
	}
	tmp, err := writeTemp(dataDir, key)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("保存机器钥匙失败：%w", err)
	}
	id := fromKey(key, path)
	id.Created = true
	writeInfo(dataDir, id)
	prev := ""
	if old != nil {
		prev = old.InstallID
	}
	appendLog(dataDir, "reset", id.InstallID, prev)
	return id, nil
}

func fromKey(key *ecdsa.PrivateKey, path string) *Identity {
	pub := EncodePublicKey(&key.PublicKey)
	iid, _ := InstallID(pub)
	return &Identity{Key: key, PublicKey: pub, InstallID: iid, KeyPath: path}
}

func writeTemp(dataDir string, key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(Dir(dataDir), keyName+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("保存机器钥匙失败：%w", err)
	}
	name := f.Name()
	if err := f.Chmod(0o600); err != nil && !isWindows() {
		f.Close()
		os.Remove(name)
		return "", err
	}
	werr := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if serr := f.Sync(); werr == nil {
		werr = serr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(name)
		return "", fmt.Errorf("保存机器钥匙失败：%w", werr)
	}
	return name, nil
}

func isWindows() bool { return os.PathSeparator == '\\' }

// Info is what install.json holds.
type Info struct {
	InstallID string `json:"install_id"`
	PublicKey string `json:"public_key"`
	UpdatedAt string `json:"updated_at"`
}

func writeInfo(dataDir string, id *Identity) {
	b, _ := json.MarshalIndent(Info{InstallID: id.InstallID, PublicKey: id.PublicKey, UpdatedAt: time.Now().Format(time.RFC3339)}, "", "  ")
	_ = os.WriteFile(filepath.Join(Dir(dataDir), infoName), append(b, '\n'), 0o600)
}

func appendLog(dataDir, action, installID, prev string) {
	f, err := os.OpenFile(filepath.Join(Dir(dataDir), logName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line := time.Now().Format(time.RFC3339) + " " + action + " " + installID
	if prev != "" {
		line += " (was " + prev + ")"
	}
	_, _ = io.WriteString(f, line+"\n")
}

// ParsePrivateKeyPEM reads a P-256 private key from PEM: PKCS#8
// ("PRIVATE KEY") or SEC1 ("EC PRIVATE KEY", what the PowerShell client
// exports).
func ParsePrivateKeyPEM(b []byte) (*ecdsa.PrivateKey, error) {
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			return nil, errors.New("没有找到 PEM 私钥")
		}
		var key any
		var err error
		switch blk.Type {
		case "PRIVATE KEY":
			key, err = x509.ParsePKCS8PrivateKey(blk.Bytes)
		case "EC PRIVATE KEY":
			key, err = x509.ParseECPrivateKey(blk.Bytes)
		default:
			continue // e.g. "EC PARAMETERS" written by openssl before the key
		}
		if err != nil {
			return nil, err
		}
		ec, ok := key.(*ecdsa.PrivateKey)
		if !ok || ec.Curve != elliptic.P256() {
			return nil, errors.New("不是 ECDSA P-256 私钥")
		}
		return ec, nil
	}
}

// EncodePublicKey is the base64url of the uncompressed point 0x04||X||Y.
func EncodePublicKey(pub *ecdsa.PublicKey) string {
	out := make([]byte, 65)
	out[0] = 4
	pub.X.FillBytes(out[1:33])
	pub.Y.FillBytes(out[33:65])
	return b64.EncodeToString(out)
}

// InstallID derives the install ID from an encoded public key.
func InstallID(pubB64 string) (string, error) {
	raw, err := b64.DecodeString(pubB64)
	if err != nil || len(raw) != 65 || raw[0] != 4 {
		return "", errors.New("公钥格式不对")
	}
	h := sha256.Sum256(raw)
	s := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:]))
	return "afx-" + s[:20], nil
}

// BodyHash is the lower-case hex SHA-256 of the raw request body.
func BodyHash(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

// Canonical is the string to sign (lines joined by \n, no trailing newline).
func Canonical(method, path string, ts int64, nonce string, body []byte) string {
	return strings.Join([]string{Version, strings.ToUpper(method), path, strconv.FormatInt(ts, 10), nonce, BodyHash(body)}, "\n")
}

// NewNonce returns 16 random bytes as base64url (22 characters).
func NewNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

// Sign signs a request and returns r||s as base64url.
func (id *Identity) Sign(method, path string, ts int64, nonce string, body []byte) (string, error) {
	h := sha256.Sum256([]byte(Canonical(method, path, ts, nonce, body)))
	r, s, err := ecdsa.Sign(rand.Reader, id.Key, h[:])
	if err != nil {
		return "", err
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return b64.EncodeToString(out), nil
}

// DecodePublicKey parses EncodePublicKey's output and checks the point is on
// P-256.
func DecodePublicKey(s string) (*ecdsa.PublicKey, error) {
	raw, err := b64.DecodeString(s)
	if err != nil || len(raw) != 65 || raw[0] != 4 {
		return nil, errors.New("公钥格式不对：要 65 字节的未压缩点（base64url）")
	}
	x := new(big.Int).SetBytes(raw[1:33])
	y := new(big.Int).SetBytes(raw[33:65])
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, errors.New("公钥不在 P-256 曲线上")
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

// Verify checks an r||s base64url signature (what Sign produces). The
// gateway also accepts DER; the client only needs its own format, so this
// is for tests and self-checks.
func Verify(pub *ecdsa.PublicKey, method, path string, ts int64, nonce string, body []byte, sigB64 string) bool {
	raw, err := b64.DecodeString(sigB64)
	if err != nil || len(raw) != 64 {
		return false
	}
	h := sha256.Sum256([]byte(Canonical(method, path, ts, nonce, body)))
	return ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:]))
}
