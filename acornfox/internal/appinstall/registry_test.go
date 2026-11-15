package appinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/state"
)

// regWorld is a registry on a real store, with the paths needed to look at
// what reached the disk.
type regWorld struct {
	r       *Registry
	st      *state.Store
	dbPath  string
	dataDir string
}

func newRegWorld(t *testing.T) *regWorld {
	t.Helper()
	dir := t.TempDir()
	w := &regWorld{dbPath: filepath.Join(dir, "acornfox.db"), dataDir: filepath.Join(dir, "data")}
	st, err := state.Open(state.Config{Path: w.dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w.st = st
	w.r = NewRegistry(st, w.dataDir)
	return w
}

func (w *regWorld) mustCreate(t *testing.T, sv Service) Service {
	t.Helper()
	got, err := w.r.CreateService(context.Background(), sv)
	if err != nil {
		t.Fatalf("CreateService(%s): %v", sv.Name, err)
	}
	return got
}

func (w *regWorld) mustBind(t *testing.T, b Binding) Binding {
	t.Helper()
	got, err := w.r.SetBinding(context.Background(), b)
	if err != nil {
		t.Fatalf("SetBinding(%s/%s): %v", b.App, b.UseName, err)
	}
	return got
}

// The two kinds the tests use, as the registry sees a kind.
var (
	tokenFields = []ServiceField{{Key: "url", Required: true}, {Key: "token", Required: true, Secret: true}}
	mailFields  = []ServiceField{{Key: "host", Required: true}, {Key: "port"}, {Key: "username"}, {Key: "password", Secret: true}}
)

const (
	testSecret = "tok-PLAINTEXT-secret-value-9f3a"
	testIssued = "issued-PLAINTEXT-credential-77c1"
)

func TestRegistryServiceRoundTrip(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	values := map[string]string{"url": "http://demo:8080", "token": testSecret, "note": "多行\n的值 with \"quotes\""}
	made := w.mustCreate(t, Service{Name: "demo", Kind: "demo_token", Source: state.ServiceFromApp, App: "demo", ProvideIndex: 1, Values: values})
	if !reflect.DeepEqual(made.Values, values) || made.CreatedAt.IsZero() {
		t.Fatalf("created: %+v", made)
	}
	got, err := w.r.GetService(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, made) {
		t.Fatalf("read back %+v, created %+v", got, made)
	}
	if got.Kind != "demo_token" || got.Source != state.ServiceFromApp || got.App != "demo" || got.ProvideIndex != 1 {
		t.Fatalf("round trip: %+v", got)
	}

	// A service with no values yet: nothing is encrypted, the map is usable.
	w.mustCreate(t, Service{Name: "empty", Kind: "smtp", Source: state.ServiceFromUser})
	row, err := w.st.GetService(ctx, "empty")
	if err != nil || row.ValuesEnc != "" {
		t.Fatalf("empty values stored as %q (%v)", row.ValuesEnc, err)
	}
	if got, err := w.r.GetService(ctx, "empty"); err != nil || got.Values == nil || len(got.Values) != 0 {
		t.Fatalf("empty values read as %#v (%v)", got.Values, err)
	}

	// The store's refusals come through unchanged.
	if _, err := w.r.GetService(ctx, "nope"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := w.r.CreateService(ctx, Service{Name: "demo", Kind: "smtp", Source: state.ServiceFromUser}); !errors.Is(err, state.ErrConflict) || !errors.Is(err, state.ErrServiceNameTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := w.r.CreateService(ctx, Service{Name: "demo-again", Kind: "demo_token", Source: state.ServiceFromApp, App: "demo", ProvideIndex: 1}); !errors.Is(err, state.ErrServiceEntryTaken) {
		t.Fatalf("duplicate entry: %v", err)
	}
	for _, bad := range []string{"Bad Name", "a/b", "kinds", ""} {
		if _, err := w.r.CreateService(ctx, Service{Name: bad, Kind: "smtp", Source: state.ServiceFromUser}); !errors.Is(err, state.ErrInvalid) {
			t.Fatalf("bad name %q: %v", bad, err)
		}
	}
	if _, err := w.r.CreateService(ctx, Service{Name: "nokey", Kind: "smtp", Source: state.ServiceFromUser, Values: map[string]string{"": "x"}}); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("empty item name: %v", err)
	}
	if _, err := w.r.GetService(ctx, "nokey"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("a refused service was stored: %v", err)
	}
}

// A service is called what the user calls it: the name goes through every
// operation as given.
func TestRegistryNamesInTheUsersWords(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	for _, name := range []string{"我的自建邮箱", "我的上游", "My-Mail", "mail.work"} {
		w.mustCreate(t, Service{Name: name, Kind: "smtp", Source: state.ServiceFromUser, Values: map[string]string{"host": "mail.example.com"}})
		got, err := w.r.GetService(ctx, name)
		if err != nil || got.Name != name || got.Values["host"] != "mail.example.com" {
			t.Fatalf("%s: read %+v (%v)", name, got, err)
		}
		if _, err := w.r.UpdateService(ctx, name, func(sv *Service) error { sv.Values["port"] = "465"; return nil }); err != nil {
			t.Fatalf("%s: update: %v", name, err)
		}
		b := w.mustBind(t, Binding{App: "blog", UseName: "mail", Service: name})
		if b.Service != name {
			t.Fatalf("%s: bound to %q", name, b.Service)
		}
		var inUse *state.ServiceInUseError
		if err := w.r.DeleteService(ctx, name); !errors.As(err, &inUse) || !reflect.DeepEqual(inUse.Services(), []string{name}) {
			t.Fatalf("%s: delete in use: %v", name, err)
		}
		if ok, err := w.r.DeleteBinding(ctx, "blog", "mail"); err != nil || !ok {
			t.Fatalf("%s: unbind: %v %v", name, ok, err)
		}
		if err := w.r.DeleteService(ctx, name); err != nil {
			t.Fatalf("%s: delete: %v", name, err)
		}
	}
	// The same word in another case is another service.
	w.mustCreate(t, Service{Name: "Mail", Kind: "smtp", Source: state.ServiceFromUser})
	w.mustCreate(t, Service{Name: "mail", Kind: "smtp", Source: state.ServiceFromUser})
}

// Bytes that are not text are refused, not stored as something else.
func TestRegistryRefusesInvalidUTF8(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	bad := "tok\xff\xfe-value"
	if _, err := w.r.CreateService(ctx, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: map[string]string{"token": bad}}); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("create with invalid UTF-8: %v", err)
	}
	if _, err := w.r.GetService(ctx, "mine"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("a refused service was stored: %v", err)
	}
	if _, err := w.r.CreateService(ctx, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: map[string]string{"to\xffken": "v"}}); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("create with an invalid item name: %v", err)
	}
	good := map[string]string{"token": "密钥 – ünïcödé ✓ \u0000 \t<&>"}
	w.mustCreate(t, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: good})
	if _, err := w.r.UpdateService(ctx, "mine", func(sv *Service) error { sv.Values["token"] = bad; return nil }); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("update with invalid UTF-8: %v", err)
	}
	// Any text comes back as given, and the refused update left it alone.
	if got, err := w.r.GetService(ctx, "mine"); err != nil || !reflect.DeepEqual(got.Values, good) {
		t.Fatalf("read back %q (%v)", got.Values, err)
	}
	if _, err := w.r.SetBinding(ctx, Binding{App: "chat", UseName: "llm", Service: "mine", Issued: map[string]string{"api_key": bad}}); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("bind with invalid UTF-8: %v", err)
	}
	if _, err := w.r.GetBinding(ctx, "chat", "llm"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("a refused binding was stored: %v", err)
	}
}

// fn of UpdateService runs on the database's only connection: a call to the
// registry from inside it cannot get one. This pins down what the usage notes
// warn about, so a change that lifts the limit also updates the notes.
func TestRegistryUpdateServiceFnMustNotCallBack(t *testing.T) {
	w := newRegWorld(t)
	w.mustCreate(t, Service{Name: "mail", Kind: "smtp", Source: state.ServiceFromUser})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := w.r.UpdateService(ctx, "mail", func(sv *Service) error {
		_, err := w.r.ListBindings(ctx, state.BindingFilter{Service: "mail"})
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a nested call returned %v, want it to wait until the context ends", err)
	}
	// The way to do it: look up first, then update.
	if _, err := w.r.ListBindings(context.Background(), state.BindingFilter{Service: "mail"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.r.UpdateService(context.Background(), "mail", func(sv *Service) error { sv.Values["host"] = "h"; return nil }); err != nil {
		t.Fatalf("the registry is stuck after the refused call: %v", err)
	}
}

func TestRegistryUpdateService(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	w.mustCreate(t, Service{Name: "mail", Kind: "smtp", Source: state.ServiceFromUser, Values: map[string]string{"host": "smtp.example.com", "password": testSecret}})
	before, _ := w.st.GetService(ctx, "mail")

	got, err := w.r.UpdateService(ctx, "mail", func(sv *Service) error {
		if sv.Values["password"] != testSecret {
			t.Errorf("fn did not get the values in clear: %v", sv.Values)
		}
		sv.Values["port"] = "465"
		delete(sv.Values, "host")
		sv.Name, sv.Kind = "renamed", "other" // not changeable
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"port": "465", "password": testSecret}
	if !reflect.DeepEqual(got.Values, want) || got.Name != "mail" || got.Kind != "smtp" {
		t.Fatalf("update returned %+v", got)
	}
	read, err := w.r.GetService(ctx, "mail")
	if err != nil || !reflect.DeepEqual(read, got) {
		t.Fatalf("stored %+v, returned %+v (%v)", read, got, err)
	}
	after, _ := w.st.GetService(ctx, "mail")
	if after.ValuesEnc == before.ValuesEnc || after.ValuesEnc == "" {
		t.Fatalf("ciphertext did not change: %q", after.ValuesEnc)
	}

	// Replacing the map, and emptying it.
	if got, err := w.r.UpdateService(ctx, "mail", func(sv *Service) error { sv.Values = nil; return nil }); err != nil || got.Values == nil || len(got.Values) != 0 {
		t.Fatalf("emptied: %#v %v", got.Values, err)
	}
	if row, _ := w.st.GetService(ctx, "mail"); row.ValuesEnc != "" {
		t.Fatalf("emptied values stored as %q", row.ValuesEnc)
	}

	// An error from fn changes nothing.
	boom := errors.New("boom")
	if _, err := w.r.UpdateService(ctx, "mail", func(sv *Service) error { sv.Values["host"] = "lost"; return boom }); !errors.Is(err, boom) {
		t.Fatalf("fn error: %v", err)
	}
	if read, _ := w.r.GetService(ctx, "mail"); len(read.Values) != 0 {
		t.Fatalf("a failed update left values: %v", read.Values)
	}
	if _, err := w.r.UpdateService(ctx, "nope", func(*Service) error { return nil }); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	// The entry of an app's service can be moved.
	w.mustCreate(t, Service{Name: "gw", Kind: "demo_token", Source: state.ServiceFromApp, App: "gw"})
	if got, err := w.r.UpdateService(ctx, "gw", func(sv *Service) error { sv.ProvideIndex = 3; return nil }); err != nil || got.ProvideIndex != 3 {
		t.Fatalf("moved entry: %+v %v", got, err)
	}
}

func TestRegistryListAndDeleteServices(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	if svs, err := w.r.ListServices(ctx, state.ServiceFilter{}); err != nil || len(svs) != 0 {
		t.Fatalf("empty registry: %v %v", svs, err)
	}
	w.mustCreate(t, Service{Name: "gw", Kind: "demo_token", Source: state.ServiceFromApp, App: "gw", Values: map[string]string{"url": "http://gw:3000"}})
	w.mustCreate(t, Service{Name: "gw-smtp", Kind: "smtp", Source: state.ServiceFromApp, App: "gw", ProvideIndex: 1})
	w.mustCreate(t, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: map[string]string{"url": "https://x.example", "token": testSecret}})

	names := func(svs []Service) []string {
		var out []string
		for _, sv := range svs {
			out = append(out, sv.Name)
		}
		return out
	}
	all, err := w.r.ListServices(ctx, state.ServiceFilter{})
	if err != nil || !reflect.DeepEqual(names(all), []string{"gw", "gw-smtp", "mine"}) {
		t.Fatalf("all: %v %v", names(all), err)
	}
	if all[2].Values["token"] != testSecret || all[0].Values["url"] != "http://gw:3000" {
		t.Fatalf("listed values are not in clear: %+v", all)
	}
	if svs, _ := w.r.ListServices(ctx, state.ServiceFilter{Kind: "demo_token"}); !reflect.DeepEqual(names(svs), []string{"gw", "mine"}) {
		t.Fatalf("by kind: %v", names(svs))
	}
	if svs, _ := w.r.ListServices(ctx, state.ServiceFilter{App: "gw"}); !reflect.DeepEqual(names(svs), []string{"gw", "gw-smtp"}) {
		t.Fatalf("by app: %v", names(svs))
	}

	// In use: refused, with the apps that use it.
	w.mustBind(t, Binding{App: "chat", UseName: "llm", Service: "gw"})
	w.mustBind(t, Binding{App: "agent", UseName: "llm", Service: "gw"})
	err = w.r.DeleteService(ctx, "gw")
	var inUse *state.ServiceInUseError
	if !errors.As(err, &inUse) || !reflect.DeepEqual(inUse.Apps(), []string{"agent", "chat"}) {
		t.Fatalf("delete in use: %v", err)
	}
	if removed, err := w.r.DeleteAppServices(ctx, "gw"); !errors.As(err, &inUse) || removed != nil {
		t.Fatalf("delete app services in use: %v %v", removed, err)
	}
	if _, err := w.r.GetService(ctx, "gw-smtp"); err != nil {
		t.Fatalf("a refused delete removed the app's other service: %v", err)
	}
	if err := w.r.DeleteService(ctx, "nope"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	// Unused: both ways of deleting work.
	if err := w.r.DeleteService(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	for _, app := range []string{"chat", "agent"} {
		if n, err := w.r.DeleteAppBindings(ctx, app); err != nil || n != 1 {
			t.Fatalf("unbind %s: %d %v", app, n, err)
		}
	}
	removed, err := w.r.DeleteAppServices(ctx, "gw")
	if err != nil || !reflect.DeepEqual(removed, []string{"gw", "gw-smtp"}) {
		t.Fatalf("delete app services: %v %v", removed, err)
	}
	if svs, _ := w.r.ListServices(ctx, state.ServiceFilter{}); len(svs) != 0 {
		t.Fatalf("left: %v", names(svs))
	}
}

func TestRegistryBindings(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	w.mustCreate(t, Service{Name: "gw", Kind: "demo_token", Source: state.ServiceFromApp, App: "gw"})
	w.mustCreate(t, Service{Name: "mail", Kind: "smtp", Source: state.ServiceFromUser})

	issued := map[string]string{"api_key": testIssued, "token_id": "42"}
	made := w.mustBind(t, Binding{App: "chat", UseName: "llm", Service: "gw", Issued: issued})
	if !reflect.DeepEqual(made.Issued, issued) || made.CreatedAt.IsZero() {
		t.Fatalf("bound: %+v", made)
	}
	got, err := w.r.GetBinding(ctx, "chat", "llm")
	if err != nil || !reflect.DeepEqual(got, made) {
		t.Fatalf("read back %+v, bound %+v (%v)", got, made, err)
	}
	// Nothing issued: stored as nothing, read as an empty set.
	w.mustBind(t, Binding{App: "chat", UseName: "mail", Service: "mail"})
	if row, _ := w.st.GetServiceBinding(ctx, "chat", "mail"); row.IssuedEnc != "" {
		t.Fatalf("nothing issued stored as %q", row.IssuedEnc)
	}
	if b, err := w.r.GetBinding(ctx, "chat", "mail"); err != nil || b.Issued == nil || len(b.Issued) != 0 {
		t.Fatalf("nothing issued read as %#v (%v)", b.Issued, err)
	}
	w.mustBind(t, Binding{App: "agent", UseName: "llm", Service: "gw", Issued: map[string]string{"api_key": "another"}})

	// Issued again: the same binding, the new credentials.
	again := w.mustBind(t, Binding{App: "chat", UseName: "llm", Service: "gw", Issued: map[string]string{"api_key": "renewed"}})
	if !reflect.DeepEqual(again.Issued, map[string]string{"api_key": "renewed"}) || !again.CreatedAt.Equal(made.CreatedAt) {
		t.Fatalf("renewed: %+v", again)
	}

	keys := func(bs []Binding) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.App+"/"+b.UseName+"->"+b.Service)
		}
		return out
	}
	all, err := w.r.ListBindings(ctx, state.BindingFilter{})
	if err != nil || !reflect.DeepEqual(keys(all), []string{"agent/llm->gw", "chat/llm->gw", "chat/mail->mail"}) {
		t.Fatalf("all: %v %v", keys(all), err)
	}
	if all[0].Issued["api_key"] != "another" || all[1].Issued["api_key"] != "renewed" {
		t.Fatalf("listed credentials are not in clear: %+v", all)
	}
	if bs, _ := w.r.ListBindings(ctx, state.BindingFilter{App: "chat"}); !reflect.DeepEqual(keys(bs), []string{"chat/llm->gw", "chat/mail->mail"}) {
		t.Fatalf("of an app: %v", keys(bs))
	}
	if bs, _ := w.r.ListBindings(ctx, state.BindingFilter{Service: "gw"}); !reflect.DeepEqual(keys(bs), []string{"agent/llm->gw", "chat/llm->gw"}) {
		t.Fatalf("to a service: %v", keys(bs))
	}

	if _, err := w.r.GetBinding(ctx, "chat", "nope"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := w.r.SetBinding(ctx, Binding{App: "chat", UseName: "db", Service: "nope"}); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing service: %v", err)
	}
	if _, err := w.r.SetBinding(ctx, Binding{App: "Chat", UseName: "llm", Service: "gw"}); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("bad app: %v", err)
	}
	// Moving a need to another service overwrites what it held: the caller
	// reads the old credentials first (GetBinding) to take them back.
	old, err := w.r.GetBinding(ctx, "agent", "llm")
	if err != nil || old.Issued["api_key"] != "another" {
		t.Fatalf("before moving: %+v %v", old, err)
	}
	if moved := w.mustBind(t, Binding{App: "agent", UseName: "llm", Service: "mail"}); moved.Service != "mail" || len(moved.Issued) != 0 {
		t.Fatalf("moved: %+v", moved)
	}
	if now, _ := w.r.GetBinding(ctx, "agent", "llm"); now.Service != "mail" || len(now.Issued) != 0 {
		t.Fatalf("after moving: %+v", now)
	}
	if n, err := w.r.DeleteAppBindings(ctx, "agent"); err != nil || n != 1 {
		t.Fatalf("unbind agent: %d %v", n, err)
	}
	if _, err := w.r.SetBinding(ctx, Binding{App: "chat", UseName: "db", Service: "gw", Issued: map[string]string{"": "x"}}); !errors.Is(err, state.ErrInvalid) {
		t.Fatalf("empty key: %v", err)
	}

	if ok, err := w.r.DeleteBinding(ctx, "chat", "mail"); err != nil || !ok {
		t.Fatalf("unbind: %v %v", ok, err)
	}
	if ok, err := w.r.DeleteBinding(ctx, "chat", "mail"); err != nil || ok {
		t.Fatalf("unbind again: %v %v", ok, err)
	}
	if n, err := w.r.DeleteAppBindings(ctx, "chat"); err != nil || n != 1 {
		t.Fatalf("unbind app: %d %v", n, err)
	}
	if n, err := w.r.DeleteAppBindings(ctx, "chat"); err != nil || n != 0 {
		t.Fatalf("unbind app again: %d %v", n, err)
	}
}

// What is stored holds no value in clear: not the store's rows, not the
// database files.
func TestRegistryStoresNoPlaintext(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	w.mustCreate(t, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser,
		Values: map[string]string{"url": "https://plain-host.example/v1", "token": testSecret}})
	w.mustBind(t, Binding{App: "chat", UseName: "llm", Service: "mine", Issued: map[string]string{"api_key": testIssued}})
	if _, err := w.r.UpdateService(ctx, "mine", func(sv *Service) error { sv.Values["extra"] = "updated-PLAINTEXT-5521"; return nil }); err != nil {
		t.Fatal(err)
	}
	plains := []string{testSecret, testIssued, "plain-host.example", "updated-PLAINTEXT-5521", `"token"`, `"api_key"`}

	row, err := w.st.GetService(ctx, "mine")
	if err != nil {
		t.Fatal(err)
	}
	brow, err := w.st.GetServiceBinding(ctx, "chat", "llm")
	if err != nil {
		t.Fatal(err)
	}
	for _, enc := range []string{row.ValuesEnc, brow.IssuedEnc} {
		if !strings.HasPrefix(enc, "v1:") {
			t.Fatalf("stored value %q is not ciphertext", enc)
		}
		for _, p := range plains {
			if strings.Contains(enc, p) {
				t.Errorf("ciphertext %q contains %q", enc, p)
			}
		}
	}

	// The same values encrypted twice do not give the same ciphertext.
	a, err := w.r.sealValues(map[string]string{"token": testSecret})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := w.r.sealValues(map[string]string{"token": testSecret})
	if a == b {
		t.Error("two encryptions of the same values are identical")
	}

	// Nothing in clear in the files of the database either.
	if err := w.st.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(w.dbPath + "*")
	if err != nil || len(files) == 0 {
		t.Fatalf("database files: %v %v", files, err)
	}
	sawService := false
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(row.ValuesEnc)) {
			sawService = true
		}
		for _, p := range plains[:4] {
			if bytes.Contains(data, []byte(p)) {
				t.Errorf("%s contains %q in clear", filepath.Base(f), p)
			}
		}
	}
	if !sawService {
		t.Error("the ciphertext was not found in the database files: the check above looked at nothing")
	}
}

func TestRegistryUnreadableValues(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	w.mustCreate(t, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: map[string]string{"token": testSecret}})
	w.mustBind(t, Binding{App: "chat", UseName: "llm", Service: "mine", Issued: map[string]string{"api_key": testIssued}})

	// A registry with the same key file reads them (a restart).
	same := NewRegistry(w.st, w.dataDir)
	if sv, err := same.GetService(ctx, "mine"); err != nil || sv.Values["token"] != testSecret {
		t.Fatalf("same key: %+v %v", sv, err)
	}

	// Another key does not, and says which row.
	other := NewRegistry(w.st, t.TempDir())
	_, err := other.GetService(ctx, "mine")
	var bad *ServiceUnreadableError
	if !errors.Is(err, ErrServiceUnreadable) || !strings.Contains(err.Error(), "mine") {
		t.Fatalf("other key, service: %v", err)
	}
	if !errors.As(err, &bad) || bad.Service != "mine" || bad.App != "" || bad.UseName != "" || bad.Err == nil {
		t.Fatalf("other key, service: %#v", bad)
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Fatalf("the error shows the value: %v", err)
	}
	if _, err := other.ListServices(ctx, state.ServiceFilter{}); !errors.Is(err, ErrServiceUnreadable) {
		t.Fatalf("other key, list: %v", err)
	}
	_, err = other.GetBinding(ctx, "chat", "llm")
	if !errors.Is(err, ErrServiceUnreadable) || !strings.Contains(err.Error(), "chat") {
		t.Fatalf("other key, binding: %v", err)
	}
	if !errors.As(err, &bad) || bad.Service != "mine" || bad.App != "chat" || bad.UseName != "llm" {
		t.Fatalf("other key, binding: %#v", bad)
	}
	if strings.Contains(err.Error(), testIssued) {
		t.Fatalf("the error shows the credential: %v", err)
	}
	if _, err := other.ListBindings(ctx, state.BindingFilter{}); !errors.Is(err, ErrServiceUnreadable) {
		t.Fatalf("other key, bindings: %v", err)
	}
	// It cannot overwrite what it cannot read.
	if _, err := other.UpdateService(ctx, "mine", func(sv *Service) error { sv.Values = map[string]string{"token": "x"}; return nil }); !errors.Is(err, ErrServiceUnreadable) {
		t.Fatalf("other key, update: %v", err)
	}

	// Unreadable finds every such row without stopping at the first, rows
	// with nothing encrypted are not among them, and a registry that reads
	// everything finds none.
	w.mustCreate(t, Service{Name: "empty", Kind: "smtp", Source: state.ServiceFromUser})
	w.mustCreate(t, Service{Name: "another", Kind: "demo_token", Source: state.ServiceFromUser, Values: map[string]string{"token": "x"}})
	w.mustBind(t, Binding{App: "blog", UseName: "mail", Service: "empty"})
	if rows, err := w.r.Unreadable(ctx); err != nil || rows != nil {
		t.Fatalf("own key: %v %v", rows, err)
	}
	rows, err := other.Unreadable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, row := range rows {
		found = append(found, row.Service+"|"+row.App+"|"+row.UseName)
	}
	if !reflect.DeepEqual(found, []string{"another||", "mine||", "mine|chat|llm"}) {
		t.Fatalf("unreadable rows: %v", found)
	}
	// What cannot be read can still be unplugged and deleted.
	if ok, err := other.DeleteBinding(ctx, "chat", "llm"); err != nil || !ok {
		t.Fatalf("other key, unbind: %v %v", ok, err)
	}
	if err := other.DeleteService(ctx, "another"); err != nil {
		t.Fatalf("other key, delete: %v", err)
	}
	if rows, _ := other.Unreadable(ctx); len(rows) != 1 || rows[0].Service != "mine" {
		t.Fatalf("after cleaning up: %v", rows)
	}

	// A damaged row is refused too.
	if _, err := w.st.UpdateService(ctx, "mine", func(row *state.Service) error {
		row.ValuesEnc = row.ValuesEnc[:len(row.ValuesEnc)-4] + "AAAA"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.r.GetService(ctx, "mine"); !errors.Is(err, ErrServiceUnreadable) {
		t.Fatalf("damaged row: %v", err)
	}
	if _, err := w.st.UpdateService(ctx, "mine", func(row *state.Service) error { row.ValuesEnc = "plain text"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := w.r.GetService(ctx, "mine"); !errors.Is(err, ErrServiceUnreadable) {
		t.Fatalf("not ciphertext: %v", err)
	}
}

func TestEngineRegistry(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()
	e := New(Config{Store: w.st, DataDir: w.dataDir})
	r, err := e.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateService(ctx, Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: map[string]string{"token": testSecret}}); err != nil {
		t.Fatal(err)
	}
	// The engine's registry and a registry on the same data directory read
	// each other's values: one key.
	if sv, err := w.r.GetService(ctx, "mine"); err != nil || sv.Values["token"] != testSecret {
		t.Fatalf("same data dir: %+v %v", sv, err)
	}
	// A store without the registry operations is refused, not used.
	narrow := New(Config{Store: struct{ Store }{w.st}, DataDir: w.dataDir})
	if r, err := narrow.Registry(); err == nil || r != nil {
		t.Fatalf("narrow store: %v %v", r, err)
	}
}

func TestMaskServiceSecret(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"a":                Masked,
		"1234567":          Masked,          // 7: too short to show any of it
		"12345678":         Masked + "5678", // 8: the last 4
		"sk-abcdefghij":    Masked + "ghij",
		"密码密码密码密":          Masked,          // 7 characters, not 7 bytes
		"密码密码密码密码":         Masked + "密码密码", // 8 characters
		"with space 1234 ": Masked + "234 ",
	} {
		if got := MaskServiceSecret(in); got != want {
			t.Errorf("MaskServiceSecret(%q) = %q, want %q", in, got, want)
		}
	}
	// What is shown never holds more than the last 4 characters.
	if got := MaskServiceSecret(testSecret); strings.Contains(got, testSecret[:len(testSecret)-4]) || !strings.HasSuffix(got, "9f3a") {
		t.Errorf("masked %q", got)
	}
}

func TestMaskServiceValues(t *testing.T) {
	values := map[string]string{
		"host":     "smtp.example.com",
		"port":     "587",
		"password": "correct-horse-battery",
		"unknown":  "not-in-the-kind-1234",
		"username": "",
	}
	orig := map[string]string{}
	for k, v := range values {
		orig[k] = v
	}
	got := MaskServiceValues(values, mailFields)
	want := map[string]string{
		"host":     "smtp.example.com",
		"port":     "587",
		"password": Masked + "tery",
		"unknown":  Masked + "1234", // not an item of the kind: treated as secret
		"username": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("masked = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(values, orig) {
		t.Errorf("the input was changed: %v", values)
	}
	// With no definition of the kind everything is masked.
	if got := MaskServiceValues(values, nil); got["host"] != Masked+".com" || got["port"] != Masked {
		t.Errorf("without fields: %v", got)
	}
	if got := MaskServiceValues(nil, mailFields); got == nil || len(got) != 0 {
		t.Errorf("nil values: %#v", got)
	}
	// Issued credentials: all of them.
	issued := MaskIssued(map[string]string{"api_key": testIssued, "token_id": "42", "user": "acornfox-chat"})
	if !reflect.DeepEqual(issued, map[string]string{"api_key": Masked + "77c1", "token_id": Masked, "user": Masked + "chat"}) {
		t.Errorf("issued = %v", issued)
	}
}

func TestServiceStatusOf(t *testing.T) {
	full := map[string]string{"url": "http://gw:3000", "token": testSecret}
	fromApp := func(values map[string]string) Service {
		return Service{Name: "gw", Kind: "demo_token", Source: state.ServiceFromApp, App: "gw", Values: values}
	}
	fromUser := func(values map[string]string) Service {
		return Service{Name: "mine", Kind: "demo_token", Source: state.ServiceFromUser, Values: values}
	}
	// health spells out what is expected: the status, its label, and the
	// missing items as a list that is never nil.
	health := func(status ServiceStatus, label string, missing ...string) ServiceHealth {
		if missing == nil {
			missing = []string{}
		}
		return ServiceHealth{Status: status, StatusLabel: label, Missing: missing}
	}
	ok := health(ServiceOK, "可用")
	for name, c := range map[string]struct {
		sv          Service
		fields      []ServiceField
		perConsumer []string
		provider    ProviderState
		want        ServiceHealth
	}{
		// The four statuses.
		"ok": {fromApp(full), tokenFields, nil, ProviderRunning, ok},
		"incomplete": {fromApp(map[string]string{"url": "http://gw:3000"}), tokenFields, nil, ProviderRunning,
			health(ServiceIncomplete, "还差必填项", "token")},
		"provider stopped": {fromApp(full), tokenFields, nil, ProviderStopped,
			health(ServiceProviderStopped, "提供它的应用停着")},
		"provider gone": {fromApp(full), tokenFields, nil, ProviderGone,
			health(ServiceProviderGone, "提供它的应用已删")},

		// A user's service has no provider: only its values count.
		"user, ok":                     {fromUser(full), tokenFields, nil, ProviderNotApplicable, ok},
		"user, provider state ignored": {fromUser(full), tokenFields, nil, ProviderGone, ok},
		"user, nothing filled": {fromUser(nil), tokenFields, nil, ProviderNotApplicable,
			health(ServiceIncomplete, "还差必填项", "url", "token")},
		"user, empty value is no value": {fromUser(map[string]string{"url": "", "token": testSecret}), tokenFields, nil, ProviderNotApplicable,
			health(ServiceIncomplete, "还差必填项", "url")},

		// Items that are not required never count.
		"optional items missing": {fromUser(map[string]string{"host": "smtp.example.com"}), mailFields, nil, ProviderNotApplicable, ok},
		"a kind with no fields":  {fromUser(nil), nil, nil, ProviderNotApplicable, ok},

		// What the provider issues per consumer is not expected here.
		"issued per consumer": {fromApp(map[string]string{"url": "http://gw:3000"}), tokenFields, []string{"token"}, ProviderRunning, ok},
		"issued per consumer, another item missing": {fromApp(nil), tokenFields, []string{"token"}, ProviderRunning,
			health(ServiceIncomplete, "还差必填项", "url")},

		// The provider's state comes first; what is missing is still said.
		"stopped and incomplete": {fromApp(nil), tokenFields, nil, ProviderStopped,
			health(ServiceProviderStopped, "提供它的应用停着", "url", "token")},
		"gone and incomplete": {fromApp(nil), tokenFields, nil, ProviderGone,
			health(ServiceProviderGone, "提供它的应用已删", "url", "token")},
		// Only a running provider makes an app's service usable.
		"app, state not given": {fromApp(full), tokenFields, nil, "",
			health(ServiceProviderStopped, "提供它的应用停着")},
		"app, state not applicable": {fromApp(full), tokenFields, nil, ProviderNotApplicable,
			health(ServiceProviderStopped, "提供它的应用停着")},
	} {
		got := ServiceStatusOf(c.sv, c.fields, c.perConsumer, c.provider)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %#v, want %#v", name, got, c.want)
		}
		if got.Usable() != (c.want.Status == ServiceOK) {
			t.Errorf("%s: Usable() = %v", name, got.Usable())
		}
	}
}

// The status words and the shape of ServiceHealth are what the pages, the
// command line and other programs read: they are written out here in full,
// so changing a constant or a tag fails this test.
func TestServiceStatusWireForm(t *testing.T) {
	for status, want := range map[ServiceStatus][2]string{
		ServiceOK:              {"ok", "可用"},
		ServiceIncomplete:      {"incomplete", "还差必填项"},
		ServiceProviderStopped: {"provider_stopped", "提供它的应用停着"},
		ServiceProviderGone:    {"provider_gone", "提供它的应用已删"},
	} {
		if string(status) != want[0] || status.Label() != want[1] {
			t.Errorf("status %q labelled %q, want %q labelled %q", string(status), status.Label(), want[0], want[1])
		}
	}
	if got := ServiceStatus("something else").Label(); got != "" {
		t.Errorf("unknown status labelled %q", got)
	}

	full := Service{Source: state.ServiceFromUser, Values: map[string]string{"url": "u", "token": "t"}}
	for name, c := range map[string]struct {
		h    ServiceHealth
		want string
	}{
		"ok": {ServiceStatusOf(full, tokenFields, nil, ProviderNotApplicable),
			`{"status":"ok","status_label":"可用","missing":[]}`},
		"incomplete": {ServiceStatusOf(Service{Source: state.ServiceFromUser}, tokenFields, nil, ProviderNotApplicable),
			`{"status":"incomplete","status_label":"还差必填项","missing":["url","token"]}`},
		"provider stopped": {ServiceStatusOf(Service{Source: state.ServiceFromApp, App: "gw", Values: full.Values}, tokenFields, nil, ProviderStopped),
			`{"status":"provider_stopped","status_label":"提供它的应用停着","missing":[]}`},
		"provider gone": {ServiceStatusOf(Service{Source: state.ServiceFromApp, App: "gw", Values: full.Values}, tokenFields, nil, ProviderGone),
			`{"status":"provider_gone","status_label":"提供它的应用已删","missing":[]}`},
	} {
		raw, err := json.Marshal(c.h)
		if err != nil || string(raw) != c.want {
			t.Errorf("%s: %s (%v), want %s", name, raw, err, c.want)
		}
	}
}

func TestProviderStateOf(t *testing.T) {
	app := Service{Source: state.ServiceFromApp, App: "gw"}
	user := Service{Source: state.ServiceFromUser}
	running := &state.App{Name: "gw", Desired: state.DesiredRunning}
	stopped := &state.App{Name: "gw", Desired: state.DesiredStopped}
	for name, c := range map[string]struct {
		sv   Service
		app  *state.App
		want ProviderState
	}{
		"app running":          {app, running, ProviderRunning},
		"app stopped":          {app, stopped, ProviderStopped},
		"app deleted":          {app, nil, ProviderGone},
		"user's service":       {user, nil, ProviderNotApplicable},
		"user's, app given":    {user, stopped, ProviderNotApplicable},
		"source not filled in": {Service{}, running, ProviderNotApplicable},
	} {
		if got := ProviderStateOf(c.sv, c.app); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	// End to end: a stopped provider makes its service unusable.
	sv := Service{Source: state.ServiceFromApp, App: "gw", Values: map[string]string{"url": "u", "token": "t"}}
	if h := ServiceStatusOf(sv, tokenFields, nil, ProviderStateOf(sv, stopped)); h.Status != ServiceProviderStopped || h.Usable() {
		t.Errorf("stopped provider: %+v", h)
	}
	if h := ServiceStatusOf(sv, tokenFields, nil, ProviderStateOf(sv, running)); !h.Usable() {
		t.Errorf("running provider: %+v", h)
	}
}

func TestDefaultServiceName(t *testing.T) {
	takenSet := func(names ...string) func(string) bool {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		return func(n string) bool { return m[n] }
	}
	for name, c := range map[string]struct {
		app   string
		kinds []string
		index int
		taken func(string) bool
		want  string
	}{
		"one entry: the app's name":         {"gw", []string{"llm_gateway"}, 0, nil, "gw"},
		"several: first":                    {"suite", []string{"llm_gateway", "smtp"}, 0, nil, "suite-llm_gateway"},
		"several: second":                   {"suite", []string{"llm_gateway", "smtp"}, 1, nil, "suite-smtp"},
		"same kind twice: first":            {"suite", []string{"smtp", "smtp"}, 0, nil, "suite-smtp"},
		"same kind twice: second":           {"suite", []string{"smtp", "smtp"}, 1, nil, "suite-smtp-2"},
		"same kind three times, mixed":      {"suite", []string{"smtp", "llm_gateway", "smtp", "smtp"}, 3, nil, "suite-smtp-3"},
		"one entry, name taken":             {"gw", []string{"llm_gateway"}, 0, takenSet("gw"), "gw-2"},
		"one entry, name and -2 taken":      {"gw", []string{"llm_gateway"}, 0, takenSet("gw", "gw-2"), "gw-3"},
		"several, name taken":               {"suite", []string{"llm_gateway", "smtp"}, 1, takenSet("suite-smtp"), "suite-smtp-2"},
		"same kind twice, -2 taken":         {"suite", []string{"smtp", "smtp"}, 1, takenSet("suite-smtp-2"), "suite-smtp-3"},
		"another app's name does not count": {"gw", []string{"llm_gateway"}, 0, takenSet("gw2", "other"), "gw"},
		// A kind may end in "_": the name is still one that can be registered.
		"kind ending in underscore": {"app", []string{"smtp", "foo_"}, 1, nil, "app-foo_"},
		// A word no service can be called is skipped like a taken name.
		"reserved word":           {"kinds", []string{"smtp"}, 0, nil, "kinds-2"},
		"reserved word, -2 taken": {"kinds", []string{"smtp"}, 0, takenSet("kinds-2"), "kinds-3"},
		"index too large":         {"gw", []string{"llm_gateway"}, 1, nil, ""},
		"negative index":          {"gw", []string{"llm_gateway"}, -1, nil, ""},
		"no entries":              {"gw", nil, 0, nil, ""},
	} {
		got := DefaultServiceName(c.app, c.kinds, c.index, c.taken)
		if got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
		if got != "" && !state.ValidServiceName(got) {
			t.Errorf("%s: %q is not a valid service name", name, got)
		}
	}
	// The longest app name with the longest kind still gives a valid name,
	// whatever the kind ends in and however far the number has to go.
	for _, kind := range []string{"k" + strings.Repeat("a", 63), "k" + strings.Repeat("_", 63)} {
		if !state.ValidServiceKind(kind) {
			t.Fatalf("test data: kind %q", kind)
		}
		n := 0
		long := DefaultServiceName("a"+strings.Repeat("b", 39), []string{kind, "smtp"}, 0, func(string) bool { n++; return n <= 1000 })
		if !state.ValidServiceName(long) || !strings.HasSuffix(long, "-1001") {
			t.Errorf("longest default name %q (%d) is not valid", long, len(long))
		}
	}
}

func TestRegistryAppServiceNames(t *testing.T) {
	w := newRegWorld(t)
	ctx := context.Background()

	// Nothing registered: the plain defaults.
	if got, err := w.r.AppServiceNames(ctx, "gw", []string{"llm_gateway"}); err != nil || !reflect.DeepEqual(got, []string{"gw"}) {
		t.Fatalf("one entry: %v %v", got, err)
	}
	if got, _ := w.r.AppServiceNames(ctx, "suite", []string{"smtp", "llm_gateway", "smtp"}); !reflect.DeepEqual(got, []string{"suite-smtp", "suite-llm_gateway", "suite-smtp-2"}) {
		t.Fatalf("several entries: %v", got)
	}
	if got, err := w.r.AppServiceNames(ctx, "gw", nil); err != nil || len(got) != 0 {
		t.Fatalf("no entries: %v %v", got, err)
	}

	// The user already has a service called like the app.
	w.mustCreate(t, Service{Name: "gw", Kind: "smtp", Source: state.ServiceFromUser})
	if got, _ := w.r.AppServiceNames(ctx, "gw", []string{"llm_gateway"}); !reflect.DeepEqual(got, []string{"gw-2"}) {
		t.Fatalf("name taken by the user: %v", got)
	}

	// The names it gives can all be registered, and asking again gives the
	// same names back (an entry that has a service keeps its name).
	w.mustCreate(t, Service{Name: "suite-smtp", Kind: "smtp", Source: state.ServiceFromUser})
	kinds := []string{"smtp", "llm_gateway", "smtp"}
	names, err := w.r.AppServiceNames(ctx, "suite", kinds)
	if err != nil || !reflect.DeepEqual(names, []string{"suite-smtp-2", "suite-llm_gateway", "suite-smtp-3"}) {
		t.Fatalf("with a taken name: %v %v", names, err)
	}
	for i, n := range names {
		w.mustCreate(t, Service{Name: n, Kind: kinds[i], Source: state.ServiceFromApp, App: "suite", ProvideIndex: i})
	}
	if again, _ := w.r.AppServiceNames(ctx, "suite", kinds); !reflect.DeepEqual(again, names) {
		t.Fatalf("asked again: %v, first %v", again, names)
	}
	// A new entry appended by a later version: the old ones keep their
	// names, the new one gets a free one.
	more, _ := w.r.AppServiceNames(ctx, "suite", append(kinds, "llm_gateway"))
	if !reflect.DeepEqual(more, append(append([]string(nil), names...), "suite-llm_gateway-2")) {
		t.Fatalf("with a new entry: %v", more)
	}
	// An app that had one entry and now has two: the first keeps the bare
	// name it was registered with.
	w.mustCreate(t, Service{Name: "solo", Kind: "smtp", Source: state.ServiceFromApp, App: "solo"})
	if got, _ := w.r.AppServiceNames(ctx, "solo", []string{"smtp", "llm_gateway"}); !reflect.DeepEqual(got, []string{"solo", "solo-llm_gateway"}) {
		t.Fatalf("grown from one entry: %v", got)
	}

	// An entry that already has a service of another kind: refused, saying
	// which, instead of handing the old service's name to the new kind.
	got, err := w.r.AppServiceNames(ctx, "solo", []string{"llm_gateway"})
	var changed *ServiceKindChangedError
	if !errors.As(err, &changed) || got != nil {
		t.Fatalf("retyped entry: %v %v", got, err)
	}
	if *changed != (ServiceKindChangedError{App: "solo", Index: 0, Service: "solo", Have: "smtp", Want: "llm_gateway"}) {
		t.Fatalf("retyped entry: %+v", *changed)
	}
	for _, part := range []string{"solo", "smtp", "llm_gateway"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("%q does not mention %q", err, part)
		}
	}
	// Reordered entries are the same case.
	if _, err := w.r.AppServiceNames(ctx, "suite", []string{"llm_gateway", "smtp", "smtp"}); !errors.As(err, &changed) || changed.Index != 0 || changed.Have != "smtp" {
		t.Fatalf("reordered entries: %v", err)
	}
	// Another app with the same kinds is not concerned, and a service whose
	// entry no longer exists is not looked at.
	if got, err := w.r.AppServiceNames(ctx, "other", []string{"llm_gateway"}); err != nil || !reflect.DeepEqual(got, []string{"other"}) {
		t.Fatalf("another app: %v %v", got, err)
	}
	if got, err := w.r.AppServiceNames(ctx, "suite", kinds[:1]); err != nil || !reflect.DeepEqual(got, names[:1]) {
		t.Fatalf("fewer entries: %v %v", got, err)
	}

	// An app whose name is a word no service can be called.
	if got, err := w.r.AppServiceNames(ctx, "kinds", []string{"smtp"}); err != nil || !reflect.DeepEqual(got, []string{"kinds-2"}) {
		t.Fatalf("reserved word: %v %v", got, err)
	}
	w.mustCreate(t, Service{Name: "kinds-2", Kind: "smtp", Source: state.ServiceFromApp, App: "kinds"})
}
