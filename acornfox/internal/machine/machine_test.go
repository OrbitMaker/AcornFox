package machine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManualOf(t *testing.T) {
	type withSkill struct {
		Name  string `json:"name"`
		Skill *struct {
			Path    string `json:"path"`
			Version string `json:"version"`
		} `json:"skill,omitempty"`
	}
	has := withSkill{Name: "Memos"}
	has.Skill = &struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	}{"skills/app-memos.md", "0.31.0"}
	if m := ManualOf(has, "app"); !m.Has || m.Kind != "app" || m.Version != "0.31.0" {
		t.Fatalf("有 skill 字段：%+v", m)
	}
	if m := ManualOf(withSkill{Name: "x"}, "app"); m.Has || m.Kind != "" || m.Version != "" {
		t.Fatalf("没有 skill 字段：%+v", m)
	}
	if m := ManualOf(nil, "app"); m.Has {
		t.Fatalf("nil：%+v", m)
	}
	// A pointer to a struct that has no such field (today's show), and a nil pointer.
	type plain struct{ Name string }
	if m := ManualOf(&plain{"x"}, "solution"); m.Has {
		t.Fatalf("没有这个字段的类型：%+v", m)
	}
	var np *withSkill
	if m := ManualOf(np, "app"); m.Has {
		t.Fatalf("nil 指针：%+v", m)
	}
	if m := ManualOf(&has, "solution"); !m.Has || m.Kind != "solution" {
		t.Fatalf("指针：%+v", m)
	}
}

func TestStateOf(t *testing.T) {
	for _, c := range []struct{ desired, observed, want string }{
		{"running", "running", "running"},
		{"running", "stopped", "stopped"},
		{"running", "missing", "not_running"},
		{"running", "unavailable", "unknown"},
		{"stopped", "running", "stopped"},
		{"stopped", "missing", "stopped"},
		{"", "", "unknown"},
	} {
		if got := StateOf(c.desired, c.observed); got != c.want {
			t.Errorf("StateOf(%q, %q) = %q, want %q", c.desired, c.observed, got, c.want)
		}
	}
}

func TestNormalizeNoNullsAndFixedOrder(t *testing.T) {
	v := View{}
	Normalize(&v)
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "null") {
		t.Fatalf("空的视图里不能有 null：%s", b)
	}
	v = View{
		Apps:      []App{{Instance: "b", Provides: []string{"z", "a"}}, {Instance: "a"}},
		Solutions: []Solution{{ID: "s2", Apps: []string{"y", "x"}}, {ID: "s1"}},
		Services:  []Service{{Service: "two", UsedBy: []string{"q", "p"}}, {Service: "one"}},
	}
	Normalize(&v)
	if v.Apps[0].Instance != "a" || v.Apps[1].Provides[0] != "a" || v.Solutions[0].ID != "s1" || v.Solutions[1].Apps[0] != "x" ||
		v.Services[0].Service != "one" || v.Services[1].UsedBy[0] != "p" {
		t.Fatalf("顺序没排：%+v", v)
	}
	b, _ = json.Marshal(v)
	if strings.Contains(string(b), "null") {
		t.Fatalf("有 null：%s", b)
	}
}

func sample() View {
	v := View{
		GeneratedAt: "2026-11-24T10:00:00Z",
		Machine:     Machine{Name: "n7", Version: "0.6.4", Region: "cn"},
		Apps: []App{
			{ID: "memos", Instance: "memos", Name: "Memos", Tagline: "随手记笔记", Source: "catalog", State: "running", URL: "http://192.168.1.5:18810", Exposure: "lan", Version: "0.31.0",
				Manual: Manual{Has: true, Kind: "app", Version: "0.31.0"}},
			{ID: "postgres16", Instance: "postgres16", Name: "PostgreSQL 16", Source: "catalog", State: "running", Exposure: "internal", Provides: []string{"postgres"}},
			{ID: "my-site", Instance: "my-site", Name: "my-site", Source: "deploy", State: "stopped", URL: "http://127.0.0.1:18811", Exposure: "local"},
		},
		Solutions: []Solution{{ID: "n8n-with-postgres", Name: "搭一个自己的自动化工作台", Apps: []string{"n8n", "postgres16"}}},
		Services: []Service{
			{Type: "postgres", Name: "PostgreSQL 数据库", Service: "postgres16", Provider: "postgres16", Status: "ok", UsedBy: []string{"n8n"}},
			{Type: "llm_gateway", Name: "大模型接口", Service: "我的上游", Status: "ok", RegisteredByUser: true},
		},
		Platform: Platform{Backup: true, Upgrade: true},
		Warnings: []Warning{{Code: CodeIncomplete, Part: "manuals", Message: "应用目录读不了"}},
	}
	Normalize(&v)
	return v
}

func TestMarkdownPart(t *testing.T) {
	md := Markdown(sample(), "家里的服务器")
	for _, want := range []string{
		"<!-- 生成时间：2026-11-24T10:00:00Z；服务器：家里的服务器；AcornFox 0.6.4；",
		"# 服务器 家里的服务器 现在能做什么", "acornfox --target 家里的服务器 --json machine",
		"**Memos**（编号 memos）：在运行", "http://192.168.1.5:18810", "有手册（对 0.31.0 版验过）：acornfox --json catalog skill memos",
		"没有页面，只有别的应用从内部网络用它", "自己部署的项目", "提供的服务：postgres",
		"已装的解决方案（1 个）", "搭一个自己的自动化工作台", "n8n、postgres16",
		"用户自己登记", "应用 postgres16 提供", "在用它的：n8n",
		"备份和恢复应用数据：能", "改了设置让应用跟上", "这个版本没有", "注意（MACHINE-01）",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("分册缺 %q:\n%s", want, md)
		}
	}
	if !strings.HasPrefix(md, "<!-- 生成时间：") {
		t.Errorf("第一行要是生成时间的头：%.60s", md)
	}
	if h := Text(sample()); !strings.Contains(h, "n7") || !strings.Contains(h, "memos") || !strings.Contains(h, "有手册") {
		t.Errorf("短文字：%s", h)
	}
}

func TestCacheFileNameStaysInTheDirectory(t *testing.T) {
	for in, want := range map[string]string{
		"n7":            "machine-n7.md",
		"家里的服务器":        "machine-家里的服务器.md",
		"../../etc/x":   "machine-etc_x.md",
		"a b/c\\d":      "machine-a_b_c_d.md",
		"":              "machine-server.md",
		"..":            "machine-server.md",
		"my.server-1_a": "machine-my.server-1_a.md",
	} {
		got := CacheFileName(in)
		if got != want {
			t.Errorf("CacheFileName(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("%q 里有路径分隔符", got)
		}
	}
}

// The containers AcornFox does not manage: only a name, running or not and
// the ports; none when there are none.
func TestOtherContainersInTheView(t *testing.T) {
	v := View{OtherContainers: []OtherContainer{{Name: "redis", Running: true, Ports: []int{6379, 80}}, {Name: "old-db"}}}
	Normalize(&v)
	if v.OtherContainers[0].Name != "old-db" || v.OtherContainers[0].Ports == nil || v.OtherContainers[1].Ports[0] != 80 {
		t.Fatalf("顺序和空列表：%+v", v.OtherContainers)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "null") {
		t.Fatalf("有 null：%s", b)
	}
	md := Markdown(v, "n7")
	for _, want := range []string{"不归 AcornFox 管的容器（2 个）", "只能看，不能管", "redis：在跑，端口 80、6379", "old-db：停了", "acornfox --json other-containers"} {
		if !strings.Contains(md, want) {
			t.Errorf("分册缺 %q:\n%s", want, md)
		}
	}
	if h := Text(v); !strings.Contains(h, "不归 AcornFox 管的容器 2 个（只看）") || !strings.Contains(h, "redis：在跑") {
		t.Errorf("短文字：%s", h)
	}
	// None: the section is not there at all.
	none := View{}
	Normalize(&none)
	if strings.Contains(Markdown(none, "n7"), "不归 AcornFox 管") || strings.Contains(Text(none), "不归 AcornFox 管") {
		t.Error("没有其他容器时不写这一节")
	}
}
