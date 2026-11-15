package state

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func userService(name, kind string) Service {
	return Service{Name: name, Kind: kind, Source: ServiceFromUser, ValuesEnc: "v1:" + name}
}

func appService(name, kind, app string, index int) Service {
	return Service{Name: name, Kind: kind, Source: ServiceFromApp, App: app, ProvideIndex: index, ValuesEnc: "v1:" + name}
}

func mustCreateService(t *testing.T, s *Store, sv Service) Service {
	t.Helper()
	got, err := s.CreateService(context.Background(), sv)
	if err != nil {
		t.Fatalf("CreateService(%s): %v", sv.Name, err)
	}
	return got
}

func mustBind(t *testing.T, s *Store, app, use, service string) ServiceBinding {
	t.Helper()
	b, err := s.SetServiceBinding(context.Background(), ServiceBinding{App: app, UseName: use, Service: service, IssuedEnc: "v1:" + app + "/" + use})
	if err != nil {
		t.Fatalf("SetServiceBinding(%s/%s -> %s): %v", app, use, service, err)
	}
	return b
}

func serviceNames(svs []Service) []string {
	var out []string
	for _, sv := range svs {
		out = append(out, sv.Name)
	}
	return out
}

func bindingKeys(bs []ServiceBinding) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.App+"/"+b.UseName+"->"+b.Service)
	}
	return out
}

// Opening the same database again runs the migrations again: nothing is
// applied twice and what the registry holds is still there.
func TestMigration0014RunsOnceAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acornfox.db")
	ctx := context.Background()
	s, err := Open(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	mustCreateService(t, s, userService("mail", "smtp"))
	mustBind(t, s, "blog", "mail", "mail")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err = Open(Config{Path: path})
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM _schema_migrations WHERE version='0014_service_registry'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("reopen %d: 0014 recorded %d times (%v)", i, n, err)
		}
		if _, err := s.GetService(ctx, "mail"); err != nil {
			t.Fatalf("reopen %d: service lost: %v", i, err)
		}
		if _, err := s.GetServiceBinding(ctx, "blog", "mail"); err != nil {
			t.Fatalf("reopen %d: binding lost: %v", i, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A database from before migration 0014 opens with this version: the two
// tables appear and what was there before is untouched.
func TestMigration0014OnExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acornfox.db")
	ctx := context.Background()
	s, err := Open(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnsureApp(ctx, "old-app"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCatalogManifest(ctx, testManifest("old-app", "id: old-app\n", ManifestFromInstall)); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetApp(ctx, "old-app")
	if err != nil {
		t.Fatal(err)
	}
	// Put the schema back to what 0013 left.
	for _, q := range []string{
		`ALTER TABLE service_bindings DROP COLUMN dedicated`,
		`DELETE FROM _schema_migrations WHERE version='0016_binding_dedicated'`,
		`ALTER TABLE apps DROP COLUMN health_check`,
		`ALTER TABLE apps DROP COLUMN internal`,
		`DELETE FROM _schema_migrations WHERE version='0015_app_internal_health'`,
		`DROP TABLE service_bindings`,
		`DROP TABLE services`,
		`DELETE FROM _schema_migrations WHERE version='0014_service_registry'`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()

	s, err = Open(Config{Path: path})
	if err != nil {
		t.Fatalf("open a 0013 database: %v", err)
	}
	defer s.Close()
	after, err := s.GetApp(ctx, "old-app")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("app changed across the migration: %+v -> %+v (%v)", before, after, err)
	}
	if m, err := s.GetCatalogManifest(ctx, "old-app"); err != nil || m.Manifest != "id: old-app\n" {
		t.Fatalf("manifest after migration: %+v %v", m, err)
	}
	if svs, err := s.ListServices(ctx, ServiceFilter{}); err != nil || len(svs) != 0 {
		t.Fatalf("services after migration: %v %v", svs, err)
	}
	mustCreateService(t, s, appService("old-app", "llm_gateway", "old-app", 0))
	mustBind(t, s, "old-app", "llm", "old-app")
}

// The schema itself refuses what the store's own checks would refuse, so a
// row written past them cannot break the invariants.
func TestServiceTablesConstraints(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := formatTime(time.Now())
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	for name, q := range map[string]string{
		"unknown source":           `INSERT INTO services(name,kind,source,created_at,updated_at) VALUES('a','smtp','other','` + now + `','` + now + `')`,
		"app source without app":   `INSERT INTO services(name,kind,source,created_at,updated_at) VALUES('a','smtp','app','` + now + `','` + now + `')`,
		"user source with app":     `INSERT INTO services(name,kind,source,app,created_at,updated_at) VALUES('a','smtp','user','x','` + now + `','` + now + `')`,
		"second service per entry": `INSERT INTO services(name,kind,source,app,provide_index,created_at,updated_at) VALUES('a','smtp','app','gw',0,'` + now + `','` + now + `')`,
		"bad timestamp":            `INSERT INTO services(name,kind,source,created_at,updated_at) VALUES('a','smtp','user','yesterday','` + now + `')`,
		"binding to nothing":       `INSERT INTO service_bindings(app,use_name,service,created_at) VALUES('blog','mail','nope','` + now + `')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err == nil {
			t.Errorf("%s: the insert went through", name)
		}
	}
	mustBind(t, s, "chat", "llm", "gw")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM services WHERE name='gw'`); err == nil {
		t.Error("a service with a binding was deleted behind the store's back")
	}
}

func TestCreateAndGetService(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Second)

	made := mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 1))
	got, err := s.GetService(ctx, "gw")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(made, got) {
		t.Fatalf("created %+v, read back %+v", made, got)
	}
	if got.Kind != "llm_gateway" || got.Source != ServiceFromApp || got.App != "gw" || got.ProvideIndex != 1 || got.ValuesEnc != "v1:gw" {
		t.Fatalf("round trip: %+v", got)
	}
	if got.CreatedAt.Before(start) || !got.CreatedAt.Equal(got.UpdatedAt) {
		t.Fatalf("timestamps: %v %v", got.CreatedAt, got.UpdatedAt)
	}
	// A user's service, and one with no values yet.
	mustCreateService(t, s, Service{Name: "company-mail", Kind: "smtp", Source: ServiceFromUser})
	if got, err := s.GetService(ctx, "company-mail"); err != nil || got.ValuesEnc != "" || got.App != "" {
		t.Fatalf("user service: %+v %v", got, err)
	}

	if _, err := s.GetService(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// The name is taken, whoever asks; the error says it is the name.
	_, err = s.CreateService(ctx, userService("gw", "smtp"))
	if !errors.Is(err, ErrConflict) || !errors.Is(err, ErrServiceNameTaken) || errors.Is(err, ErrServiceEntryTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
	if !strings.Contains(err.Error(), `"gw"`) || strings.Contains(err.Error(), "entry") {
		t.Fatalf("duplicate name is worded as %q", err)
	}
	// Taken by name and by entry at once: it is the name that is reported.
	if _, err := s.CreateService(ctx, appService("gw", "llm_gateway", "gw", 1)); !errors.Is(err, ErrServiceNameTaken) {
		t.Fatalf("duplicate name and entry: %v", err)
	}
	// One service per provides entry of an app; the error says it is the
	// entry.
	_, err = s.CreateService(ctx, appService("gw-again", "llm_gateway", "gw", 1))
	if !errors.Is(err, ErrConflict) || !errors.Is(err, ErrServiceEntryTaken) || errors.Is(err, ErrServiceNameTaken) {
		t.Fatalf("duplicate entry: %v", err)
	}
	if _, err := s.GetService(ctx, "gw-again"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused service was stored: %v", err)
	}
	mustCreateService(t, s, appService("gw-second", "smtp", "gw", 0))
	// Names are compared exactly: another case is another name.
	mustCreateService(t, s, userService("GW", "smtp"))

	for name, sv := range map[string]Service{
		"empty name":            userService("", "smtp"),
		"name with a space":     userService("my mail", "smtp"),
		"name starting in dash": userService("-mail", "smtp"),
		"name with a slash":     userService("my/mail", "smtp"),
		"name with an equals":   userService("my=mail", "smtp"),
		"reserved name":         userService("kinds", "smtp"),
		"name too long":         userService(strings.Repeat("a", ServiceNameMaxLen+1), "smtp"),
		"name too long, wide":   userService(strings.Repeat("邮", ServiceNameMaxLen+1), "smtp"),
		"empty kind":            userService("m", ""),
		"kind with a dash":      userService("m", "llm-gateway"),
		"kind too long":         userService("m", "k"+strings.Repeat("a", 64)),
		"unknown source":        {Name: "m", Kind: "smtp", Source: "catalog"},
		"app source, no app":    {Name: "m", Kind: "smtp", Source: ServiceFromApp},
		"app source, bad app":   {Name: "m", Kind: "smtp", Source: ServiceFromApp, App: "Bad_App"},
		"user source with app":  {Name: "m", Kind: "smtp", Source: ServiceFromUser, App: "gw"},
		"user source and index": {Name: "m", Kind: "smtp", Source: ServiceFromUser, ProvideIndex: 2},
		"negative index":        appService("m", "smtp", "gw", -1),
	} {
		if _, err := s.CreateService(ctx, sv); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	// The longest names that are accepted: the limit counts characters.
	mustCreateService(t, s, userService(strings.Repeat("a", ServiceNameMaxLen), "k"+strings.Repeat("a", 63)))
	mustCreateService(t, s, userService(strings.Repeat("邮", ServiceNameMaxLen), "smtp"))
	mustCreateService(t, s, userService("9", "smtp"))

	// A name in the user's own words goes all the way: created, read, listed
	// by it, bound to, refused for deletion while in use, deleted.
	for _, name := range []string{"我的自建邮箱", "我的上游", "My-Mail", "mail.work", "公司邮箱_2"} {
		made := mustCreateService(t, s, userService(name, "smtp"))
		got, err := s.GetService(ctx, name)
		if err != nil || !reflect.DeepEqual(made, got) {
			t.Fatalf("%s: created %+v, read back %+v (%v)", name, made, got, err)
		}
		mustBind(t, s, "blog", "mail", name)
		if bs, err := s.ListServiceBindings(ctx, BindingFilter{Service: name}); err != nil || len(bs) != 1 || bs[0].Service != name {
			t.Fatalf("%s: bindings %+v (%v)", name, bs, err)
		}
		var inUse *ServiceInUseError
		if err := s.DeleteService(ctx, name); !errors.As(err, &inUse) || !reflect.DeepEqual(inUse.Services(), []string{name}) {
			t.Fatalf("%s: delete in use: %v", name, err)
		}
		if _, err := s.DeleteAppBindings(ctx, "blog"); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteService(ctx, name); err != nil {
			t.Fatalf("%s: delete: %v", name, err)
		}
	}
}

func TestUpdateService(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	made := mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, appService("gw-mail", "smtp", "gw", 1))

	got, err := s.UpdateService(ctx, "gw", func(sv *Service) error {
		sv.ValuesEnc, sv.ProvideIndex = "v1:new", 2
		// None of these may change.
		sv.Name, sv.Kind, sv.Source, sv.App, sv.CreatedAt = "other", "smtp", ServiceFromUser, "", time.Time{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.GetService(ctx, "gw")
	if err != nil || !reflect.DeepEqual(got, read) {
		t.Fatalf("update returned %+v, stored %+v (%v)", got, read, err)
	}
	if read.ValuesEnc != "v1:new" || read.ProvideIndex != 2 {
		t.Fatalf("not updated: %+v", read)
	}
	if read.Name != "gw" || read.Kind != "llm_gateway" || read.Source != ServiceFromApp || read.App != "gw" || !read.CreatedAt.Equal(made.CreatedAt) {
		t.Fatalf("immutable fields changed: %+v", read)
	}
	if read.UpdatedAt.Before(made.UpdatedAt) {
		t.Fatalf("updated_at went back: %v -> %v", made.UpdatedAt, read.UpdatedAt)
	}
	if _, err := s.GetService(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a renamed copy appeared: %v", err)
	}

	if _, err := s.UpdateService(ctx, "nope", func(*Service) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// fn's error stops the update and nothing is written.
	boom := errors.New("boom")
	if _, err := s.UpdateService(ctx, "gw", func(sv *Service) error { sv.ValuesEnc = "v1:lost"; return boom }); !errors.Is(err, boom) {
		t.Fatalf("fn error: %v", err)
	}
	if _, err := s.UpdateService(ctx, "gw", func(sv *Service) error { sv.ProvideIndex = -1; return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative index: %v", err)
	}
	// Entry 1 of the app is another service's.
	if _, err := s.UpdateService(ctx, "gw", func(sv *Service) error { sv.ProvideIndex = 1; return nil }); !errors.Is(err, ErrConflict) || !errors.Is(err, ErrServiceEntryTaken) {
		t.Fatalf("taken entry: %v", err)
	}
	if read, _ := s.GetService(ctx, "gw"); read.ValuesEnc != "v1:new" || read.ProvideIndex != 2 {
		t.Fatalf("a refused update left a trace: %+v", read)
	}
}

func TestListServices(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if svs, err := s.ListServices(ctx, ServiceFilter{}); err != nil || svs != nil {
		t.Fatalf("empty registry: %v %v", svs, err)
	}
	mustCreateService(t, s, userService("office-mail", "smtp"))
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, appService("gw-smtp", "smtp", "gw", 1))
	mustCreateService(t, s, userService("my-key", "llm_gateway"))
	mustCreateService(t, s, appService("another", "llm_gateway", "another", 0))

	for name, c := range map[string]struct {
		f    ServiceFilter
		want []string
	}{
		"all, by name":       {ServiceFilter{}, []string{"another", "gw", "gw-smtp", "my-key", "office-mail"}},
		"by kind":            {ServiceFilter{Kind: "llm_gateway"}, []string{"another", "gw", "my-key"}},
		"by app":             {ServiceFilter{App: "gw"}, []string{"gw", "gw-smtp"}},
		"by kind and app":    {ServiceFilter{Kind: "smtp", App: "gw"}, []string{"gw-smtp"}},
		"unknown kind":       {ServiceFilter{Kind: "postgres"}, nil},
		"app providing none": {ServiceFilter{App: "office-mail"}, nil},
	} {
		svs, err := s.ListServices(ctx, c.f)
		if err != nil || !reflect.DeepEqual(serviceNames(svs), c.want) {
			t.Errorf("%s: %v (%v), want %v", name, serviceNames(svs), err, c.want)
		}
	}
}

func TestDeleteService(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mustCreateService(t, s, userService("mail", "smtp"))
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))

	if err := s.DeleteService(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// Unused: it goes.
	if err := s.DeleteService(ctx, "mail"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetService(ctx, "mail"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}

	// In use: refused, and the error says by whom.
	mustBind(t, s, "chat", "llm", "gw")
	mustBind(t, s, "agent", "llm", "gw")
	mustBind(t, s, "agent", "backup_llm", "gw")
	err := s.DeleteService(ctx, "gw")
	var inUse *ServiceInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("in use: err = %v, want *ServiceInUseError", err)
	}
	if !errors.Is(err, ErrConflict) {
		t.Errorf("in use: %v does not match ErrConflict", err)
	}
	wantUsers := []ServiceUser{{"gw", "agent", "backup_llm"}, {"gw", "agent", "llm"}, {"gw", "chat", "llm"}}
	if !reflect.DeepEqual(inUse.Users, wantUsers) {
		t.Errorf("users = %+v, want %+v", inUse.Users, wantUsers)
	}
	if got := inUse.Apps(); !reflect.DeepEqual(got, []string{"agent", "chat"}) {
		t.Errorf("apps = %v", got)
	}
	if got := inUse.Services(); !reflect.DeepEqual(got, []string{"gw"}) {
		t.Errorf("services = %v", got)
	}
	for _, part := range []string{"gw", "agent", "chat"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("message %q does not name %s", err.Error(), part)
		}
	}
	if _, err := s.GetService(ctx, "gw"); err != nil {
		t.Fatalf("a refused delete removed the service: %v", err)
	}
	if bs, _ := s.ListServiceBindings(ctx, BindingFilter{Service: "gw"}); len(bs) != 3 {
		t.Fatalf("a refused delete removed bindings: %v", bindingKeys(bs))
	}

	// Once nobody uses it any more it goes.
	for _, app := range []string{"chat", "agent"} {
		if _, err := s.DeleteAppBindings(ctx, app); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteService(ctx, "gw"); err != nil {
		t.Fatalf("unused again: %v", err)
	}
}

func TestDeleteAppServices(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, appService("gw-smtp", "smtp", "gw", 1))
	mustCreateService(t, s, appService("other", "llm_gateway", "other", 0))
	// A user's service that happens to carry the app's name in its own name
	// is not the app's.
	mustCreateService(t, s, userService("gw-notes", "smtp"))

	// An app that provides nothing: nothing happens.
	if removed, err := s.DeleteAppServices(ctx, "nobody"); err != nil || removed != nil {
		t.Fatalf("app providing none: %v %v", removed, err)
	}

	// Another app uses one of the two: neither goes.
	mustBind(t, s, "chat", "mail", "gw-smtp")
	mustBind(t, s, "chat", "llm", "other")
	mustBind(t, s, "gw", "self_llm", "gw") // its own binding does not count
	removed, err := s.DeleteAppServices(ctx, "gw")
	var inUse *ServiceInUseError
	if !errors.As(err, &inUse) || removed != nil {
		t.Fatalf("in use: removed %v, err = %v", serviceNames(removed), err)
	}
	if want := []ServiceUser{{"gw-smtp", "chat", "mail"}}; !reflect.DeepEqual(inUse.Users, want) {
		t.Errorf("users = %+v, want %+v", inUse.Users, want)
	}
	if svs, _ := s.ListServices(ctx, ServiceFilter{App: "gw"}); len(svs) != 2 {
		t.Fatalf("a refused delete removed services: %v", serviceNames(svs))
	}
	if _, err := s.GetServiceBinding(ctx, "gw", "self_llm"); err != nil {
		t.Fatalf("a refused delete removed the app's own binding: %v", err)
	}

	// The other app lets go: both go, with the app's binding to itself.
	if ok, err := s.DeleteServiceBinding(ctx, "chat", "mail"); err != nil || !ok {
		t.Fatalf("unbind: %v %v", ok, err)
	}
	removed, err = s.DeleteAppServices(ctx, "gw")
	if err != nil || !reflect.DeepEqual(serviceNames(removed), []string{"gw", "gw-smtp"}) {
		t.Fatalf("removed %v (%v)", serviceNames(removed), err)
	}
	if removed[0].ValuesEnc != "v1:gw" || removed[1].ProvideIndex != 1 {
		t.Errorf("removed rows are not what was stored: %+v", removed)
	}
	left, _ := s.ListServices(ctx, ServiceFilter{})
	if !reflect.DeepEqual(serviceNames(left), []string{"gw-notes", "other"}) {
		t.Fatalf("left: %v", serviceNames(left))
	}
	bs, _ := s.ListServiceBindings(ctx, BindingFilter{})
	if !reflect.DeepEqual(bindingKeys(bs), []string{"chat/llm->other"}) {
		t.Fatalf("bindings left: %v", bindingKeys(bs))
	}
	// Again: nothing left to remove.
	if removed, err := s.DeleteAppServices(ctx, "gw"); err != nil || removed != nil {
		t.Fatalf("second time: %v %v", removed, err)
	}
}

func TestSetAndGetServiceBinding(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, userService("my-key", "llm_gateway"))

	made := mustBind(t, s, "chat", "llm", "gw")
	got, err := s.GetServiceBinding(ctx, "chat", "llm")
	if err != nil || !reflect.DeepEqual(made, got) {
		t.Fatalf("created %+v, read back %+v (%v)", made, got, err)
	}
	if got.Service != "gw" || got.IssuedEnc != "v1:chat/llm" || got.CreatedAt.IsZero() {
		t.Fatalf("round trip: %+v", got)
	}
	// The consuming app does not have to exist as a row of apps, and
	// nothing has to be issued.
	if _, err := s.SetServiceBinding(ctx, ServiceBinding{App: "ghost", UseName: "llm", Service: "my-key"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetServiceBinding(ctx, "ghost", "llm"); err != nil || got.IssuedEnc != "" {
		t.Fatalf("binding with nothing issued: %+v %v", got, err)
	}

	// New credentials for the same service: the binding keeps its date.
	if _, err := s.db.ExecContext(ctx, `UPDATE service_bindings SET created_at='2001-02-03T04:05:06.000000000Z' WHERE app='chat'`); err != nil {
		t.Fatal(err)
	}
	again, err := s.SetServiceBinding(ctx, ServiceBinding{App: "chat", UseName: "llm", Service: "gw", IssuedEnc: "v1:renewed"})
	if err != nil {
		t.Fatal(err)
	}
	if again.IssuedEnc != "v1:renewed" || again.CreatedAt.Year() != 2001 {
		t.Fatalf("renewed: %+v", again)
	}
	if read, err := s.GetServiceBinding(ctx, "chat", "llm"); err != nil || !reflect.DeepEqual(again, read) {
		t.Fatalf("renewed %+v, stored %+v (%v)", again, read, err)
	}
	// Moved to another service: one row, the new service, a new date.
	moved, err := s.SetServiceBinding(ctx, ServiceBinding{App: "chat", UseName: "llm", Service: "my-key", IssuedEnc: ""})
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.GetServiceBinding(ctx, "chat", "llm")
	if err != nil || !reflect.DeepEqual(moved, read) {
		t.Fatalf("moved %+v, stored %+v (%v)", moved, read, err)
	}
	if read.Service != "my-key" || read.IssuedEnc != "" || time.Since(read.CreatedAt) > time.Hour {
		t.Fatalf("moved: %+v", read)
	}
	if bs, _ := s.ListServiceBindings(ctx, BindingFilter{App: "chat"}); len(bs) != 1 {
		t.Fatalf("moving left %d rows", len(bs))
	}

	if _, err := s.GetServiceBinding(ctx, "chat", "mail"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := s.SetServiceBinding(ctx, ServiceBinding{App: "chat", UseName: "mail", Service: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing service: %v", err)
	}
	for name, b := range map[string]ServiceBinding{
		"bad app":      {App: "Chat", UseName: "llm", Service: "gw"},
		"no app":       {UseName: "llm", Service: "gw"},
		"bad need":     {App: "chat", UseName: "my-llm", Service: "gw"},
		"no need":      {App: "chat", Service: "gw"},
		"bad service":  {App: "chat", UseName: "llm", Service: "no such"},
		"empty target": {App: "chat", UseName: "llm"},
	} {
		if _, err := s.SetServiceBinding(ctx, b); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestListServiceBindings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if bs, err := s.ListServiceBindings(ctx, BindingFilter{}); err != nil || bs != nil {
		t.Fatalf("empty registry: %v %v", bs, err)
	}
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, userService("mail", "smtp"))
	mustBind(t, s, "chat", "llm", "gw")
	mustBind(t, s, "chat", "mail", "mail")
	mustBind(t, s, "agent", "llm", "gw")

	for name, c := range map[string]struct {
		f    BindingFilter
		want []string
	}{
		"all":                {BindingFilter{}, []string{"agent/llm->gw", "chat/llm->gw", "chat/mail->mail"}},
		"of an app":          {BindingFilter{App: "chat"}, []string{"chat/llm->gw", "chat/mail->mail"}},
		"to a service":       {BindingFilter{Service: "gw"}, []string{"agent/llm->gw", "chat/llm->gw"}},
		"app and service":    {BindingFilter{App: "chat", Service: "mail"}, []string{"chat/mail->mail"}},
		"app with none":      {BindingFilter{App: "gw"}, nil},
		"service with none":  {BindingFilter{Service: "nope"}, nil},
		"app not on service": {BindingFilter{App: "agent", Service: "mail"}, nil},
	} {
		bs, err := s.ListServiceBindings(ctx, c.f)
		if err != nil || !reflect.DeepEqual(bindingKeys(bs), c.want) {
			t.Errorf("%s: %v (%v), want %v", name, bindingKeys(bs), err, c.want)
		}
	}
}

func TestDeleteServiceBindings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, userService("mail", "smtp"))
	mustBind(t, s, "chat", "llm", "gw")
	mustBind(t, s, "chat", "mail", "mail")
	mustBind(t, s, "agent", "llm", "gw")

	if ok, err := s.DeleteServiceBinding(ctx, "chat", "nope"); err != nil || ok {
		t.Fatalf("missing need: %v %v", ok, err)
	}
	if ok, err := s.DeleteServiceBinding(ctx, "chat", "mail"); err != nil || !ok {
		t.Fatalf("one need: %v %v", ok, err)
	}
	if ok, err := s.DeleteServiceBinding(ctx, "chat", "mail"); err != nil || ok {
		t.Fatalf("the same need again: %v %v", ok, err)
	}
	mustBind(t, s, "chat", "mail", "mail")

	if n, err := s.DeleteAppBindings(ctx, "nobody"); err != nil || n != 0 {
		t.Fatalf("app with none: %d %v", n, err)
	}
	if n, err := s.DeleteAppBindings(ctx, "chat"); err != nil || n != 2 {
		t.Fatalf("app with two: %d %v", n, err)
	}
	bs, _ := s.ListServiceBindings(ctx, BindingFilter{})
	if !reflect.DeepEqual(bindingKeys(bs), []string{"agent/llm->gw"}) {
		t.Fatalf("left: %v", bindingKeys(bs))
	}
	// The services themselves are untouched.
	if svs, _ := s.ListServices(ctx, ServiceFilter{}); len(svs) != 2 {
		t.Fatalf("services: %v", serviceNames(svs))
	}
}

// A binding moved aside keeps what it holds and when it was made, comes back
// the same way, and a parked name is never a need's.
func TestMoveServiceBinding(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustCreateService(t, s, userService("other", "llm_gateway"))
	orig := mustBind(t, s, "chat", "llm", "gw")
	parked := ParkedUseName("llm")
	if ValidUseName(parked) {
		t.Fatalf("a parked name passes for a need's: %s", parked)
	}
	if base, ok := ParkedUse(parked); !ok || base != "llm" {
		t.Fatalf("ParkedUse(%s) = %s %v", parked, base, ok)
	}
	if _, ok := ParkedUse("llm"); ok {
		t.Fatal("a need's name taken for a parked one")
	}
	if err := s.MoveServiceBinding(ctx, "chat", "llm", parked); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetServiceBinding(ctx, "chat", "llm"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the need still plugged in: %v", err)
	}
	got, err := s.GetServiceBinding(ctx, "chat", parked)
	if err != nil || got.Service != "gw" || got.IssuedEnc != orig.IssuedEnc || !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Fatalf("parked %+v %v", got, err)
	}
	// The need plugged into something else meanwhile: moving back is refused.
	mustBind(t, s, "chat", "llm", "other")
	if err := s.MoveServiceBinding(ctx, "chat", parked, "llm"); !errors.Is(err, ErrConflict) {
		t.Fatalf("move onto a taken need: %v", err)
	}
	if _, err := s.DeleteServiceBinding(ctx, "chat", "llm"); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveServiceBinding(ctx, "chat", parked, "llm"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetServiceBinding(ctx, "chat", "llm"); err != nil || got.Service != "gw" || got.IssuedEnc != orig.IssuedEnc {
		t.Fatalf("back %+v %v", got, err)
	}
	if err := s.MoveServiceBinding(ctx, "chat", parked, "llm"); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrConflict) {
		t.Fatalf("nothing parked: %v", err)
	}
	if err := s.MoveServiceBinding(ctx, "chat", "mail", ParkedUseName("mail")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nothing to move: %v", err)
	}
	for _, bad := range [][2]string{{"llm", "llm"}, {"llm", "Bad~name"}, {"llm", "llm~other"}} {
		if err := s.MoveServiceBinding(ctx, "chat", bad[0], bad[1]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("move %v: %v", bad, err)
		}
	}
	// A parked binding still holds on to its service.
	if err := s.MoveServiceBinding(ctx, "chat", "llm", parked); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteService(ctx, "gw"); err == nil {
		t.Fatal("a service with a parked binding deleted")
	}
}

// Deleting an app leaves the registry alone: the caller decides when the
// app's services and bindings go.
func TestDeleteAppLeavesRegistry(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for _, app := range []string{"gw", "chat"} {
		if _, _, err := s.EnsureApp(ctx, app); err != nil {
			t.Fatal(err)
		}
	}
	mustCreateService(t, s, appService("gw", "llm_gateway", "gw", 0))
	mustBind(t, s, "chat", "llm", "gw")
	for _, app := range []string{"gw", "chat"} {
		if err := s.DeleteApp(ctx, app, false); err != nil {
			t.Fatalf("delete %s: %v", app, err)
		}
	}
	if _, err := s.GetService(ctx, "gw"); err != nil {
		t.Fatalf("service went with its app: %v", err)
	}
	if _, err := s.GetServiceBinding(ctx, "chat", "llm"); err != nil {
		t.Fatalf("binding went with its app: %v", err)
	}
}

func TestServiceNameForms(t *testing.T) {
	for _, ok := range []string{
		"a", "gw", "new-api", "new-api-llm_gateway", "new-api-2", "9lives",
		// What a user calls a service, in their own words.
		"我的自建邮箱", "我的上游", "邮箱", "公司邮箱-2", "My-Mail", "A", "mail.work", "OpenAI_上游",
		"メール", "почта", "café", "cafe\u0301", // a combining mark after a letter
		// Trailing punctuation is fine: a default name may end in the "_" of a kind.
		"app-foo_", "a-", "a.",
		strings.Repeat("a", ServiceNameMaxLen), strings.Repeat("邮", ServiceNameMaxLen),
	} {
		if !ValidServiceName(ok) {
			t.Errorf("ValidServiceName(%q) = false", ok)
		}
	}
	for _, bad := range []string{
		"",
		"-a", "_a", ".a", ".", "..", "\u0301a", // must start with a letter or digit
		"a b", "office mail", " a", "a ", "a\tb", "a\nb", "我的\u3000邮箱", "a\u00a0b", // blanks of any kind
		"a/b", "a\\b", "a=b", "a,b", "a:b", "a?b", "a#b", "a%b", "a&b", "a+b", "a@b", "a*b", "a'b", `a"b`, "a$b", "a;b", "a|b", "a<b",
		"邮箱（公司）", "邮箱，备用", // punctuation of any script
		"a\x00b", "a\x7fb", "a\u200bb", "a\u202eb", "a\ufeffb", // control and invisible characters
		"a\xffb", "\xe9\x82", "a\ufffdb", // not valid UTF-8, and what it decodes to
		"a😀",    // symbols
		"kinds", // reserved
		strings.Repeat("a", ServiceNameMaxLen+1), strings.Repeat("邮", ServiceNameMaxLen+1),
	} {
		if ValidServiceName(bad) {
			t.Errorf("ValidServiceName(%q) = true", bad)
		}
	}
	// Reserved words are refused as they are spelled; nothing else is
	// reserved.
	if !ReservedServiceName("kinds") || ReservedServiceName("Kinds") || ReservedServiceName("kind") || ReservedServiceName("") {
		t.Error("ReservedServiceName")
	}
	if !ValidServiceName("Kinds") || !ValidServiceName("kinds-2") {
		t.Error("a name that only resembles a reserved word is refused")
	}
	for _, ok := range []string{"smtp", "llm_gateway", "s3", "foo_"} {
		if !ValidServiceKind(ok) || !ValidUseName(ok) {
			t.Errorf("kind / need %q refused", ok)
		}
	}
	for _, bad := range []string{"", "3s", "_x", "llm-gateway", "Smtp", "邮箱"} {
		if ValidServiceKind(bad) || ValidUseName(bad) {
			t.Errorf("kind / need %q accepted", bad)
		}
	}
	// Any app name, alone or followed by a kind and a number, is a valid
	// service name, whatever the kind ends in.
	longest := "a" + strings.Repeat("b", 39)
	for _, kind := range []string{"k" + strings.Repeat("a", 63), "k" + strings.Repeat("_", 63), "foo_"} {
		if !ValidAppName(longest) || !ValidServiceKind(kind) {
			t.Fatalf("test data: %q %q", longest, kind)
		}
		for _, name := range []string{longest, longest + "-" + kind, longest + "-" + kind + "-99"} {
			if !ValidServiceName(name) {
				t.Errorf("default name %q is not a valid service name", name)
			}
		}
	}
}
