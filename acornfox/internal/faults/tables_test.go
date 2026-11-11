package faults_test

// The packages that raise codes keep short tables that put a hint on each
// error. They and the catalog must say the same thing word for word, so a
// user sees the same next step in an error, in `acornfox faults <code>`, in
// docs/faults.md and on the console's runtime page.

import (
	"testing"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/setup"
)

func find(t *testing.T, code string) faults.Fault {
	t.Helper()
	f, ok := faults.Find(code)
	if !ok || f.Code != code {
		t.Fatalf("%s 不在故障目录里：在 internal/faults 补上条目", code)
	}
	return f
}

// appinstall.Faults: Meaning is the entry's Explain, Action its Next.
func TestAppinstallTableMatches(t *testing.T) {
	for _, d := range appinstall.Faults {
		f := find(t, d.Code)
		if f.Explain != d.Meaning {
			t.Errorf("%s：appinstall 的 Meaning 和目录的 Explain 不一样\n  appinstall: %s\n  faults:     %s", d.Code, d.Meaning, f.Explain)
		}
		if f.Next != d.Action {
			t.Errorf("%s：appinstall 的 Action 和目录的 Next 不一样\n  appinstall: %s\n  faults:     %s", d.Code, d.Action, f.Next)
		}
	}
}

// setup.Faults: title, cause texts, next step and needs_user are the same.
func TestSetupTableMatches(t *testing.T) {
	for _, d := range setup.Faults {
		f := find(t, d.Code)
		if f.Title != d.Title {
			t.Errorf("%s：标题不一样\n  setup:  %s\n  faults: %s", d.Code, d.Title, f.Title)
		}
		if f.Next != d.Next {
			t.Errorf("%s：下一步不一样\n  setup:  %s\n  faults: %s", d.Code, d.Next, f.Next)
		}
		if f.NeedsUser != d.NeedsUser {
			t.Errorf("%s：needs_user 不一样", d.Code)
		}
		if len(f.Causes) != len(d.Causes) {
			t.Errorf("%s：原因条数不一样（setup %d，目录 %d）", d.Code, len(d.Causes), len(f.Causes))
			continue
		}
		for i, c := range d.Causes {
			if f.Causes[i].Text != c {
				t.Errorf("%s：第 %d 个原因不一样\n  setup:  %s\n  faults: %s", d.Code, i+1, c, f.Causes[i].Text)
			}
		}
	}
}

// gatewayclient.Codes: Meaning is the entry's Explain, Client (what this
// client does) its Auto.
func TestGatewayClientTableMatches(t *testing.T) {
	for _, d := range gatewayclient.Codes {
		f := find(t, d.Code)
		if f.Explain != d.Meaning {
			t.Errorf("%s：Meaning 和 Explain 不一样\n  gatewayclient: %s\n  faults:        %s", d.Code, d.Meaning, f.Explain)
		}
		if f.Auto != d.Client {
			t.Errorf("%s：Client 和 Auto 不一样\n  gatewayclient: %s\n  faults:        %s", d.Code, d.Client, f.Auto)
		}
	}
}

// The setup entries the console shows next to doctor's checks keep their
// short-shape fields through SetupDefs.
func TestSetupDefsKeepSetupTable(t *testing.T) {
	defs := map[string]faults.SetupDef{}
	for _, d := range faults.SetupDefs() {
		defs[d.Code] = d
	}
	for _, d := range setup.Faults {
		got := defs[d.Code]
		if got.Title != d.Title || got.Next != d.Next || got.NeedsUser != d.NeedsUser || len(got.Causes) != len(d.Causes) {
			t.Errorf("%s：GET /v1/setup/faults 的内容会变", d.Code)
		}
	}
	if setup.MacLocalCode != "SETUP-MAC-01" {
		t.Fatal("Mac 本机编号变了")
	}
}
