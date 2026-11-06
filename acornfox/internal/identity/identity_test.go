package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type vectors struct {
	Body       string `json:"body"`
	BodySHA256 string `json:"body_sha256"`
	Canonical  string `json:"canonical"`
	InstallID  string `json:"install_id"`
	Method     string `json:"method"`
	Nonce      string `json:"nonce"`
	Path       string `json:"path"`
	PrivD      string `json:"private_key_d"`
	PublicKey  string `json:"public_key"`
	SigP1363   string `json:"signature_p1363"`
	Timestamp  int64  `json:"timestamp"`
}

func loadVectors(t *testing.T) (vectors, *ecdsa.PrivateKey) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "contract-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	d, err := hex.DecodeString(v.PrivD)
	if err != nil {
		t.Fatal(err)
	}
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(d)
	k.X, k.Y = elliptic.P256().ScalarBaseMult(d)
	return v, k
}

func TestContractVectors(t *testing.T) {
	v, k := loadVectors(t)
	pub := EncodePublicKey(&k.PublicKey)
	if pub != v.PublicKey {
		t.Fatalf("public key %s, want %s", pub, v.PublicKey)
	}
	iid, err := InstallID(pub)
	if err != nil || iid != v.InstallID {
		t.Fatalf("install id %s (%v), want %s", iid, err, v.InstallID)
	}
	if h := BodyHash([]byte(v.Body)); h != v.BodySHA256 {
		t.Fatalf("body hash %s", h)
	}
	if c := Canonical(v.Method, v.Path, v.Timestamp, v.Nonce, []byte(v.Body)); c != v.Canonical {
		t.Fatalf("canonical\n%q\nwant\n%q", c, v.Canonical)
	}
	// Lower-case method is normalised.
	if c := Canonical(strings.ToLower(v.Method), v.Path, v.Timestamp, v.Nonce, []byte(v.Body)); c != v.Canonical {
		t.Fatal("method not upper-cased")
	}
	decoded, err := DecodePublicKey(v.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	// The vector's signature (made by the gateway's reference code) verifies.
	if !Verify(decoded, v.Method, v.Path, v.Timestamp, v.Nonce, []byte(v.Body), v.SigP1363) {
		t.Fatal("vector signature does not verify")
	}
	// Our own signature verifies, is r||s (64 bytes = 86 base64url chars),
	// and a changed body or timestamp does not.
	id := fromKey(k, "")
	sig, err := id.Sign(v.Method, v.Path, v.Timestamp, v.Nonce, []byte(v.Body))
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 86 || strings.ContainsAny(sig, "+/=") {
		t.Fatalf("signature not r||s base64url: %q", sig)
	}
	if !Verify(decoded, v.Method, v.Path, v.Timestamp, v.Nonce, []byte(v.Body), sig) {
		t.Fatal("own signature does not verify")
	}
	if Verify(decoded, v.Method, v.Path, v.Timestamp+1, v.Nonce, []byte(v.Body), sig) {
		t.Fatal("verified with another timestamp")
	}
	if Verify(decoded, v.Method, v.Path, v.Timestamp, v.Nonce, []byte(v.Body+" "), sig) {
		t.Fatal("verified with another body")
	}
}

func TestLoadOrCreateAndReset(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	a, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Created || !strings.HasPrefix(a.InstallID, "afx-") || len(a.InstallID) != 24 {
		t.Fatalf("%+v", a)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(KeyPath(dir))
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("key mode %v", fi.Mode().Perm())
		}
		di, _ := os.Stat(Dir(dir))
		if di.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode %v", di.Mode().Perm())
		}
	}
	b, _ := os.ReadFile(KeyPath(dir))
	if blk, _ := pem.Decode(b); blk == nil || blk.Type != "PRIVATE KEY" {
		t.Fatal("not PKCS#8 PEM")
	}
	again, err := LoadOrCreate(dir)
	if err != nil || again.Created || again.InstallID != a.InstallID {
		t.Fatalf("second load: %+v %v", again, err)
	}
	var info Info
	ib, _ := os.ReadFile(filepath.Join(Dir(dir), "install.json"))
	if json.Unmarshal(ib, &info) != nil || info.InstallID != a.InstallID || info.PublicKey != a.PublicKey {
		t.Fatalf("install.json %s", ib)
	}

	r, err := Reset(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.InstallID == a.InstallID {
		t.Fatal("reset kept the install id")
	}
	if cur, _ := Load(dir); cur.InstallID != r.InstallID {
		t.Fatal("reset not saved")
	}
	pb, _ := os.ReadFile(KeyPath(dir) + ".prev")
	pk, err := ParsePrivateKeyPEM(pb)
	if err != nil || fromKey(pk, "").InstallID != a.InstallID {
		t.Fatal("old key not kept as .prev")
	}
	logb, _ := os.ReadFile(filepath.Join(Dir(dir), "identity.log"))
	if !strings.Contains(string(logb), "create "+a.InstallID) || !strings.Contains(string(logb), "reset "+r.InstallID+" (was "+a.InstallID+")") {
		t.Fatalf("log: %s", logb)
	}
	// No temp files left behind.
	ents, _ := os.ReadDir(Dir(dir))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

func TestResetReplacesBrokenKey(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(Dir(dir), 0o700)
	os.WriteFile(KeyPath(dir), []byte("garbage"), 0o600)
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("broken key loaded")
	}
	if _, err := Reset(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
}

func TestReadsSEC1AndOpenSSLStyle(t *testing.T) {
	_, k := loadVectors(t)
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	// openssl ecparam -genkey writes an "EC PARAMETERS" block first.
	params := pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{6, 8, 42, 134, 72, 206, 61, 3, 1, 7}})
	body := append(params, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})...)
	dir := t.TempDir()
	os.MkdirAll(Dir(dir), 0o700)
	if err := os.WriteFile(KeyPath(dir), body, 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id.Created || id.InstallID != "afx-6vnjm4bf6lngwgjafays" {
		t.Fatalf("%+v", id)
	}
	// A P-384 key is refused.
	k384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if k384 != nil {
		d384, _ := x509.MarshalPKCS8PrivateKey(k384)
		if _, err := ParsePrivateKeyPEM(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: d384})); err == nil {
			t.Fatal("P-384 accepted")
		}
	}
}

func TestConcurrentCreateAgrees(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	ids := make([]string, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := LoadOrCreate(dir)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = id.InstallID
		}(i)
	}
	wg.Wait()
	for _, s := range ids[1:] {
		if s != ids[0] {
			t.Fatalf("different ids: %v", ids)
		}
	}
}

func TestNonce(t *testing.T) {
	n, err := NewNonce()
	if err != nil || len(n) != 22 {
		t.Fatalf("%q %v", n, err)
	}
}
