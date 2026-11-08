package appinstall

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// secretBox encrypts what the install keeps for later (install parameters,
// first-login fields) with AES-256-GCM. The key is 32 random bytes in
// <data>/secrets/catalog.key (0600, created on first use). The database then
// holds only ciphertext "v1:<base64url(nonce||sealed)>"; a copy of the
// database alone does not reveal the passwords.
type secretBox struct {
	path string
	mu   sync.Mutex
	aead cipher.AEAD
}

func newSecretBox(dataDir string) *secretBox {
	return &secretBox{path: filepath.Join(dataDir, "secrets", "catalog.key")}
}

func (b *secretBox) cipher() (cipher.AEAD, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.aead != nil {
		return b.aead, nil
	}
	key, err := os.ReadFile(b.path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(b.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			_ = os.Remove(b.path)
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s 不是 32 字节", b.path)
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	b.aead, err = cipher.NewGCM(blk)
	return b.aead, err
}

func (b *secretBox) seal(plain []byte) (string, error) {
	a, err := b.cipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.RawURLEncoding.EncodeToString(a.Seal(nonce, nonce, plain, nil)), nil
}

func (b *secretBox) open(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "v1:") {
		return nil, errors.New("不认识的密文格式")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[3:])
	if err != nil {
		return nil, err
	}
	a, err := b.cipher()
	if err != nil {
		return nil, err
	}
	if len(raw) < a.NonceSize() {
		return nil, errors.New("密文太短")
	}
	return a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], nil)
}
