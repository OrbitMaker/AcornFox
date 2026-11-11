package publicconsole

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config is the persisted public (or LAN) console configuration. It exists
// only while that entry is enabled.
//
// Passwords: the public console's password is generated with well over 120
// bits of entropy and stored as a SHA-256 digest (a slow hash adds nothing).
// The LAN console's password is chosen by the user in the browser, so it is
// stored as a salted PBKDF2 hash (PasswordHash). A LAN console enabled by an
// older version still has a generated password in PasswordSHA256; it keeps
// working until the password is cleared. A LAN console with neither is
// "enabled, password not set yet": the first visitor from the local network
// sets it.
type Config struct {
	Domain         string          `json:"domain"`
	PasswordSHA256 string          `json:"password_sha256"`
	EnabledAt      time.Time       `json:"enabled_at"`
	Sessions       []IssuedSession `json:"sessions,omitempty"` // sessions issued by the gateway

	// LAN console only.
	PasswordHash    string     `json:"password_hash,omitempty"`     // pbkdf2-sha256$<iterations>$<salt>$<key>, base64 (raw, std)
	PasswordSetAt   *time.Time `json:"password_set_at,omitempty"`   // when the user set it in the browser
	PasswordSetFrom string     `json:"password_set_from,omitempty"` // client address it was set from
	Devices         []Device   `json:"devices,omitempty"`           // "remember this device" entries
}

// IssuedSession records one console session created through the public
// login, by the SHA-256 digest of its secret.
type IssuedSession struct {
	Digest   string    `json:"digest"`
	IssuedAt time.Time `json:"issued_at"`
	DeviceID string    `json:"device_id,omitempty"` // the remembered device it belongs to, if any
}

// DeviceRemember is how long "remember this device" lasts on the LAN console.
const DeviceRemember = 90 * 24 * time.Hour

// MaxDevices bounds the remembered devices; the least recently used go first.
const MaxDevices = 20

// MinPasswordLen and MaxPasswordLen bound a user-chosen LAN console password.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 128
)

// Device is one browser the user asked the LAN console to remember. Only the
// SHA-256 digest of its cookie secret (256 random bits) is stored.
type Device struct {
	ID         string    `json:"id"` // random 12 hex, not secret
	Digest     string    `json:"digest"`
	Name       string    `json:"name"` // from the User-Agent, e.g. "Mac · Chrome"
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	LastIP     string    `json:"last_ip,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// HasPassword reports whether a login password exists (user-set or, from an
// older version, generated).
func (c *Config) HasPassword() bool { return c.PasswordHash != "" || c.PasswordSHA256 != "" }

// FindDevice returns the unexpired remembered device with this cookie digest.
func (c *Config) FindDevice(digest string, now time.Time) *Device {
	for i := range c.Devices {
		d := &c.Devices[i]
		if subtle.ConstantTimeCompare([]byte(d.Digest), []byte(digest)) == 1 && now.Before(d.ExpiresAt) {
			return d
		}
	}
	return nil
}

// RemoveDevice drops the device and every session issued to it; it reports
// whether the device existed.
func (c *Config) RemoveDevice(id string) bool {
	found := false
	kept := c.Devices[:0]
	for _, d := range c.Devices {
		if d.ID == id {
			found = true
			continue
		}
		kept = append(kept, d)
	}
	c.Devices = kept
	if found {
		sess := c.Sessions[:0]
		for _, s := range c.Sessions {
			if s.DeviceID != id {
				sess = append(sess, s)
			}
		}
		c.Sessions = sess
	}
	return found
}

// ClearPassword removes the password and, with it, everything that was
// granted under it: sessions and remembered devices.
func (c *Config) ClearPassword() {
	c.PasswordSHA256, c.PasswordHash, c.PasswordSetAt, c.PasswordSetFrom = "", "", nil, ""
	c.Sessions, c.Devices = nil, nil
}

// pbkdf2Iterations follows OWASP's 2023 recommendation for PBKDF2-HMAC-SHA256.
const pbkdf2Iterations = 600000

// HashPassword returns the salted PBKDF2 hash of a user-chosen password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return "pbkdf2-sha256$" + strconv.Itoa(pbkdf2Iterations) + "$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key), nil
}

// verifyPasswordHash checks password against a HashPassword result.
func verifyPasswordHash(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 || iter > 10000000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// HasSession reports whether the session with this digest was issued by the
// gateway under the current configuration.
func (c *Config) HasSession(digest string) bool {
	for _, s := range c.Sessions {
		if subtle.ConstantTimeCompare([]byte(s.Digest), []byte(digest)) == 1 {
			return true
		}
	}
	return false
}

// FileStore keeps Config in one 0600 JSON file under the data directory, so
// upgrades (which never touch the data directory) keep it.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a store at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// Load returns the configuration, or nil when the public console is off.
func (s *FileStore) Load() (*Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *FileStore) load() (*Config, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取公网控制台配置失败：%w", err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("公网控制台配置损坏：%w", err)
	}
	return &c, nil
}

// Save replaces the configuration atomically.
func (s *FileStore) Save(c *Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(c)
}

func (s *FileStore) save(c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".public-console-*.json")
	if err != nil {
		return fmt.Errorf("写入公网控制台配置失败：%w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Update loads, mutates and saves under one lock. fn is not called when the
// public console is off.
func (s *FileStore) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load()
	if err != nil || c == nil {
		return err
	}
	fn(c)
	return s.save(c)
}

// Delete removes the configuration; a no-op when absent.
func (s *FileStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// passwordAlphabet omits look-alike characters (0/O, 1/l/I).
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GeneratePassword returns 24 random characters from a 55-letter alphabet
// (about 138 bits), grouped by dashes for reading: xxxxxx-xxxxxx-xxxxxx-xxxxxx.
func GeneratePassword() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := 0; i < 24; i++ {
		if i > 0 && i%6 == 0 {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(passwordAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// Digest is the lowercase hex SHA-256 of s (same as the state package uses for
// console session secrets).
func Digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// CheckPassword compares password with the stored hash (user-set) or digest
// (generated) in constant time. Without a password nothing matches.
func (c *Config) CheckPassword(password string) bool {
	if c.PasswordHash != "" {
		return verifyPasswordHash(c.PasswordHash, password)
	}
	if c.PasswordSHA256 == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(Digest(password)), []byte(c.PasswordSHA256)) == 1
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
