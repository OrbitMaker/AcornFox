package platformvar

import (
	"context"
	"fmt"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
)

// cloudFake stands in for the cloud edition's provider, only in the tests:
// its values come from the user's account and the cluster (here fixed
// fields), the app's address is the one the platform assigns. It shows the
// interface can be filled without anything of this machine.
type cloudFake struct {
	accountTZ string // the user's account time zone
	cluster   string // "cn" or "global"
	pypi, npm string // the cluster's internal mirrors
}

var _ Provider = (*cloudFake)(nil)

func newCloudFake() *cloudFake {
	return &cloudFake{accountTZ: "Asia/Shanghai", cluster: "cn",
		pypi: "http://pypi.cluster.internal/simple/", npm: "http://npm.cluster.internal/"}
}

func (c *cloudFake) Edition() string              { return catalog.EditionCloud }
func (c *cloudFake) Table() []catalog.PlatformVar { return Table() }

func (c *cloudFake) base() map[string]string {
	return map[string]string{
		catalog.PVTZ: c.accountTZ, catalog.PVRegion: c.cluster,
		catalog.PVMirrorPyPI: c.pypi, catalog.PVMirrorNPM: c.npm,
	}
}

func (c *cloudFake) Machine(context.Context) []State {
	base := c.base()
	var out []State
	for _, pv := range Table() {
		st := State{PlatformVar: pv}
		if v, ok := base[pv.Name]; ok {
			st.Value, st.Available = v, true
		} else if perApp(pv.Name) {
			st.Available, st.Note = true, NotePerApp
		} else {
			st.Note = NoteNoValue
		}
		out = append(out, st)
	}
	return out
}

func (c *cloudFake) Values(_ context.Context, app App) (map[string]string, error) {
	if app.Created && app.Name == "" {
		return nil, fmt.Errorf("an app without a name")
	}
	out := c.base()
	if app.Created {
		withAddress(out, c.Address(app))
	}
	return withSets(out, app.Sets), nil
}

// Address: the platform assigns it; where the server would open the app
// (app.Where) means nothing on a cluster.
func (c *cloudFake) Address(app App) Address {
	if app.Internal || app.Name == "" {
		return Address{}
	}
	host := strings.ToLower(app.Name) + ".app.acornfox.com"
	return Address{URL: "https://" + host, Scheme: "https", Host: host}
}

func (c *cloudFake) Has(name string) bool {
	_, ok := c.base()[name]
	return ok || perApp(name)
}

func (c *cloudFake) Check(sets map[string]string) error { return CheckSets(Table(), sets) }
