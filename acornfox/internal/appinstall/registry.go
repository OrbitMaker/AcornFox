package appinstall

// The service registry: what this machine can offer to its apps, and which
// need of which app is plugged into which service.
//
// A service has a name (unique on the machine), a kind (the service type) and
// values (the items of that type: an address, a key, ...). It is either
// provided by an installed app or registered by the user. A binding says that
// one need of an app (an entry of its manifest's uses) is plugged into a
// service, and holds what the provider issued to that consumer alone.
//
// This file is the layer between the store and whoever shows, matches or
// installs: it keeps values in clear on its side and ciphertext on the
// store's side (the same key file as the install parameters), and it holds
// the fixed rules that must not depend on who calls: how a secret is shown,
// when a service counts as usable, what an app's service is called.
//
// Usage notes (who builds the commands, the API and the matching on this):
//
// Getting a Registry: Engine.Registry (shares the engine's key holder) or,
// with no engine, NewRegistry(store, dataDir). *state.Store is a ServiceStore.
//
// Services:
//
//   - CreateService: add one. state.ErrServiceNameTaken when the name is in
//     use, state.ErrServiceEntryTaken when the app already has a service for
//     that provides entry (both are state.ErrConflict); state.ErrInvalid when
//     malformed (state.ValidServiceName says what a name can be: the user's
//     own words, no blanks or punctuation other than "-", "_" and ".").
//   - UpdateService: read, let fn change Values and ProvideIndex, write, in
//     one transaction. Nothing else can change. fn must not call the
//     registry or the store: the database has one connection and fn is
//     holding it, so such a call waits until the context ends. Look up what
//     fn needs (who uses the service, ...) before calling UpdateService.
//   - GetService: by name; state.ErrNotFound.
//   - ListServices: ordered by name; filter by kind and / or providing app.
//   - DeleteService: refuses with *state.ServiceInUseError (its Users and
//     Apps say who) while any app is plugged into the service.
//   - DeleteAppServices: every service an app provides, all or nothing; the
//     same error while another app uses any of them.
//
// Bindings:
//
//   - SetBinding: plug a need into a service, or replace what was issued for
//     it; state.ErrNotFound when the service is missing. It overwrites what
//     the need held before without a word (see the order below).
//   - GetBinding: one need of one app; state.ErrNotFound.
//   - ListBindings: filter by consuming app and / or service; the zero filter
//     lists everything (who uses what, in one query).
//   - DeleteBinding: one need; reports whether there was one.
//   - DeleteAppBindings: every need of an app.
//
// The credentials of a binding exist nowhere else. Whatever makes a binding
// go or change (SetBinding to another service, SetBinding with newly issued
// credentials, DeleteBinding, DeleteAppBindings) comes after the caller has
// read the binding and taken back what it held; otherwise the provider keeps
// a credential nobody can revoke any more.
//
// Deleting an app in the store does not touch the registry. The order on
// uninstall is: take back what was issued (the bindings still hold it), then
// DeleteAppBindings, then DeleteAppServices.
//
// Fixed rules (pure functions, no store):
//
//   - MaskServiceSecret: a secret for display, its last 4 characters only,
//     or nothing of it when it has fewer than 8.
//   - MaskServiceValues: a copy of the values with the secret items of the
//     kind, and the items the kind does not know, masked.
//   - MaskIssued: a copy with every value masked.
//   - ServiceStatusOf: ok, incomplete, provider_stopped or provider_gone
//     (the words that go out as "status"; not_ready is put over ok by the
//     readiness check, ready.go), the label to show for it, and
//     which required items are missing. Never stored; work it out when
//     asked. ServiceHealth marshals to the three fields that go out:
//     status, status_label, missing.
//   - ProviderStateOf: the provider state ServiceStatusOf wants, from the
//     app's row (nil: the app is gone). It goes by what the app is meant to
//     do (stopped by the user or not), not by whether its container is up:
//     a check that must notice a crashed provider passes its own state.
//   - DefaultServiceName: what the service of one provides entry is called.
//     Registry.AppServiceNames does it for a whole app against the names
//     already in use, and refuses (*ServiceKindChangedError) when an entry
//     that already has a service is now of another kind.
//
// Things to know:
//
//   - Values and issued credentials are text. A number or a boolean that
//     comes out of a JSON answer is stored in its JSON text form by the
//     caller.
//   - Service and Binding carry secrets in clear and do not marshal them
//     (json:"-"): build what goes out from MaskServiceValues and MaskIssued.
//   - ServiceField is the minimum this file needs to know about a kind; it
//     is meant to be filled from the catalog's service type definitions.
//   - Values and issued credentials must be valid UTF-8; anything else is
//     refused (state.ErrInvalid) rather than stored altered.
//   - A value that cannot be decrypted (the key file was replaced, or the
//     row is damaged) is a *ServiceUnreadableError (errors.Is
//     ErrServiceUnreadable) naming the service or the binding. One such row
//     makes ListServices / ListBindings fail as a whole; Unreadable lists
//     the rows in question without failing, and DeleteService,
//     DeleteBinding still work on them (they do not decrypt).
//   - Use one key holder per process: Engine.Registry where there is an
//     engine. Two holders on a data directory with no key file yet can both
//     try to create it, and one of them fails.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/acornfox/acornfox/internal/state"
)

// Service is one service of the registry with its values in clear.
type Service struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Source is state.ServiceFromApp or state.ServiceFromUser.
	Source string `json:"source"`
	// App and ProvideIndex say which entry of which app's provides the
	// service comes from (state.ServiceFromApp only).
	App          string `json:"app,omitempty"`
	ProvideIndex int    `json:"provide_index"`
	// Values are the items every consumer gets, {item: value}. What the
	// provider issues per consumer is not here but in each Binding.
	Values    map[string]string `json:"-"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Binding says that the need UseName of App is plugged into Service.
type Binding struct {
	App     string `json:"app"`
	UseName string `json:"use_name"`
	Service string `json:"service"`
	// Issued is everything the provider's issue steps saved for this
	// consumer; it is what the revoke steps get back later.
	Issued map[string]string `json:"-"`
	// Dedicated: the need takes the service for itself (uses[].dedicated):
	// no other app may be plugged into it while this binding is there.
	Dedicated bool `json:"dedicated,omitempty"`
	// MaxConsumers is not stored: the providing entry's max_consumers (0:
	// no limit), which SetBinding checks when it writes.
	MaxConsumers int       `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// ServiceField is what the registry needs to know about one item of a
// service kind.
type ServiceField struct {
	Key      string
	Required bool // the service is not usable without a value for it
	Secret   bool // never shown in full
}

// ErrServiceUnreadable means a stored value could not be decrypted. The error
// returned is a *ServiceUnreadableError.
var ErrServiceUnreadable = errors.New("登记表里的值解不开")

// ServiceUnreadableError says which row of the registry could not be
// decrypted. It matches ErrServiceUnreadable with errors.Is.
type ServiceUnreadableError struct {
	// Service is the service whose values are unreadable, or, for a binding,
	// the service the need is plugged into.
	Service string
	// App and UseName are set when it is a binding's issued credentials that
	// are unreadable.
	App     string
	UseName string
	// Err is why (never holds a value).
	Err error
}

func (e *ServiceUnreadableError) Error() string {
	if e.App != "" {
		return fmt.Sprintf("%v：%s 的 %s 接入（%v）", ErrServiceUnreadable, e.App, e.UseName, e.Err)
	}
	return fmt.Sprintf("%v：服务 %s（%v）", ErrServiceUnreadable, e.Service, e.Err)
}

// Is makes errors.Is(err, ErrServiceUnreadable) true.
func (e *ServiceUnreadableError) Is(target error) bool { return target == ErrServiceUnreadable }

// Unwrap returns the cause.
func (e *ServiceUnreadableError) Unwrap() error { return e.Err }

// ServiceKindChangedError is returned by AppServiceNames when an entry of an
// app's provides already has a service and that service is of another kind
// than the entry now is (the entries were reordered or retyped). A service
// cannot change kind, so its name must not be handed to the new entry.
type ServiceKindChangedError struct {
	App     string
	Index   int    // the entry of the app's provides
	Service string // the service that entry has
	Have    string // the kind of that service
	Want    string // the kind of the entry now
}

func (e *ServiceKindChangedError) Error() string {
	return fmt.Sprintf("应用 %s 提供的第 %d 条服务（%s）登记的类型是 %s，现在的清单里这一条是 %s", e.App, e.Index+1, e.Service, e.Have, e.Want)
}

// ServiceStore is what the registry needs from *state.Store.
type ServiceStore interface {
	CreateService(ctx context.Context, sv state.Service) (state.Service, error)
	UpdateService(ctx context.Context, name string, fn func(*state.Service) error) (state.Service, error)
	GetService(ctx context.Context, name string) (state.Service, error)
	ListServices(ctx context.Context, f state.ServiceFilter) ([]state.Service, error)
	DeleteService(ctx context.Context, name string) error
	DeleteAppServices(ctx context.Context, app string) ([]state.Service, error)
	SetServiceBinding(ctx context.Context, b state.ServiceBinding) (state.ServiceBinding, error)
	CheckServicePlug(ctx context.Context, b state.ServiceBinding) error
	GetServiceBinding(ctx context.Context, app, useName string) (state.ServiceBinding, error)
	ListServiceBindings(ctx context.Context, f state.BindingFilter) ([]state.ServiceBinding, error)
	DeleteServiceBinding(ctx context.Context, app, useName string) (bool, error)
	MoveServiceBinding(ctx context.Context, app, from, to string) error
	DeleteAppBindings(ctx context.Context, app string) (int, error)
}

// Registry reads and writes the service registry with values in clear.
type Registry struct {
	store ServiceStore
	box   *secretBox
}

// NewRegistry builds a registry on store whose key is the one under dataDir
// (<data>/secrets/catalog.key, created on first use). Where an Engine exists
// use Engine.Registry instead, so both go through one key holder.
func NewRegistry(store ServiceStore, dataDir string) *Registry {
	return &Registry{store: store, box: newSecretBox(dataDir)}
}

// Registry returns the registry on the engine's store and key. It fails when
// the engine's store has no registry operations.
func (e *Engine) Registry() (*Registry, error) {
	st, ok := e.cfg.Store.(ServiceStore)
	if !ok {
		return nil, errors.New("这个存储没有服务登记表的操作")
	}
	return &Registry{store: st, box: e.box}, nil
}

// sealValues encrypts a set of values; an empty set is stored as "".
func (r *Registry) sealValues(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	for k, v := range values {
		if k == "" {
			return "", fmt.Errorf("%w: 项名不能是空的", state.ErrInvalid)
		}
		// Encoding would quietly replace the bytes that are not text, and
		// the value read back would not be the value given.
		if !utf8.ValidString(k) || !utf8.ValidString(v) {
			return "", fmt.Errorf("%w: 值里有不是文字的字节（不是合法的 UTF-8）", state.ErrInvalid)
		}
	}
	plain, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return r.box.seal(plain)
}

// openValues decrypts what sealValues made; "" is an empty set. The result is
// never nil.
func (r *Registry) openValues(enc string) (map[string]string, error) {
	out := map[string]string{}
	if enc == "" {
		return out, nil
	}
	plain, err := r.box.open(enc)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}

func (r *Registry) openService(row state.Service) (Service, error) {
	values, err := r.openValues(row.ValuesEnc)
	if err != nil {
		return Service{}, &ServiceUnreadableError{Service: row.Name, Err: err}
	}
	return Service{
		Name: row.Name, Kind: row.Kind, Source: row.Source, App: row.App, ProvideIndex: row.ProvideIndex,
		Values: values, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *Registry) openBinding(row state.ServiceBinding) (Binding, error) {
	issued, err := r.openValues(row.IssuedEnc)
	if err != nil {
		return Binding{}, &ServiceUnreadableError{Service: row.Service, App: row.App, UseName: row.UseName, Err: err}
	}
	return Binding{App: row.App, UseName: row.UseName, Service: row.Service, Issued: issued, Dedicated: row.Dedicated, CreatedAt: row.CreatedAt}, nil
}

// CreateService registers sv. Errors are the store's: state.ErrInvalid,
// state.ErrServiceNameTaken, state.ErrServiceEntryTaken (the last two are
// state.ErrConflict).
func (r *Registry) CreateService(ctx context.Context, sv Service) (Service, error) {
	enc, err := r.sealValues(sv.Values)
	if err != nil {
		return Service{}, err
	}
	row, err := r.store.CreateService(ctx, state.Service{
		Name: sv.Name, Kind: sv.Kind, Source: sv.Source, App: sv.App, ProvideIndex: sv.ProvideIndex, ValuesEnc: enc,
	})
	if err != nil {
		return Service{}, err
	}
	return r.openService(row)
}

// UpdateService reads the service, lets fn change its Values and
// ProvideIndex, and writes it back, all in one transaction. fn gets the
// values in clear and may edit the map in place or replace it; an error from
// fn leaves the service as it was. state.ErrNotFound when there is none.
//
// fn must not call the registry or the store: it runs inside the transaction
// on the database's only connection, and such a call would wait for that
// connection until ctx ends.
func (r *Registry) UpdateService(ctx context.Context, name string, fn func(*Service) error) (Service, error) {
	var out Service
	row, err := r.store.UpdateService(ctx, name, func(row *state.Service) error {
		sv, err := r.openService(*row)
		if err != nil {
			return err
		}
		if err := fn(&sv); err != nil {
			return err
		}
		enc, err := r.sealValues(sv.Values)
		if err != nil {
			return err
		}
		row.ValuesEnc, row.ProvideIndex = enc, sv.ProvideIndex
		if sv.Values == nil {
			sv.Values = map[string]string{}
		}
		out = sv
		return nil
	})
	if err != nil {
		return Service{}, err
	}
	out.Name, out.Kind, out.Source, out.App = row.Name, row.Kind, row.Source, row.App
	out.ProvideIndex, out.CreatedAt, out.UpdatedAt = row.ProvideIndex, row.CreatedAt, row.UpdatedAt
	return out, nil
}

// GetService returns the service called name; state.ErrNotFound.
func (r *Registry) GetService(ctx context.Context, name string) (Service, error) {
	row, err := r.store.GetService(ctx, name)
	if err != nil {
		return Service{}, err
	}
	return r.openService(row)
}

// ListServices returns the services matching f, ordered by name. A row that
// cannot be decrypted fails the whole call (*ServiceUnreadableError says
// which); Unreadable finds every such row.
func (r *Registry) ListServices(ctx context.Context, f state.ServiceFilter) ([]Service, error) {
	rows, err := r.store.ListServices(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]Service, 0, len(rows))
	for _, row := range rows {
		sv, err := r.openService(row)
		if err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, nil
}

// DeleteService removes the service called name. While apps are plugged into
// it nothing is removed and the error is a *state.ServiceInUseError that says
// which.
func (r *Registry) DeleteService(ctx context.Context, name string) error {
	return r.store.DeleteService(ctx, name)
}

// DeleteAppServices removes every service app provides and returns their
// names. While another app is plugged into any of them nothing is removed and
// the error is a *state.ServiceInUseError.
func (r *Registry) DeleteAppServices(ctx context.Context, app string) ([]string, error) {
	rows, err := r.store.DeleteAppServices(ctx, app)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names, nil
}

// SetBinding plugs the need b.UseName of b.App into b.Service, or replaces
// what was issued for it. state.ErrNotFound when the service does not exist;
// a *state.ServiceTakenError when b.Dedicated or b.MaxConsumers leave no
// room for it, given the other apps plugged into the service (checked in the
// transaction that writes it).
//
// What the need held before is overwritten. Before moving a need to another
// service, or storing newly issued credentials, read the binding
// (GetBinding) and take the old credentials back from the provider: after
// this call they are not recorded anywhere.
func (r *Registry) SetBinding(ctx context.Context, b Binding) (Binding, error) {
	enc, err := r.sealValues(b.Issued)
	if err != nil {
		return Binding{}, err
	}
	row, err := r.store.SetServiceBinding(ctx, state.ServiceBinding{App: b.App, UseName: b.UseName, Service: b.Service, IssuedEnc: enc,
		Dedicated: b.Dedicated, MaxConsumers: b.MaxConsumers})
	if err != nil {
		return Binding{}, err
	}
	return r.openBinding(row)
}

// CheckPlug tells, writing nothing, whether SetBinding(b) would be refused
// now because of the other apps plugged into b.Service (a
// *state.ServiceTakenError) or not (nil). SetBinding checks again.
func (r *Registry) CheckPlug(ctx context.Context, b Binding) error {
	return r.store.CheckServicePlug(ctx, state.ServiceBinding{App: b.App, UseName: b.UseName, Service: b.Service, Dedicated: b.Dedicated, MaxConsumers: b.MaxConsumers})
}

// GetBinding returns what the need useName of app is plugged into;
// state.ErrNotFound.
func (r *Registry) GetBinding(ctx context.Context, app, useName string) (Binding, error) {
	row, err := r.store.GetServiceBinding(ctx, app, useName)
	if err != nil {
		return Binding{}, err
	}
	return r.openBinding(row)
}

// ListBindings returns the bindings matching f, ordered by app and need. A
// row that cannot be decrypted fails the whole call (see ListServices).
func (r *Registry) ListBindings(ctx context.Context, f state.BindingFilter) ([]Binding, error) {
	rows, err := r.store.ListServiceBindings(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(rows))
	for _, row := range rows {
		b, err := r.openBinding(row)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// DeleteBinding unplugs the need useName of app and reports whether it was
// plugged into anything. What was issued for it goes with the row: take it
// back first.
func (r *Registry) DeleteBinding(ctx context.Context, app, useName string) (bool, error) {
	return r.store.DeleteServiceBinding(ctx, app, useName)
}

// ParkBinding moves the binding of need useName of app aside
// (state.ParkedUseName), what it holds unchanged; UnparkBinding moves it
// back. Both state.ErrNotFound when there is nothing to move and
// state.ErrConflict when the place is taken.
func (r *Registry) ParkBinding(ctx context.Context, app, useName string) error {
	return r.store.MoveServiceBinding(ctx, app, useName, state.ParkedUseName(useName))
}

// UnparkBinding: see ParkBinding.
func (r *Registry) UnparkBinding(ctx context.Context, app, useName string) error {
	return r.store.MoveServiceBinding(ctx, app, state.ParkedUseName(useName), useName)
}

// DeleteAppBindings unplugs every need of app and returns how many there
// were. It does not take anything back from the providers.
func (r *Registry) DeleteAppBindings(ctx context.Context, app string) (int, error) {
	return r.store.DeleteAppBindings(ctx, app)
}

// Unreadable returns one *ServiceUnreadableError per row of the registry
// that cannot be decrypted (services first, by name; then bindings, by app
// and need), nil when every row reads. It is for a health check: unlike the
// List functions it does not stop at the first.
func (r *Registry) Unreadable(ctx context.Context) ([]*ServiceUnreadableError, error) {
	var out []*ServiceUnreadableError
	note := func(err error) error {
		var bad *ServiceUnreadableError
		if errors.As(err, &bad) {
			out = append(out, bad)
			return nil
		}
		return err
	}
	rows, err := r.store.ListServices(ctx, state.ServiceFilter{})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, err := r.openService(row); err != nil {
			if err := note(err); err != nil {
				return nil, err
			}
		}
	}
	brows, err := r.store.ListServiceBindings(ctx, state.BindingFilter{})
	if err != nil {
		return nil, err
	}
	for _, row := range brows {
		if _, err := r.openBinding(row); err != nil {
			if err := note(err); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// AppServiceNames returns the name of the service of every provides entry of
// app, kinds being the kind of each entry in manifest order. An entry that
// already has a service keeps that service's name; the others get
// DefaultServiceName against every name in use, the ones just given
// included. It only reads.
//
// An entry that already has a service of another kind (a later version of
// the app reordered or retyped its provides) is a *ServiceKindChangedError:
// a service cannot change kind, and its name must not go to the new entry.
// A service of the app whose entry is beyond kinds (the entry was removed)
// is not looked at.
func (r *Registry) AppServiceNames(ctx context.Context, app string, kinds []string) ([]string, error) {
	rows, err := r.store.ListServices(ctx, state.ServiceFilter{})
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	have := map[int]state.Service{}
	for _, row := range rows {
		taken[row.Name] = true
		if row.Source == state.ServiceFromApp && row.App == app {
			have[row.ProvideIndex] = row
		}
	}
	names := make([]string, len(kinds))
	for i := range kinds {
		if row, ok := have[i]; ok {
			if row.Kind != kinds[i] {
				return nil, &ServiceKindChangedError{App: app, Index: i, Service: row.Name, Have: row.Kind, Want: kinds[i]}
			}
			names[i] = row.Name
			continue
		}
		names[i] = DefaultServiceName(app, kinds, i, func(n string) bool { return taken[n] })
		taken[names[i]] = true
	}
	return names, nil
}

// DefaultServiceName returns what the service of entry index of app's
// provides is called by default. kinds is the kind of every entry, in
// manifest order; taken reports whether a name is already in use (nil: none
// is).
//
//   - An app with one entry: the app's name.
//   - An app with several: the app's name, "-" and the entry's kind.
//   - Several entries of the same kind in one app: the second gets "-2"
//     appended, the third "-3", and so on.
//   - When the name is taken, or is a word no service can be called
//     (state.ReservedServiceName), the number goes up (starting at "-2")
//     until the name is free.
//
// For an app name and kinds of the accepted forms the result is always a
// valid service name. When names are worked out for several entries before
// any is stored, taken must also report the ones just given. An index
// outside kinds returns "".
func DefaultServiceName(app string, kinds []string, index int, taken func(name string) bool) string {
	if index < 0 || index >= len(kinds) {
		return ""
	}
	base := app
	n := 1
	if len(kinds) > 1 {
		base = app + "-" + kinds[index]
		for _, k := range kinds[:index] {
			if k == kinds[index] {
				n++
			}
		}
	}
	for {
		name := base
		if n > 1 {
			name = base + "-" + strconv.Itoa(n)
		}
		if !state.ReservedServiceName(name) && (taken == nil || !taken(name)) {
			return name
		}
		n++
	}
}

// MaskServiceSecret is how a secret value of the registry is shown: the mask
// and the last 4 characters, or the mask alone when the value has fewer than
// 8 characters. An empty value stays empty.
func MaskServiceSecret(v string) string {
	if v == "" {
		return ""
	}
	r := []rune(v)
	if len(r) < 8 {
		return Masked
	}
	return Masked + string(r[len(r)-4:])
}

// MaskServiceValues returns a copy of values fit to show: the items fields
// marks secret are masked, and so is any item fields does not know (without
// the kind's definition nothing says it is harmless).
func MaskServiceValues(values map[string]string, fields []ServiceField) map[string]string {
	open := map[string]bool{}
	for _, f := range fields {
		if !f.Secret {
			open[f.Key] = true
		}
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		if open[k] {
			out[k] = v
		} else {
			out[k] = MaskServiceSecret(v)
		}
	}
	return out
}

// MaskIssued returns a copy of what was issued to a consumer with every value
// masked: issued credentials are never shown in full.
func MaskIssued(issued map[string]string) map[string]string {
	return MaskServiceValues(issued, nil)
}

// ProviderState is what the app providing a service is doing now.
type ProviderState string

const (
	ProviderNotApplicable ProviderState = "none"    // the user's own service: no app behind it
	ProviderRunning       ProviderState = "running" // the app is there and meant to run
	ProviderStopped       ProviderState = "stopped" // the app is there but stopped
	ProviderGone          ProviderState = "gone"    // the app has been deleted
)

// ProviderStateOf works out the provider state of sv from the row of the
// providing app (nil when there is no such app any more). It goes by what
// the app is meant to do, not by what its container is doing right now; a
// caller that knows better passes its own state to ServiceStatusOf.
func ProviderStateOf(sv Service, app *state.App) ProviderState {
	switch {
	case sv.Source != state.ServiceFromApp:
		return ProviderNotApplicable
	case app == nil:
		return ProviderGone
	case app.Desired == state.DesiredStopped:
		return ProviderStopped
	}
	return ProviderRunning
}

// ServiceStatus is whether a service can be used right now. It is never
// stored: ServiceStatusOf works it out. The values are the words that go out
// as "status" to the pages, the command line and other programs; they are
// part of the interface and must not change.
type ServiceStatus string

const (
	ServiceOK              ServiceStatus = "ok"               // usable
	ServiceIncomplete      ServiceStatus = "incomplete"       // required items have no value yet
	ServiceProviderStopped ServiceStatus = "provider_stopped" // the app providing it is stopped
	ServiceProviderGone    ServiceStatus = "provider_gone"    // the app providing it has been deleted
	// ServiceNotReady: everything else is fine, but the providing app's
	// readiness check (its manifest's provides[].ready) did not pass just
	// now. ServiceStatusOf never gives it: it is put over ok by whoever ran
	// the check (ready.go). Still usable: it does not stop matching.
	ServiceNotReady ServiceStatus = "not_ready"
)

// Label is the status in the user's words (what goes out as "status_label").
func (s ServiceStatus) Label() string {
	switch s {
	case ServiceOK:
		return "可用"
	case ServiceIncomplete:
		return "还差必填项"
	case ServiceProviderStopped:
		return "提供它的应用停着"
	case ServiceProviderGone:
		return "提供它的应用已删"
	case ServiceNotReady:
		return "还不能用"
	}
	return ""
}

// ServiceHealth is the status of a service and what it still lacks. It
// marshals to the fields that go out with a service: status, status_label
// and missing (an empty list, never null, when nothing is missing).
type ServiceHealth struct {
	Status ServiceStatus `json:"status"`
	// StatusLabel is Status.Label().
	StatusLabel string `json:"status_label"`
	// Missing are the required items with no value, in the order of the
	// kind's fields; never nil. It is filled whatever the status is, so a
	// stopped provider that also lacks an item shows both.
	Missing []string `json:"missing"`
}

// Usable reports whether the service can be handed to an app now. A
// service that is not ready yet (its provider's readiness check did not
// pass) is: being not ready does not stop matching, the user is told.
func (h ServiceHealth) Usable() bool { return h.Status == ServiceOK || h.Status == ServiceNotReady }

// ServiceStatusOf works out the status of sv.
//
// fields are the items of its kind. perConsumer are the items the provider
// issues to each consumer separately: they are not expected among the
// service's own values and never count as missing. provider is what the
// providing app is doing; it is ignored for a user's service.
//
// One status is reported, the first that applies:
//
//  1. the providing app is gone;
//  2. the providing app is not running (stopped, or a state this function
//     does not know: only ProviderRunning makes an app's service usable);
//  3. a required item has no value (an empty value is no value);
//  4. ok.
func ServiceStatusOf(sv Service, fields []ServiceField, perConsumer []string, provider ProviderState) ServiceHealth {
	issued := map[string]bool{}
	for _, k := range perConsumer {
		issued[k] = true
	}
	h := ServiceHealth{Missing: []string{}}
	for _, f := range fields {
		if f.Required && !issued[f.Key] && sv.Values[f.Key] == "" {
			h.Missing = append(h.Missing, f.Key)
		}
	}
	fromApp := sv.Source == state.ServiceFromApp
	switch {
	case fromApp && provider == ProviderGone:
		h.Status = ServiceProviderGone
	case fromApp && provider != ProviderRunning:
		h.Status = ServiceProviderStopped
	case len(h.Missing) > 0:
		h.Status = ServiceIncomplete
	default:
		h.Status = ServiceOK
	}
	h.StatusLabel = h.Status.Label()
	return h
}
