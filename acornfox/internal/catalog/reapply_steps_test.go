package catalog

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The three moments an init step runs at (when: after_healthy, on_reapply,
// on_change with watch) and how lint and the minimum platform version treat
// them.

// withInit makes the services test catalog's consumer declare 0.7.0 and
// write the init steps given (a YAML list, indented as under "init:").
func withInit(t *testing.T, dir, steps string, mutate ...[2]string) {
	t.Helper()
	f := filepath.Join(dir, "apps", "demo-consumer", manifestName)
	edit(t, f, "\ndelivery:\n", "\ninit:\n"+steps+"\ndelivery:\n")
	edit(t, f, `min_platform: "0.6.0"`, `min_platform: "0.7.0"`)
	for _, m := range mutate {
		edit(t, f, m[0], m[1])
	}
}

const goodInit = `  - name: seed
    when: after_healthy
    exec: { command: [demo, seed] }
  - name: sync
    when: on_reapply
    exec: { command: [demo, sync, "{{uses.demo.url}}"] }
  - name: url
    when: on_change
    watch: [uses.demo.url, platform.tz]
    exec: { command: [demo, set-url, "{{uses.demo.url}}"] }
  - name: tz
    when: on_change
    watch: [platform.tz]
    exec: { command: [demo, set-tz, "{{platform.tz}}"] }
`

func TestInitStepsOfThreeMoments(t *testing.T) {
	dir := copyServices(t)
	withInit(t, dir, goodInit)
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Errors() != 0 {
		t.Fatalf("a valid manifest has errors: %+v", r.Problems)
	}
	mp := r.MinPlatform.Apps["demo-consumer"]
	if mp.Computed != "0.7.0" || mp.Declared != "0.7.0" {
		t.Fatalf("min platform %+v", mp)
	}
	// the groups: one per moment, and one per distinct watch list
	m, err := ParseManifest(readServicesManifestAt(t, dir, "demo-consumer"))
	if err != nil {
		t.Fatal(err)
	}
	gs := InitGroups(m.Init)
	want := []InitGroup{
		{When: StepAfterHealthy, Index: []int{0}},
		{When: StepOnReapply, Index: []int{1}},
		{When: StepOnChange, Watch: []string{"platform.tz", "uses.demo.url"}, Index: []int{2}},
		{When: StepOnChange, Watch: []string{"platform.tz"}, Index: []int{3}},
	}
	if !reflect.DeepEqual(gs, want) {
		t.Fatalf("groups %+v, want %+v", gs, want)
	}
	// the same watch list written in another order is one group
	m.Init = append(m.Init, Step{Name: "again", When: StepOnChange, Watch: []string{"uses.demo.url", "platform.tz"}})
	if gs := InitGroups(m.Init); len(gs) != 4 || len(gs[2].Index) != 2 {
		t.Fatalf("groups %+v", gs)
	}
	// a manifest that wrote nothing is after_healthy
	if (Step{}).Trigger() != StepAfterHealthy {
		t.Fatal("default moment")
	}
}

func readServicesManifestAt(t *testing.T, dir, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "apps", id, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Each way of getting it wrong is one code.
func TestInitStepsProblems(t *testing.T) {
	cases := []struct {
		name  string
		steps string
		mut   [][2]string
		want  string
	}{
		{"on_change without watch", "  - name: x\n    when: on_change\n    exec: { command: [demo, x] }\n", nil, CodeSchema},
		{"watch on a step of another moment", "  - name: x\n    when: on_reapply\n    watch: [platform.tz]\n    exec: { command: [demo, x] }\n", nil, CodeSchema},
		{"watch that is no reference", "  - name: x\n    when: on_change\n    watch: [saved.token]\n    exec: { command: [demo, x] }\n", nil, CodeSchema},
		{"watch that names nothing", "  - name: x\n    when: on_change\n    watch: []\n    exec: { command: [demo, x] }\n", nil, CodeSchema},
		{"watch twice", "  - name: x\n    when: on_change\n    watch: [platform.tz, platform.tz]\n    exec: { command: [demo, x] }\n", nil, CodeSchema},
		{"a parameter that is not there", "  - name: x\n    when: on_change\n    watch: [params.nope]\n    exec: { command: [demo, x] }\n", nil, CodeReference},
		{"a platform variable that is not there", "  - name: x\n    when: on_change\n    watch: [platform.nope]\n    exec: { command: [demo, x] }\n", nil, CodePlatformVar},
		{"a service that is not there", "  - name: x\n    when: on_change\n    watch: [uses.nope.url]\n    exec: { command: [demo, x] }\n", nil, CodeReference},
		{"an item the service has not", "  - name: x\n    when: on_change\n    watch: [uses.demo.nothing]\n    exec: { command: [demo, x] }\n", nil, CodeServiceFields},
		{"a parameter nobody can change", "  - name: x\n    when: on_change\n    watch: [params.key]\n    exec: { command: [demo, x] }\n",
			[][2]string{{"\nuses:\n", "\nparams:\n  - { key: key, type: secret, hidden: true, generate: { length: 24 } }\n\nuses:\n"}}, CodeWatch},
		{"saved of another moment", "  - name: login\n    when: after_healthy\n    http: { method: POST, path: /login, save: { token: data.token } }\n  - name: use\n    when: on_reapply\n    http: { method: POST, path: /x, headers: { Authorization: \"{{saved.token}}\" } }\n", nil, CodeStepRef},
		{"saved of a group with another watch list", "  - name: login\n    when: on_change\n    watch: [platform.tz]\n    http: { method: POST, path: /login, save: { token: data.token } }\n  - name: use\n    when: on_change\n    watch: [uses.demo.url]\n    http: { method: POST, path: /x, headers: { Authorization: \"{{saved.token}}\" } }\n", nil, CodeStepRef},
		{"a lower minimum platform than it needs", "  - name: x\n    when: on_reapply\n    exec: { command: [demo, x] }\n", [][2]string{{`min_platform: "0.7.0"`, `min_platform: "0.6.3"`}}, CodeMinPlatform},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyServices(t)
			withInit(t, dir, c.steps, c.mut...)
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(problemsWith(r, c.want)) == 0 {
				t.Fatalf("没报 %s：%+v", c.want, r.Problems)
			}
			if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
				t.Fatalf("有错误时不能打包：%v", err)
			}
		})
	}
}

// Steps of one group may use what an earlier step of the group saved; the
// paths of problems keep the step's place in the whole list.
func TestInitGroupsSavedAndPaths(t *testing.T) {
	dir := copyServices(t)
	withInit(t, dir, "  - name: a\n    when: after_healthy\n    exec: { command: [demo, a] }\n"+
		"  - name: login\n    when: on_reapply\n    http: { method: POST, path: /login, save: { token: data.token } }\n"+
		"  - name: b\n    when: after_healthy\n    exec: { command: [demo, \"{{saved.nothing}}\"] }\n"+
		"  - name: use\n    when: on_reapply\n    http: { method: POST, path: /x, headers: { Authorization: \"{{saved.token}}\" } }\n")
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	hits := problemsWith(r, CodeStepRef)
	if len(hits) != 1 || !strings.HasPrefix(hits[0].Path, "/init/b") {
		t.Fatalf("problems %+v", r.Problems)
	}
}
