package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/machine"
	"github.com/acornfox/acornfox/internal/state"
)

// Machine capabilities (internal/machine): what this machine can do right
// now, for an AI workbench that reads it before it starts a task.
//
//	GET /v1/machine   {schema, generated_at, machine{name, version, region},
//	                   apps[{id, instance, name, tagline, source, state, url, exposure,
//	                   version, manual{has, kind, version}, provides[], uses[{name, kind,
//	                   required, service}]}], solutions[{id, name, apps[], manual}],
//	                   services[{type, name, service, provider, status, used_by[],
//	                   registered_by_user}], platform{backup, upgrade, reapply}, warnings[]}
//
// Read-only and worked out at the moment it is asked, nothing is stored. It
// answers 200 even when a part cannot be worked out (the catalog cannot be
// read, one app's needs cannot be read): that part is missing or short and
// warnings[] says which, with the fault code MACHINE-01. It never contains a
// password, a key, a token or a value of a service or an app setting: it is
// built from the state of the apps, the names of the services and the
// catalog's text, and the types have no field for a value. Routes sit on the
// trusted socket and the logged-in console only.

func (s *server) registerMachine(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/machine", s.getMachine)
}

func (s *server) getMachine(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.machineView(r.Context()))
}

// machineView works the view out. It never fails: a part it cannot read is
// left out and named in Warnings.
func (s *server) machineView(ctx context.Context) machine.View {
	name, _ := os.Hostname()
	v := machine.View{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Machine:     machine.Machine{Name: name, Version: s.version, Region: "global"},
		Platform:    machine.Platform{Backup: s.backups != nil, Upgrade: s.appCenter != nil, Reapply: s.appCenter != nil},
	}
	if s.sources != nil && s.sources().China {
		v.Machine.Region = "cn"
	}
	warn := func(part, format string, a ...any) {
		v.Warnings = append(v.Warnings, machine.Warning{Code: machine.CodeIncomplete, Part: part, Message: fmt.Sprintf(format, a...)})
	}

	apps, err := s.store.ListApps(ctx)
	if err != nil {
		warn("apps", "读不了应用列表：%v", err)
		machine.Normalize(&v)
		return v
	}

	// The catalog: names, one-liners, what an app provides, whether there is
	// a manual. Without it the apps are still listed from their own state.
	var cat appinstall.CatalogView
	haveCat := false
	if s.appCenter != nil {
		if c, cerr := s.appCenter.List(ctx); cerr != nil {
			warn("manuals", "应用目录读不了（%v），应用的名字、一句话、手册、提供的服务没查到", cerr)
		} else {
			cat, haveCat = c, true
		}
	}
	type key struct{ origin, id string }
	catApps := map[key]appinstall.CatalogAppView{}
	catRecipes := map[key]appinstall.RecipeView{}
	if haveCat {
		for _, a := range cat.Apps {
			catApps[key{a.Source.ID, a.ID}] = a
		}
		for _, r := range cat.Recipes {
			catRecipes[key{r.Source.ID, r.ID}] = r
		}
	}
	origin := func(a state.App) string {
		if a.CatalogOrigin == "" {
			return appinstall.PublicSource
		}
		return a.CatalogOrigin
	}

	solApps := map[key][]string{}
	var solOrder []key
	for _, a := range apps {
		av := s.buildAppView(ctx, a, false)
		ma := machine.App{
			ID: a.Name, Instance: a.Name, Name: a.Name, Source: "deploy",
			State: machine.StateOf(a.Desired, av.ObservedState), Exposure: av.Exposure,
		}
		if !av.InternalOnly {
			ma.URL = av.URL
		}
		if a.CatalogID != "" {
			ma.ID, ma.Source, ma.Version = a.CatalogID, "catalog", a.CatalogVersion
			if ca, ok := catApps[key{origin(a), a.CatalogID}]; ok {
				ma.Name, ma.Tagline = ca.Name, ca.Summary
				if ca.Show != nil {
					if ca.Show.Name != "" {
						ma.Name = ca.Show.Name
					}
					if ca.Show.Tagline != "" {
						ma.Tagline = ca.Show.Tagline
					}
					ma.Manual = machine.ManualOf(ca.Show, "app")
				}
				ma.Provides = ca.Provides
			}
			if s.appCenter != nil {
				if uses, uerr := s.appCenter.AppServices(ctx, a.Name); uerr != nil {
					warn("apps", "%s 用到哪些服务读不出来：%v", a.Name, uerr)
				} else {
					for _, u := range uses {
						ma.Uses = append(ma.Uses, machine.Use{Name: u.Use, Kind: u.Kind, Required: u.Required, Service: u.Service})
					}
				}
			}
			if rid, ok := strings.CutPrefix(a.CatalogSource, "recipe:"); ok && rid != "" {
				k := key{origin(a), rid}
				if _, seen := solApps[k]; !seen {
					solOrder = append(solOrder, k)
				}
				solApps[k] = append(solApps[k], a.CatalogID)
			}
		}
		v.Apps = append(v.Apps, ma)
	}

	for _, k := range solOrder {
		sol := machine.Solution{ID: k.id, Name: k.id, Apps: solApps[k]}
		if r, ok := catRecipes[k]; ok {
			sol.Name = r.Title
			if r.Show != nil {
				if r.Show.Name != "" {
					sol.Name = r.Show.Name
				}
				sol.Manual = machine.ManualOf(r.Show, "solution")
			}
		}
		v.Solutions = append(v.Solutions, sol)
	}

	if s.appCenter != nil {
		sl, serr := s.appCenter.Services(ctx)
		if serr != nil {
			warn("services", "服务登记表读不了：%v", serr)
		}
		if len(sl.Unreadable) > 0 {
			warn("services", "有 %d 条服务存的值解不开，没列出来：%s", len(sl.Unreadable), strings.Join(sl.Unreadable, "、"))
		}
		for _, sv := range sl.Services {
			ms := machine.Service{
				Type: sv.Kind, Name: sv.KindLabel, Service: sv.Name,
				Status: string(sv.Status), RegisteredByUser: sv.Source == state.ServiceFromUser,
			}
			if sv.Source == state.ServiceFromApp {
				ms.Provider = sv.App
			}
			seen := map[string]bool{}
			for _, u := range sv.UsedBy {
				if !u.Gone && !seen[u.App] {
					seen[u.App] = true
					ms.UsedBy = append(ms.UsedBy, u.App)
				}
			}
			v.Services = append(v.Services, ms)
		}
	}
	machine.Normalize(&v)
	return v
}
