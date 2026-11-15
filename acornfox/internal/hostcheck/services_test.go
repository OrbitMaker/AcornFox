package hostcheck

import (
	"strings"
	"testing"
)

func TestCheckAppServices(t *testing.T) {
	installed := func(app string) bool { return app != "gone-consumer" }
	stopped := ServiceInUse{Name: "gw", Kind: "llm_gateway", KindLabel: "大模型接口", Status: ServiceStatusProviderStopped, StatusLabel: "提供它的应用停着", Provider: "gw",
		Users: []ServiceUser{{App: "agent", Use: "llm"}, {App: "gone-consumer", Use: "llm"}}}
	gone := ServiceInUse{Name: "gw2", Kind: "llm_gateway", KindLabel: "大模型接口", Status: ServiceStatusProviderGone, StatusLabel: "提供它的应用已删", Provider: "gw2",
		Users: []ServiceUser{{App: "agent2", Use: "llm"}}}
	incomplete := ServiceInUse{Name: "mail", Kind: "smtp", KindLabel: "邮箱（发信）", Status: ServiceStatusIncomplete, StatusLabel: "还差必填项", Missing: []string{"host", "password"},
		Users: []ServiceUser{{App: "blog", Use: "mail"}}}
	ok := ServiceInUse{Name: "up", Kind: "llm_gateway", Status: ServiceStatusOK, StatusLabel: "可用", Users: []ServiceUser{{App: "chat", Use: "llm"}}}
	unused := ServiceInUse{Name: "spare", Kind: "smtp", Status: ServiceStatusIncomplete, StatusLabel: "还差必填项", Missing: []string{"host"}}
	onlyGoneUser := ServiceInUse{Name: "old", Kind: "smtp", Status: ServiceStatusProviderStopped, Provider: "p", Users: []ServiceUser{{App: "gone-consumer", Use: "mail"}}}

	cases := []struct {
		name       string
		svcs       []ServiceInUse
		unreadable []string
		levels     []string
		want       [][]string
		not        []string
	}{
		{"nobody uses a service", []ServiceInUse{unused}, nil, []string{"ok"}, [][]string{{"没有应用接着服务"}}, nil},
		{"all usable", []ServiceInUse{ok, unused}, nil, []string{"ok"}, [][]string{{"1 个应用接着的服务都可用"}}, nil},
		{"provider stopped", []ServiceInUse{ok, stopped}, nil, []string{"warn"},
			[][]string{{"应用 agent 的需要 llm 接的服务 gw（大模型接口）", "“提供它的应用停着”", "acornfox start gw", "问用户"}}, []string{"gone-consumer"}},
		{"provider gone", []ServiceInUse{gone}, nil, []string{"warn"},
			[][]string{{"应用 agent2 的需要 llm", "“提供它的应用已删”", "acornfox install --name gw2 --reuse-data", "保留的记录", "--plan", "--use"}}, nil},
		{"items missing", []ServiceInUse{incomplete}, nil, []string{"warn"},
			[][]string{{"应用 blog 的需要 mail 接的服务 mail（邮箱（发信））", "还差 host、password", "acornfox services set mail", "项=-"}}, nil},
		{"only deleted apps use it", []ServiceInUse{onlyGoneUser}, nil, []string{"ok"}, [][]string{{"没有应用接着服务"}}, nil},
		{"unreadable", []ServiceInUse{ok}, []string{"secret-one"}, []string{"warn"}, [][]string{{"secret-one 存的值解不开"}}, nil},
		{"two problems", []ServiceInUse{stopped, incomplete}, nil, []string{"warn", "warn"}, [][]string{{"gw"}, {"mail"}}, nil},
	}
	for _, c := range cases {
		r := &recorder{}
		CheckAppServices(r, c.svcs, c.unreadable, installed)
		if len(r.lines) != len(c.levels) {
			t.Fatalf("%s: %d lines %+v", c.name, len(r.lines), r.lines)
		}
		for i, l := range r.lines {
			if l.level != c.levels[i] || l.code != CodeServiceUnusable || !l.ok {
				t.Errorf("%s: line %d %+v", c.name, i, l)
			}
			for _, w := range c.want[i] {
				if !strings.Contains(l.text, w) {
					t.Errorf("%s: %q lacks %q", c.name, l.text, w)
				}
			}
			for _, n := range c.not {
				if strings.Contains(l.text, n) {
					t.Errorf("%s: %q must not name %q", c.name, l.text, n)
				}
			}
		}
	}
}

// A service whose providing app's readiness check does not pass (not_ready)
// gets a warning of its own, CAT-SVC-09, with the provider's sentence,
// whether an app uses it or not; the other services are judged as before.
func TestCheckAppServicesNotReady(t *testing.T) {
	installed := func(app string) bool { return app != "gone-consumer" }
	notReady := ServiceInUse{Name: "db", Kind: "demo_sql", KindLabel: "样例数据库", Status: ServiceStatusNotReady, StatusLabel: "还不能用", Provider: "db",
		ReadySay: "还没建好管理账号：重装一次这个应用", Users: []ServiceUser{{App: "web", Use: "db"}, {App: "gone-consumer", Use: "db"}}}
	unused := notReady
	unused.Name, unused.Users = "db2", nil
	ok := ServiceInUse{Name: "up", Kind: "llm_gateway", Status: ServiceStatusOK, StatusLabel: "可用", Users: []ServiceUser{{App: "chat", Use: "llm"}}}

	r := &recorder{}
	CheckAppServices(r, []ServiceInUse{ok, notReady, unused}, nil, installed)
	if len(r.lines) != 2 {
		t.Fatalf("%d lines %+v", len(r.lines), r.lines)
	}
	for i, want := range [][]string{
		{"服务 db（样例数据库，由应用 db 提供）还不能用：还没建好管理账号：重装一次这个应用", "应用 web 的需要 db 接着它", "acornfox services show db"},
		{"服务 db2", "还没有应用接着它"},
	} {
		l := r.lines[i]
		if l.level != "warn" || l.code != CodeServiceNotReady || !l.ok {
			t.Errorf("line %d %+v", i, l)
		}
		for _, w := range want {
			if !strings.Contains(l.text, w) {
				t.Errorf("%q lacks %q", l.text, w)
			}
		}
		if strings.Contains(l.text, "gone-consumer") {
			t.Errorf("%q names an app deleted with its data kept", l.text)
		}
	}
	// Without the provider's sentence it still says what is wrong.
	r = &recorder{}
	bare := notReady
	bare.ReadySay = ""
	CheckAppServices(r, []ServiceInUse{bare}, nil, installed)
	if len(r.lines) != 1 || r.lines[0].code != CodeServiceNotReady || !strings.Contains(r.lines[0].text, "就绪检查没通过") {
		t.Fatalf("%+v", r.lines)
	}
}
