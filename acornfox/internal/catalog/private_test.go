package catalog

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
)

func signedIndex(t *testing.T, priv ed25519.PrivateKey, serial int64) ([]byte, []byte) {
	t.Helper()
	b, err := Build(filepath.Join("testdata", "good"), serial)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	return b.Index, b.Sig
}

func TestStoreIndex(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	keys := []ed25519.PublicKey{pub}
	dir := filepath.Join(t.TempDir(), "cat-a", "index")
	ctx := context.Background()

	idx3, sig3 := signedIndex(t, priv, 3)
	if _, _, err := StoreIndex(ctx, dir, "cat-a", idx3, sig3, 3, nil); CodeOf(err) != CodeSyncNoKeys {
		t.Fatalf("没有公钥：%v", err)
	}
	_, badSig := signedIndex(t, other, 3)
	if _, _, err := StoreIndex(ctx, dir, "cat-a", idx3, badSig, 3, keys); CodeOf(err) != CodeSyncSignature {
		t.Fatalf("别的钥匙签的：%v", err)
	}
	if LoadIndex(dir, "cat-a", keys) != nil {
		t.Fatal("验不过的索引落了盘")
	}
	if _, _, err := StoreIndex(ctx, dir, "cat-a", idx3, sig3, 4, keys); CodeOf(err) != CodeSyncMismatch {
		t.Fatalf("网关说的序号和签名的不一致：%v", err)
	}
	up, serial, err := StoreIndex(ctx, dir, "cat-a", idx3, sig3, 3, keys)
	if err != nil || !up || serial != 3 {
		t.Fatalf("第一次存：%v %v %d", err, up, serial)
	}
	if idx := LoadIndex(dir, "cat-a", keys); idx == nil || idx.Serial != 3 {
		t.Fatal("读不回来")
	}
	if up, _, err := StoreIndex(ctx, dir, "cat-a", idx3, sig3, 3, keys); err != nil || up {
		t.Fatalf("同序号重复存应该什么都不改：%v %v", err, up)
	}
	idx2, sig2 := signedIndex(t, priv, 2)
	if _, _, err := StoreIndex(ctx, dir, "cat-a", idx2, sig2, 2, keys); CodeOf(err) != CodeSyncRollback {
		t.Fatalf("回退：%v", err)
	}
	idx5, sig5 := signedIndex(t, priv, 5)
	if up, serial, err := StoreIndex(ctx, dir, "cat-a", idx5, sig5, 5, keys); err != nil || !up || serial != 5 {
		t.Fatalf("升级：%v %v %d", err, up, serial)
	}
	if LoadIndex(dir, "cat-a", []ed25519.PublicKey{other.Public().(ed25519.PublicKey)}) != nil {
		t.Fatal("换了受信公钥后缓存还被当成有效")
	}
}

// A signed document of another kind (the operations overlay) is never
// accepted as an index, even with a valid signature over its bytes.
func TestParseIndexRefusesOtherKinds(t *testing.T) {
	if _, err := ParseIndex([]byte(`{"kind":"ops-overlay","schema":1,"serial":13,"apps":[],"recipes":[]}`)); err == nil {
		t.Fatal("ops.json 被当成了索引")
	}
	if _, err := ParseIndex([]byte(`{"schema":1,"serial":13,"apps":[],"recipes":[]}`)); err != nil {
		t.Fatalf("正常索引被拒：%v", err)
	}
}

func TestPrivateIDs(t *testing.T) {
	for id, ok := range map[string]bool{"cat-a": true, "cat-A_b-9": true, "cat-": false, "cat-a/b": false, "../x": false, "cat-..": false} {
		if ValidCatalogID(id) != ok {
			t.Errorf("%q: %v", id, !ok)
		}
	}
	if PrivatePackageFileID("new-api", "v1.0.0-rc.48") != "catpkg-new-api-v1.0.0-rc.48" {
		t.Fatal("私有应用包的文件编号和经营系统定的不一致")
	}
}

func signedIndexFor(t *testing.T, priv ed25519.PrivateKey, serial int64, catalogID string) ([]byte, []byte) {
	t.Helper()
	b, err := BuildFor(filepath.Join("testdata", "good"), serial, catalogID)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	return b.Index, b.Sig
}

// The signed "catalog" field binds an index to one catalog: a private
// catalog takes only its own (or an unlabelled) index, the public sync only
// "public" (or unlabelled), so a validly signed index of one catalog cannot
// stand in for another.
func TestCatalogIdentityBound(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := []ed25519.PublicKey{pub}
	ctx := context.Background()

	if _, err := BuildFor(filepath.Join("testdata", "good"), 1, "../x"); CodeOf(err) != CodeBuildInput {
		t.Fatalf("坏目录编号：%v", err)
	}
	other, otherSig := signedIndexFor(t, priv, 9, "cat-b")
	public, publicSig := signedIndexFor(t, priv, 9, PublicCatalog)
	dir := filepath.Join(t.TempDir(), "cat-a", "index")
	for name, c := range map[string][2][]byte{"别的私有目录": {other, otherSig}, "公共目录": {public, publicSig}} {
		if _, _, err := StoreIndex(ctx, dir, "cat-a", c[0], c[1], 9, keys); CodeOf(err) != CodeSyncWrongCatalog {
			t.Fatalf("%s的索引冒充 cat-a：%v", name, err)
		}
	}
	if LoadIndex(dir, "cat-a", keys) != nil {
		t.Fatal("别的目录的索引落了盘")
	}
	own, ownSig := signedIndexFor(t, priv, 4, "cat-a")
	if up, _, err := StoreIndex(ctx, dir, "cat-a", own, ownSig, 4, keys); err != nil || !up {
		t.Fatalf("自己的索引：%v %v", err, up)
	}
	if idx := LoadIndex(dir, "cat-a", keys); idx == nil || idx.Catalog != "cat-a" {
		t.Fatalf("读回 %+v", idx)
	}
	if LoadIndex(dir, "cat-b", keys) != nil {
		t.Fatal("cat-a 的缓存被当成 cat-b 的")
	}

	// A private index put on a public source is refused by the public sync
	// (CAT-SYNC-12) even though its serial is higher; the cache is untouched.
	b, err := BuildFor(filepath.Join("testdata", "good"), 50, "cat-b")
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	res, err := Sync(ctx, cfgFor(cache, pub, newSource(t, out)))
	if CodeOf(err) != CodeSyncAllFailed || len(res.Attempts) != 1 || res.Attempts[0].Code != CodeSyncWrongCatalog {
		t.Fatalf("私有索引放到公共来源：%+v %v", res, err)
	}
	if idx, _, _ := LoadWith(cache, nil, keys); idx != nil {
		t.Fatal("私有索引被当成公共目录收下了")
	}
	// A public-labelled index is taken by the public sync.
	b2, _ := BuildFor(filepath.Join("testdata", "good"), 7, PublicCatalog)
	b2.Sign(priv)
	out2 := t.TempDir()
	if _, err := b2.Write(out2); err != nil {
		t.Fatal(err)
	}
	if res, err := Sync(ctx, cfgFor(cache, pub, newSource(t, out2))); err != nil || !res.Updated || res.Serial != 7 {
		t.Fatalf("公共目录：%+v %v", res, err)
	}
}
