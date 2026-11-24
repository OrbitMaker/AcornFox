package appbackup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/state"
)

// Records is records.json: what the platform knew about the app when the
// backup was taken, apart from the files in its volumes. It holds secret
// values (variables, encrypted install parameters, add-on passwords), so the
// file is 0600 in a 0700 directory.
//
// A restore puts back what belongs to the app's own installation: the
// variables, the saved install parameters, the manifest it was installed
// with, the add-on records, the settings that follow the version. The service
// registry is only read (Provides, Uses, UsedBy) to tell afterwards what no
// longer matches; it is never rewritten, because the accounts and
// connections in it are shared with other apps.
type Records struct {
	Format  int       `json:"format"`
	TakenAt time.Time `json:"taken_at"`

	App             state.App              `json:"app"`
	Env             []state.EnvVar         `json:"env"`
	Volumes         []state.Volume         `json:"volumes"`
	Addons          []AddonRecord          `json:"addons,omitempty"`
	CatalogSecrets  *SecretsRecord         `json:"catalog_secrets,omitempty"`
	CatalogManifest *state.CatalogManifest `json:"catalog_manifest,omitempty"`

	// Provides are the services this app provides, Uses its own plugs into
	// other apps' services, UsedBy the plugs of other apps into this app's
	// services. Ciphertext as the store keeps it.
	Provides []ServiceRecord `json:"provides,omitempty"`
	Uses     []BindingRecord `json:"uses,omitempty"`
	UsedBy   []BindingRecord `json:"used_by,omitempty"`
}

// AddonRecord is one add-on of the app (credentials in clear, as the store
// keeps them).
type AddonRecord struct {
	Kind        string          `json:"kind"`
	Image       string          `json:"image"`
	VolumeName  string          `json:"volume_name"`
	EnvVar      string          `json:"env_var"`
	Credentials json.RawMessage `json:"credentials"`
	CreatedAt   time.Time       `json:"created_at"`
}

// SecretsRecord is the saved install parameters and first-login fields
// (encrypted with this machine's key).
type SecretsRecord struct {
	ParamsEnc     string `json:"params_enc"`
	FirstLoginEnc string `json:"first_login_enc"`
}

// ServiceRecord is one row of the service registry.
type ServiceRecord struct {
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Source       string    `json:"source"`
	ProvideIndex int       `json:"provide_index"`
	ValuesEnc    string    `json:"values_enc,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// BindingRecord is one plug of an app into a service.
type BindingRecord struct {
	App       string    `json:"app"`
	UseName   string    `json:"use_name"`
	Service   string    `json:"service"`
	IssuedEnc string    `json:"issued_enc,omitempty"`
	Dedicated bool      `json:"dedicated,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// collectRecords reads everything Records holds.
func (m *Manager) collectRecords(ctx context.Context, app string) (Records, error) {
	st := m.cfg.Store
	a, err := st.GetApp(ctx, app)
	if err != nil {
		return Records{}, err
	}
	rec := Records{Format: FormatVersion, TakenAt: m.now(), App: a}
	if rec.Env, err = st.ListEnv(ctx, app); err != nil {
		return Records{}, err
	}
	if rec.Volumes, err = st.ListVolumes(ctx, app); err != nil {
		return Records{}, err
	}
	addons, err := st.ListAddons(ctx, app)
	if err != nil {
		return Records{}, err
	}
	for _, ad := range addons {
		rec.Addons = append(rec.Addons, AddonRecord{Kind: ad.Kind, Image: ad.Image, VolumeName: ad.VolumeName, EnvVar: ad.EnvVar, Credentials: ad.Credentials, CreatedAt: ad.CreatedAt})
	}
	if cs, err := st.GetCatalogSecrets(ctx, app); err == nil {
		rec.CatalogSecrets = &SecretsRecord{ParamsEnc: cs.ParamsEnc, FirstLoginEnc: cs.FirstLoginEnc}
	} else if !errors.Is(err, state.ErrNotFound) {
		return Records{}, err
	}
	if cm, err := st.GetCatalogManifest(ctx, app); err == nil {
		rec.CatalogManifest = &cm
	} else if !errors.Is(err, state.ErrNotFound) {
		return Records{}, err
	}
	svcs, err := st.ListServices(ctx, state.ServiceFilter{App: app})
	if err != nil {
		return Records{}, err
	}
	for _, sv := range svcs {
		rec.Provides = append(rec.Provides, ServiceRecord{Name: sv.Name, Kind: sv.Kind, Source: sv.Source, ProvideIndex: sv.ProvideIndex, ValuesEnc: sv.ValuesEnc, CreatedAt: sv.CreatedAt})
		bs, err := st.ListServiceBindings(ctx, state.BindingFilter{Service: sv.Name})
		if err != nil {
			return Records{}, err
		}
		for _, b := range bs {
			if b.App != app {
				rec.UsedBy = append(rec.UsedBy, bindingRecord(b))
			}
		}
	}
	own, err := st.ListServiceBindings(ctx, state.BindingFilter{App: app})
	if err != nil {
		return Records{}, err
	}
	for _, b := range own {
		rec.Uses = append(rec.Uses, bindingRecord(b))
	}
	return rec, nil
}

func bindingRecord(b state.ServiceBinding) BindingRecord {
	return BindingRecord{App: b.App, UseName: b.UseName, Service: b.Service, IssuedEnc: b.IssuedEnc, Dedicated: b.Dedicated, CreatedAt: b.CreatedAt}
}

// applyRecords puts the app's own records back (see Records) and returns the
// warnings of the service check. The current state is what a restore has
// just replaced the data of, so differences are expected and handled one by
// one; an error is something that could not be written.
func (m *Manager) applyRecords(ctx context.Context, app string, rec Records) ([]string, error) {
	st := m.cfg.Store
	// Settings that follow the version the data belongs to. Memory, CPU,
	// exposure, the entry port and the links belong to this machine's
	// present wiring and stay.
	if _, err := st.UpdateApp(ctx, app, func(a *state.App) error {
		a.Port, a.HealthPath, a.HealthCheck = rec.App.Port, rec.App.HealthPath, rec.App.HealthCheck
		a.Command, a.PidsLimit = rec.App.Command, rec.App.PidsLimit
		a.CatalogID, a.CatalogVersion, a.CatalogSource, a.CatalogOrigin = rec.App.CatalogID, rec.App.CatalogVersion, rec.App.CatalogSource, rec.App.CatalogOrigin
		return nil
	}); err != nil {
		return nil, fmt.Errorf("恢复应用设置：%w", err)
	}

	// Variables: exactly the set of the backup.
	want := map[string]bool{}
	for _, e := range rec.Env {
		want[e.Key] = true
		e.App = app
		if err := st.SetEnv(ctx, e); err != nil {
			return nil, fmt.Errorf("恢复变量 %s：%w", e.Key, err)
		}
	}
	cur, err := st.ListEnv(ctx, app)
	if err != nil {
		return nil, err
	}
	for _, e := range cur {
		if !want[e.Key] {
			if err := st.DeleteEnv(ctx, app, e.Key); err != nil && !errors.Is(err, state.ErrNotFound) {
				return nil, fmt.Errorf("去掉备份之后新加的变量 %s：%w", e.Key, err)
			}
		}
	}

	// Data directories the app had then.
	for _, v := range rec.Volumes {
		if _, _, err := st.AddVolume(ctx, app, v.Path, v.Auto); err != nil {
			return nil, fmt.Errorf("恢复数据目录 %s：%w", v.Path, err)
		}
	}

	var warnings []string
	if err := m.applyAddons(ctx, app, rec, &warnings); err != nil {
		return nil, err
	}

	if rec.CatalogSecrets != nil {
		if err := st.SetCatalogSecrets(ctx, state.CatalogSecrets{App: app, ParamsEnc: rec.CatalogSecrets.ParamsEnc, FirstLoginEnc: rec.CatalogSecrets.FirstLoginEnc}); err != nil {
			return nil, fmt.Errorf("恢复安装参数：%w", err)
		}
	}
	if rec.CatalogManifest != nil {
		cm := *rec.CatalogManifest
		cm.App = app
		if err := st.SetCatalogManifest(ctx, cm); err != nil {
			return nil, fmt.Errorf("恢复安装时的清单：%w", err)
		}
	}
	return append(warnings, m.serviceWarnings(ctx, app, rec)...), nil
}

// applyAddons makes the app's add-on records the backup's.
func (m *Manager) applyAddons(ctx context.Context, app string, rec Records, warnings *[]string) error {
	st := m.cfg.Store
	cur, err := st.ListAddons(ctx, app)
	if err != nil {
		return err
	}
	have := map[string]state.Addon{}
	for _, a := range cur {
		have[a.Kind] = a
	}
	saved := map[string]bool{}
	for _, ad := range rec.Addons {
		saved[ad.Kind] = true
		if c, ok := have[ad.Kind]; ok && sameJSON(c.Credentials, ad.Credentials) && c.EnvVar == ad.EnvVar {
			continue
		}
		// A record that differs (or was soft-deleted with other credentials)
		// would not open the restored data: replace it with the saved one.
		if err := st.RemoveAddon(ctx, app, ad.Kind, true); err != nil && !errors.Is(err, state.ErrNotFound) {
			return fmt.Errorf("换掉附加数据库 %s 的记录：%w", ad.Kind, err)
		}
		if _, err := st.AddAddon(ctx, state.Addon{App: app, Kind: ad.Kind, Image: ad.Image, VolumeName: ad.VolumeName, Credentials: ad.Credentials, EnvVar: ad.EnvVar, CreatedAt: ad.CreatedAt}); err != nil {
			return fmt.Errorf("恢复附加数据库 %s 的记录：%w", ad.Kind, err)
		}
	}
	for kind := range have {
		if !saved[kind] {
			*warnings = append(*warnings, fmt.Sprintf("附加数据库 %s 是备份之后才加的，它的数据没动，也没有从备份里恢复", kind))
		}
	}
	return nil
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return bytes.Equal(xa, ya)
}

// serviceWarnings compares the service registry now with what it was when the
// backup was taken (BKP-15). The accounts a provider app issued live in its
// data: the ones issued before the backup are back with it; the ones issued
// after are gone, and the apps that hold them have to be connected again.
func (m *Manager) serviceWarnings(ctx context.Context, app string, rec Records) []string {
	st := m.cfg.Store
	var out []string
	svcs, err := st.ListServices(ctx, state.ServiceFilter{App: app})
	if err != nil {
		return []string{CodeServiceRecheck + "：读不了服务登记表，没法核对连接（" + err.Error() + "）"}
	}
	now := map[string]bool{}
	for _, sv := range svcs {
		now[sv.Name] = true
	}
	for _, sv := range rec.Provides {
		if !now[sv.Name] {
			out = append(out, fmt.Sprintf("%s：备份时它提供的服务「%s」现在不在登记表里（之后删掉或重装过），用它的应用要重新连接", CodeServiceRecheck, sv.Name))
		}
	}
	// Plugs made after the backup into this app's services: their accounts
	// are not in the restored data.
	var late []string
	for _, sv := range svcs {
		bs, err := st.ListServiceBindings(ctx, state.BindingFilter{Service: sv.Name})
		if err != nil {
			continue
		}
		for _, b := range bs {
			if b.App != app && b.CreatedAt.After(rec.TakenAt) {
				late = append(late, fmt.Sprintf("%s（服务「%s」）", b.App, sv.Name))
			}
		}
	}
	if len(late) > 0 {
		sort.Strings(late)
		out = append(out, fmt.Sprintf("%s：这些应用的账号是备份之后才发的，恢复后在这个应用的数据里已经没有了，它们连不上：%s。把它们重新接上服务（删掉后重新安装，或在它们那边换一个连接）", CodeServiceRecheck, strings.Join(late, "、")))
	}
	// This app's own plugs into other apps' services.
	nowOwn, err := st.ListServiceBindings(ctx, state.BindingFilter{App: app})
	if err == nil {
		byUse := map[string]state.ServiceBinding{}
		for _, b := range nowOwn {
			byUse[b.UseName] = b
		}
		for _, b := range rec.Uses {
			c, ok := byUse[b.UseName]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s：备份时这个应用接着服务「%s」（用在 %s），现在没有接，数据里的设置可能还指着它", CodeServiceRecheck, b.Service, b.UseName))
			case c.Service != b.Service || c.IssuedEnc != b.IssuedEnc:
				out = append(out, fmt.Sprintf("%s：这个应用的「%s」连接在备份之后换过（服务或账号不同），恢复回来的数据里写的可能还是旧的，用不了就重新连接", CodeServiceRecheck, b.UseName))
			}
		}
	}
	return out
}
