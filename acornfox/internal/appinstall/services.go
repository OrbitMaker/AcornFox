package appinstall

// The service commands: what the API (GET/POST/PATCH/DELETE /v1/services…),
// the command line (acornfox services …) and the console's "服务" section
// do with the service registry (registry.go). The rules live here, not in
// the callers:
//
//   - A service the user registers is of a kind the catalogs in use define
//     (catalog.KnownServiceTypes). Its items are checked against that kind's
//     definition: no item the kind does not have, every required item given,
//     each value of the item's type (int, bool, url, enum: one of the
//     options), defaults filled in. Every value travels and is stored as
//     text; the stored form is the checked one ("587", "true").
//   - An address a container cannot reach is refused (CAT-SVC-07): the value
//     of an item of type url, and the host the kind's test would connect to,
//     must not be this machine's loopback (127.0.0.1, ::1, localhost, …).
//     Which items are addresses comes from the kind's definition, never from
//     an item's name.
//   - A service an app provides can be looked at and tried, and the items its
//     manifest leaves to the user ({ask: …}) can be filled in; nothing else
//     of it can be changed, and it is not deleted here (CAT-SVC-05): it goes
//     with the app. One whose app is gone can be deleted.
//   - A service still in use is not deleted (CAT-SVC-04, with used_by).
//   - Trying a service follows the kind's test (tcp or http) and nothing
//     else: there is no code for any particular kind. A service an app
//     provides is reached through the port its container publishes on this
//     machine; when that is not possible the answer says so and points at
//     the app.
//   - Secrets go out as MaskServiceSecret only: in answers, notes, errors and
//     test details.
//
// Whoever calls: Engine.ServiceKinds, Services, Service, AddService,
// PatchService, TestService, RemoveService. Errors are *Fault (CAT-SVC-*,
// CAT-INST-20), *ServiceInUseFault (CAT-SVC-04, says who) or *NoServiceError
// (no service by that name; errors.Is state.ErrNotFound).

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Fault codes of the service commands. Never renumber; new codes are
// appended (the fault catalog has the full entries).
const (
	CodeSvcUnknownKind = "CAT-SVC-01" // the kind is not defined by the catalogs in use
	CodeSvcBadValue    = "CAT-SVC-02" // a name, an item or a value is missing or malformed
	CodeSvcNameTaken   = "CAT-SVC-03" // a service of that name exists
	CodeSvcInUse       = "CAT-SVC-04" // apps are plugged into the service (used_by says which)
	CodeSvcFromApp     = "CAT-SVC-05" // the service is an app's: only its ask items can be filled, it goes with the app
	CodeSvcTestFailed  = "CAT-SVC-06" // trying the service did not get through
	CodeSvcLoopback    = "CAT-SVC-07" // an address is this machine's loopback, which a container cannot reach
)

// serviceFaults join Faults (the hint every fault carries comes from there;
// the fault catalog checks the two say the same).
var serviceFaults = []FaultDef{
	{CodeSvcUnknownKind, "目录里没有这种服务类型：类型名写错了，或者这台机器的目录太旧", "acornfox services kinds 看有哪些类型；目录太旧就 acornfox catalog sync 后再试"},
	{CodeSvcBadValue, "登记或修改服务时名字、项或值不对：缺了必填项、没有这一项、值的格式不对（数字、true/false、网址、可选值），或者名字不合规矩", "按 message 改；acornfox services kinds 看这种服务有哪些项、哪些必填、能填什么"},
	{CodeSvcNameTaken, "这台机器上已经有同名的服务", "换个名字（--name）；要改已有的那条用 acornfox services set"},
	{CodeSvcInUse, "还有应用在用这条服务，不能删", "错误里列着是哪些应用在用它；要删这条服务，得先删掉这些应用"},
	{CodeSvcFromApp, "这条服务是一个应用提供的：只有它要你在应用里做一步再填进来的那几项能填，别的项由应用算出来；它也不能单独删，删掉那个应用它才会消失", "要填的项用 acornfox services set 名字 项=值；要它消失就删掉提供它的应用"},
	{CodeSvcTestFailed, "试连没通：连不上那个地址，或者连上了但回答不对", "看 detail 写的原因：拒绝连接、超时、找不到地址查地址和端口；回答的状态码不对查 Key 或账号；改值用 acornfox services set"},
	{CodeSvcLoopback, "地址写的是这台机器的本机回环地址（127.0.0.1、localhost、::1）：应用跑在容器里，容器里的这个地址指的是容器自己，连不到这台机器上的服务", "服务在这台机器上：改填这台机器在局域网里的地址；服务是 AcornFox 装的应用：不用登记，装好后它会自己出现在服务列表里"},
}

// serviceFaultStatus is the HTTP status of each CAT-SVC fault.
var serviceFaultStatus = map[string]int{
	CodeSvcUnknownKind: http.StatusBadRequest,
	CodeSvcBadValue:    http.StatusBadRequest,
	CodeSvcNameTaken:   http.StatusConflict,
	CodeSvcInUse:       http.StatusConflict,
	CodeSvcFromApp:     http.StatusConflict,
	CodeSvcTestFailed:  http.StatusBadGateway,
	CodeSvcLoopback:    http.StatusBadRequest,
}

func init() { Faults = append(Faults, serviceFaults...) }

// NoServiceError: there is no service by that name. It matches
// state.ErrNotFound with errors.Is.
type NoServiceError struct{ Name string }

func (e *NoServiceError) Error() string {
	return fmt.Sprintf("这台机器上没有叫 %s 的服务（acornfox services list 看有哪些）", e.Name)
}

// Is makes errors.Is(err, state.ErrNotFound) true.
func (e *NoServiceError) Is(target error) bool { return target == state.ErrNotFound }

// ServiceInUseFault is CAT-SVC-04 with the needs in the way.
type ServiceInUseFault struct {
	*Fault
	UsedBy []ServiceUse
}

// Unwrap returns the fault, so errors.As finds a *Fault.
func (e *ServiceInUseFault) Unwrap() error { return e.Fault }

// ServiceUse is one need of an app plugged into a service.
type ServiceUse struct {
	App string `json:"app"`
	Use string `json:"use"`
	// Gone: the app was deleted with its data kept; its binding stays for
	// an install on that data and does not keep the service from being
	// removed.
	Gone bool `json:"gone,omitempty"`
	// Dedicated: this need holds the service for itself (its manifest's
	// uses[].dedicated): no other app may be plugged in while it is.
	Dedicated bool `json:"dedicated,omitempty"`
}

// ---- views (the JSON the API answers with) ----

// ServiceKindView is one service type as GET /v1/services/kinds lists it.
type ServiceKindView struct {
	Kind    string             `json:"kind"`
	Label   string             `json:"label"`
	Summary string             `json:"summary"`
	Fields  []ServiceFieldView `json:"fields"`
	HasTest bool               `json:"has_test"`
}

// ServiceFieldView is one item of a kind. Default is text ("" for none):
// values travel as text both ways.
type ServiceFieldView struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Secret   bool     `json:"secret"`
	Default  string   `json:"default"`
	Options  []string `json:"options"`
	Hint     string   `json:"hint"`
}

// ServiceView is one service as the API shows it.
type ServiceView struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	KindLabel string `json:"kind_label"`
	Source    string `json:"source"`
	App       string `json:"app"`
	ServiceHealth
	Values []ServiceValueView `json:"values"`
	// IssuedPerConsumer are the items the providing app hands each consumer
	// separately: they are not values of the service and are not listed in
	// Values. Empty for a user's service.
	IssuedPerConsumer []string     `json:"issued_per_consumer"`
	UsedBy            []ServiceUse `json:"used_by"`
	// DedicatedTo is the app that holds the service for itself ("" when
	// none): no other app can be plugged in (CAT-INST-38).
	DedicatedTo string `json:"dedicated_to"`
	// MaxConsumers is how many apps the providing app lets use the service
	// at once (its manifest's max_consumers); 0: no limit.
	MaxConsumers int `json:"max_consumers"`
	// SlotsLeft is how many more apps can be plugged in now: 0 when it is
	// held (DedicatedTo) or full; null when there is no limit. Apps deleted
	// with their data kept count (their binding waits for them).
	SlotsLeft *int `json:"slots_left"`
	// ReadySay and ReadyDetail are set when the status is not_ready (the
	// providing app's readiness check did not pass just now): the sentence
	// its manifest writes for the user, as written, and what failed
	// (secrets masked). Worked out when asked, never stored.
	ReadySay    string    `json:"ready_say,omitempty"`
	ReadyDetail string    `json:"ready_detail,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ServiceValueView is one item of a service. Value of a secret item is
// always masked; Set says whether it has a value at all.
type ServiceValueView struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
	Set    bool   `json:"set"`
	// Ask is the sentence of the providing app's manifest when the user has
	// to do a step in that app and fill the item in; "" otherwise.
	Ask string `json:"ask"`
}

// ServiceList is GET /v1/services. Unreadable names the services whose
// stored values cannot be decrypted (left out of Services); omitted when
// there are none.
type ServiceList struct {
	Services   []ServiceView `json:"services"`
	Unreadable []string      `json:"unreadable,omitempty"`
}

// PatchedService is the answer to PATCH: the service and notes for the user
// (who uses it and has to apply the settings again, a value set back to its
// default). Notes is never null.
type PatchedService struct {
	ServiceView
	Notes []string `json:"notes"`
}

// ServiceTestResult is POST /v1/services/{name}/test. OK: got through.
// Skipped: there was nothing that could be tried (no test for the kind, an
// item without a value, an app's service that cannot be reached from here);
// Detail says why. Code is CAT-SVC-06 when it was tried and did not get
// through. Target is where it connected ("" when skipped).
type ServiceTestResult struct {
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
	Code    string `json:"code,omitempty"`
	Target  string `json:"target,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

// ServiceValuesError is why the values of a request could not be read. Its
// text names an item, never a value.
type ServiceValuesError struct{ msg string }

func (e *ServiceValuesError) Error() string { return e.msg }

func valuesErr(format string, a ...any) error {
	return &ServiceValuesError{msg: fmt.Sprintf(format, a...)}
}

// BadServiceBody is CAT-SVC-02 for a request body that does not decode. It
// never quotes the body, which may hold a secret.
func BadServiceBody(err error) *Fault {
	var ve *ServiceValuesError
	if errors.As(err, &ve) {
		return fault(CodeSvcBadValue, nil, "请求体里的 values 不对：%s", ve.msg)
	}
	return fault(CodeSvcBadValue, nil, "请求体要是 JSON 对象，values 里每一项的值写成文字")
}

// ServiceValues are values as a request carries them: each one text. A
// number or a boolean written without quotes is taken as its text; null is
// "" (no value); anything else is refused.
type ServiceValues map[string]string

// UnmarshalJSON accepts text, numbers, booleans and null.
func (v *ServiceValues) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return valuesErr("values 要是 {项: 值} 的对象")
	}
	out := ServiceValues{}
	for k, r := range raw {
		r = bytes.TrimSpace(r)
		switch {
		case len(r) == 0 || string(r) == "null":
			out[k] = ""
		case r[0] == '"':
			var s string
			if err := json.Unmarshal(r, &s); err != nil {
				return valuesErr("%s 的值不是合法的文字", k)
			}
			out[k] = s
		case string(r) == "true" || string(r) == "false":
			out[k] = string(r)
		case r[0] == '-' || (r[0] >= '0' && r[0] <= '9'):
			var n json.Number
			if err := json.Unmarshal(r, &n); err != nil {
				return valuesErr("%s 的值不是合法的数字", k)
			}
			out[k] = n.String()
		default:
			return valuesErr("%s 的值要写成文字（例如 \"587\"），不能是列表或对象", k)
		}
	}
	*v = out
	return nil
}

// AddServiceRequest is POST /v1/services.
type AddServiceRequest struct {
	Name   string        `json:"name"`
	Kind   string        `json:"kind"`
	Values ServiceValues `json:"values"`
}

// PatchServiceRequest is PATCH /v1/services/{name}: only the items to change;
// "" clears one.
type PatchServiceRequest struct {
	Values ServiceValues `json:"values"`
}

// ---- what every call looks at ----

// svcEnv is what one call knows: the kinds of the catalogs in use, the apps
// and who uses which service. Manifests of providing apps are read when
// needed.
type svcEnv struct {
	e         *Engine
	reg       *Registry
	types     map[string]catalog.ServiceType
	kinds     []catalog.ServiceType
	views     []catalogView
	catErr    error
	apps      map[string]state.App
	users     map[string][]ServiceUse
	manifests map[string]*catalog.Manifest // app -> saved manifest (nil: none readable)
}

func (e *Engine) svcEnv(ctx context.Context, withUsers bool) (*svcEnv, error) {
	reg, err := e.Registry()
	if err != nil {
		return nil, fault(CodeInternal, err, "服务登记表用不了：%v", err)
	}
	env := &svcEnv{e: e, reg: reg, types: map[string]catalog.ServiceType{}, apps: map[string]state.App{}, users: map[string][]ServiceUse{}, manifests: map[string]*catalog.Manifest{}}
	if e.cfg.Catalog != nil {
		env.views, env.catErr = e.cfg.Catalog.views()
	} else {
		env.catErr = errors.New("没有应用目录")
	}
	var idxs []*catalog.Index
	for _, v := range env.views {
		idxs = append(idxs, v.idx)
	}
	env.kinds = catalog.KnownServiceTypes(idxs...)
	for _, t := range env.kinds {
		env.types[t.Kind] = t
	}
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil {
		return nil, fault(CodeInternal, err, "读不了应用列表：%v", err)
	}
	for _, a := range apps {
		env.apps[a.Name] = a
	}
	if withUsers {
		bs, err := reg.ListBindings(ctx, state.BindingFilter{})
		if err != nil {
			return nil, fault(CodeInternal, err, "读不了服务的接入记录：%v", err)
		}
		for _, b := range bs {
			_, installed := env.apps[b.App]
			env.users[b.Service] = append(env.users[b.Service], ServiceUse{App: b.App, Use: b.UseName, Gone: !installed, Dedicated: b.Dedicated})
		}
	}
	return env, nil
}

// provide returns the entry of the providing app's saved manifest the
// service comes from; false when it cannot be read or no longer matches.
func (env *svcEnv) provide(ctx context.Context, sv Service) (catalog.Provide, bool) {
	if sv.Source != state.ServiceFromApp {
		return catalog.Provide{}, false
	}
	m, seen := env.manifests[sv.App]
	if !seen {
		if cm, err := env.e.cfg.Store.GetCatalogManifest(ctx, sv.App); err == nil {
			m, _ = catalog.ParseManifest([]byte(cm.Manifest))
		}
		env.manifests[sv.App] = m
	}
	if m == nil || sv.ProvideIndex < 0 || sv.ProvideIndex >= len(m.Provides) || m.Provides[sv.ProvideIndex].Kind != sv.Kind {
		return catalog.Provide{}, false
	}
	return m.Provides[sv.ProvideIndex], true
}

// providerState is what the providing app is doing (registry rule).
func (env *svcEnv) providerState(sv Service) ProviderState {
	var app *state.App
	if a, ok := env.apps[sv.App]; ok {
		app = &a
	}
	return ProviderStateOf(sv, app)
}

// itemsOf works out the registry fields of sv, the items issued per consumer
// and the ask sentences.
func (env *svcEnv) itemsOf(ctx context.Context, sv Service) (t catalog.ServiceType, known bool, fields []ServiceField, perConsumer []string, asks map[string]string, pv catalog.Provide, havePv bool) {
	t, known = env.types[sv.Kind]
	pv, havePv = env.provide(ctx, sv)
	asks = map[string]string{}
	secretByProvider := map[string]bool{}
	if havePv {
		for _, k := range pv.ValueKeys() {
			src := pv.Values[k]
			switch {
			case src.PerConsumer():
				perConsumer = append(perConsumer, k)
			case src.Kind() == catalog.ValueAsk:
				asks[k] = src.Ask
			}
			if src.Secret {
				secretByProvider[k] = true
			}
		}
	}
	// An app's service whose saved manifest cannot be read: nothing says
	// which items the app issues per consumer or marks secret. An item
	// without a value is then not counted missing (it may be issued), and
	// every item is shown masked.
	blind := sv.Source == state.ServiceFromApp && !havePv
	for _, f := range t.Fields {
		required, secret := f.Required, f.Secret || secretByProvider[f.Key]
		if blind {
			required = required && sv.Values[f.Key] != ""
			secret = true
		}
		fields = append(fields, ServiceField{Key: f.Key, Required: required, Secret: secret})
	}
	return
}

// secretKeys are the items of fields that are secret.
func secretKeys(fields []ServiceField) map[string]bool {
	out := map[string]bool{}
	for _, f := range fields {
		if f.Secret {
			out[f.Key] = true
		}
	}
	return out
}

func (env *svcEnv) view(ctx context.Context, sv Service) ServiceView {
	t, known, fields, perConsumer, asks, pv, havePv := env.itemsOf(ctx, sv)
	h := ServiceStatusOf(sv, fields, perConsumer, env.providerState(sv))
	label := t.Label
	if !known || label == "" {
		label = sv.Kind
	}
	issued := map[string]bool{}
	for _, k := range perConsumer {
		issued[k] = true
	}
	secret := map[string]bool{}
	for _, f := range fields {
		secret[f.Key] = f.Secret
	}
	out := ServiceView{
		Name: sv.Name, Kind: sv.Kind, KindLabel: label, Source: sv.Source, App: sv.App, ServiceHealth: h,
		Values: []ServiceValueView{}, IssuedPerConsumer: append([]string{}, perConsumer...), UsedBy: append([]ServiceUse{}, env.users[sv.Name]...),
		CreatedAt: sv.CreatedAt, UpdatedAt: sv.UpdatedAt,
	}
	if havePv {
		out.MaxConsumers = pv.MaxConsumers
	}
	out.DedicatedTo, out.SlotsLeft = slots(env.users[sv.Name], out.MaxConsumers)
	listed := map[string]bool{}
	for _, f := range t.Fields {
		listed[f.Key] = true
		if issued[f.Key] {
			continue
		}
		v := sv.Values[f.Key]
		shown := v
		if secret[f.Key] {
			shown = MaskServiceSecret(v)
		}
		out.Values = append(out.Values, ServiceValueView{Key: f.Key, Label: f.Label, Value: shown, Secret: secret[f.Key], Set: v != "", Ask: asks[f.Key]})
	}
	// Values the kind does not know (the catalog no longer defines the kind,
	// or not that item): shown masked, nothing says they are harmless.
	var extra []string
	for k := range sv.Values {
		if !listed[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		out.Values = append(out.Values, ServiceValueView{Key: k, Label: k, Value: MaskServiceSecret(sv.Values[k]), Secret: true, Set: sv.Values[k] != "", Ask: asks[k]})
	}
	return out
}

// slots works out, from who is plugged into a service (users) and its
// provider's limit (max, 0: none), the app holding it for itself ("" when
// none) and how many more apps can be plugged in (nil: no limit). It is
// state.PlugRule seen from the service: held means none, else max less the
// apps plugged in (by app, whatever the need; apps deleted with their data
// kept included).
func slots(users []ServiceUse, max int) (string, *int) {
	apps := map[string]bool{}
	holder := ""
	for _, u := range users {
		apps[u.App] = true
		if u.Dedicated && holder == "" {
			holder = u.App
		}
	}
	switch {
	case holder != "":
		zero := 0
		return holder, &zero
	case max > 0:
		left := max - len(apps)
		if left < 0 {
			left = 0
		}
		return "", &left
	}
	return "", nil
}

// secretsOf are the secret values of sv in clear, to scrub them from any
// text that goes out.
func (env *svcEnv) secretsOf(ctx context.Context, sv Service) []string {
	_, _, fields, _, _, _, _ := env.itemsOf(ctx, sv)
	open := map[string]bool{}
	for _, f := range fields {
		if !f.Secret {
			open[f.Key] = true
		}
	}
	var out []string
	for k, v := range sv.Values {
		if !open[k] && v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ---- reading ----

// ServiceKinds lists the service types of the catalogs in use, by kind. It
// does not say which catalog apps could provide a kind: apps do not depend
// on each other, a missing service is registered by the user.
func (e *Engine) ServiceKinds(ctx context.Context) ([]ServiceKindView, error) {
	env, err := e.svcEnv(ctx, false)
	if err != nil {
		return nil, err
	}
	out := []ServiceKindView{}
	for _, t := range env.kinds {
		v := ServiceKindView{Kind: t.Kind, Label: t.Label, Summary: t.Summary, Fields: []ServiceFieldView{}, HasTest: t.Test != nil && (t.Test.TCP != nil || t.Test.HTTP != nil)}
		for _, f := range t.Fields {
			def, _ := f.DefaultText()
			opts := append([]string{}, f.Options...)
			v.Fields = append(v.Fields, ServiceFieldView{Key: f.Key, Label: f.Label, Type: f.Type, Required: f.Required, Secret: f.Secret, Default: def, Options: opts, Hint: f.Hint})
		}
		out = append(out, v)
	}
	return out, nil
}

// Services lists every service of this machine, by name, with who uses it.
func (e *Engine) Services(ctx context.Context) (ServiceList, error) {
	env, err := e.svcEnv(ctx, true)
	if err != nil {
		return ServiceList{}, err
	}
	rows, err := env.reg.store.ListServices(ctx, state.ServiceFilter{})
	if err != nil {
		return ServiceList{}, fault(CodeInternal, err, "读不了服务登记表：%v", err)
	}
	out := ServiceList{Services: []ServiceView{}}
	byName := map[string]Service{}
	for _, row := range rows {
		sv, err := env.reg.openService(row)
		if err != nil {
			out.Unreadable = append(out.Unreadable, row.Name)
			continue
		}
		byName[sv.Name] = sv
		out.Services = append(out.Services, env.view(ctx, sv))
	}
	env.applyReady(ctx, out.Services, byName)
	return out, nil
}

// Service shows one service; *NoServiceError when there is none.
func (e *Engine) Service(ctx context.Context, name string) (ServiceView, error) {
	env, err := e.svcEnv(ctx, true)
	if err != nil {
		return ServiceView{}, err
	}
	sv, err := env.get(ctx, name)
	if err != nil {
		return ServiceView{}, err
	}
	views := []ServiceView{env.view(ctx, sv)}
	env.applyReady(ctx, views, map[string]Service{sv.Name: sv})
	return views[0], nil
}

func (env *svcEnv) get(ctx context.Context, name string) (Service, error) {
	sv, err := env.reg.GetService(ctx, name)
	switch {
	case errors.Is(err, state.ErrNotFound):
		return Service{}, &NoServiceError{Name: name}
	case errors.Is(err, ErrServiceUnreadable):
		return Service{}, fault(CodeInternal, err, "服务 %s 存的值解不开（数据目录里的钥匙文件换过，或者记录坏了）", name)
	case err != nil:
		return Service{}, fault(CodeInternal, err, "读不了服务 %s：%v", name, err)
	}
	return sv, nil
}

// ---- checking values ----

// maxServiceValue is the most characters a value can have.
const maxServiceValue = 4096

var typeWords = map[string]string{
	catalog.FieldString: "文字", catalog.FieldInt: "整数", catalog.FieldBool: "true 或 false", catalog.FieldURL: "http:// 或 https:// 开头的网址", catalog.FieldEnum: "可选值之一",
}

func fieldName(f catalog.ServiceField) string {
	if f.Label == "" || f.Label == f.Key {
		return f.Key
	}
	return f.Label + "（" + f.Key + "）"
}

// cleanServiceValue checks a non-empty value against its item and returns
// the form stored; the error says what is wrong in the user's words.
func cleanServiceValue(f catalog.ServiceField, v string) (string, error) {
	if !f.Secret {
		v = strings.TrimSpace(v)
	}
	if !utf8.ValidString(v) {
		return "", fmt.Errorf("%s 的值里有不是文字的字节", fieldName(f))
	}
	if n := utf8.RuneCountInString(v); n > maxServiceValue {
		return "", fmt.Errorf("%s 的值太长（%d 个字，最多 %d）", fieldName(f), n, maxServiceValue)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%s 的值里有换行或控制字符：只能是一行", fieldName(f))
		}
	}
	switch f.Type {
	case catalog.FieldInt:
		n, err := strconv.Atoi(v)
		if err != nil {
			return "", fmt.Errorf("%s 要是整数，写的是 %q", fieldName(f), safeShow(f, v))
		}
		return strconv.Itoa(n), nil
	case catalog.FieldBool:
		switch strings.ToLower(v) {
		case "true", "yes", "on", "1":
			return "true", nil
		case "false", "no", "off", "0":
			return "false", nil
		}
		return "", fmt.Errorf("%s 要写 true 或 false，写的是 %q", fieldName(f), safeShow(f, v))
	case catalog.FieldURL:
		u, err := neturl.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", fmt.Errorf("%s 要是 http:// 或 https:// 开头的网址，写的是 %q", fieldName(f), safeShow(f, v))
		}
		return v, nil
	case catalog.FieldEnum:
		for _, o := range f.Options {
			if o == v {
				return v, nil
			}
		}
		return "", fmt.Errorf("%s 只能是 %s 之一，写的是 %q", fieldName(f), strings.Join(f.Options, "、"), safeShow(f, v))
	}
	return v, nil
}

// safeShow is a value fit to quote back in an error.
func safeShow(f catalog.ServiceField, v string) string {
	if f.Secret {
		return MaskServiceSecret(v)
	}
	return v
}

func badValues(problems []string) *Fault {
	return fault(CodeSvcBadValue, nil, "%s", strings.Join(problems, "；"))
}

func unknownItems(t catalog.ServiceType, keys []string) string {
	sort.Strings(keys)
	return fmt.Sprintf("%s 没有这些项：%s（有：%s）", kindName(t), strings.Join(keys, "、"), strings.Join(t.FieldKeys(), "、"))
}

func kindName(t catalog.ServiceType) string {
	if t.Label == "" {
		return t.Kind
	}
	return t.Label + "（" + t.Kind + "）"
}

// valuesForNew checks the values of a new service of kind t and fills in the
// defaults.
func valuesForNew(t catalog.ServiceType, in map[string]string) (map[string]string, error) {
	var unknown, problems, missing []string
	for k := range in {
		if _, ok := t.Field(k); !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		problems = append(problems, unknownItems(t, unknown))
	}
	out := map[string]string{}
	for _, f := range t.Fields {
		v := in[f.Key]
		if !f.Secret {
			v = strings.TrimSpace(v)
		}
		if v == "" {
			if def, ok := f.DefaultText(); ok && def != "" {
				out[f.Key] = def
			} else if f.Required {
				missing = append(missing, fieldName(f))
			}
			continue
		}
		c, err := cleanServiceValue(f, v)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		out[f.Key] = c
	}
	if len(missing) > 0 {
		problems = append(problems, "还差必填项："+strings.Join(missing, "、"))
	}
	if len(problems) > 0 {
		return nil, badValues(problems)
	}
	return out, nil
}

// ---- addresses a container cannot reach ----

// renderTest fills a service type's test template ({{host}}: an item of
// the type, read by catalog.FillFields) from values; missing lists the items
// it refers to that have no value.
func renderTest(tpl string, values map[string]string) (out string, refs, missing []string) {
	out = catalog.FillFields(tpl, func(k string) string {
		refs = append(refs, k)
		v := values[k]
		if v == "" {
			missing = append(missing, k)
		}
		return v
	})
	return out, refs, missing
}

// loopbackNames are the names resolvers and container images commonly map
// to this machine (besides localhost and *.localhost).
var loopbackNames = map[string]bool{
	"localhost.localdomain": true, "localhost6": true, "localhost6.localdomain6": true,
	"ip6-localhost": true, "ip6-loopback": true,
}

// loopbackHost reports whether host is this machine as seen from inside a
// container: a loopback address, localhost, or the unspecified address,
// including the shorthand numeric forms resolvers accept ("127.1",
// "0x7f000001", "2130706433", "0177.0.0.1", "0").
func loopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || loopbackNames[h] {
		return true
	}
	if i := strings.IndexByte(h, '%'); i >= 0 { // fe80::1%eth0
		h = h[:i]
	}
	ip := net.ParseIP(h)
	if ip == nil {
		ip = inetAton(h)
	}
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// inetAton reads h the way the C library's inet_aton does: one to four
// parts, each decimal, 0x hexadecimal or 0 octal, the last one filling the
// bytes left. nil when h is not such a number.
func inetAton(h string) net.IP {
	parts := strings.Split(h, ".")
	if len(parts) > 4 {
		return nil
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		base := 10
		switch {
		case len(p) > 2 && (p[:2] == "0x" || p[:2] == "0X"):
			base, p = 16, p[2:]
		case len(p) > 1 && p[0] == '0':
			base, p = 8, p[1:]
		}
		n, err := strconv.ParseUint(p, base, 32)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	var v uint64
	for i, n := range nums[:len(nums)-1] {
		if n > 255 {
			return nil
		}
		v |= n << (24 - 8*uint(i))
	}
	last := nums[len(nums)-1]
	if bits := 8 * uint(4-len(nums)+1); bits < 32 && last >= 1<<bits {
		return nil
	}
	v |= last
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// addrHost returns the host of an address as a user may write it: a bare
// host, host:port, [v6] or [v6]:port, or a URL written into a host item.
func addrHost(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.Contains(addr, "://") {
		if u, err := neturl.Parse(addr); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		return addr[1 : len(addr)-1]
	}
	return addr
}

// loopbackCheck refuses values whose address is this machine's loopback:
// the items of type url, and the host the kind's test connects to.
func loopbackCheck(t catalog.ServiceType, values map[string]string) error {
	type hit struct{ items, host string }
	var hits []hit
	labels := func(keys []string) string {
		var out []string
		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				continue
			}
			seen[k] = true
			if f, ok := t.Field(k); ok {
				out = append(out, fieldName(f))
			} else {
				out = append(out, k)
			}
		}
		return strings.Join(out, "、")
	}
	byItem := map[string]bool{}
	for _, f := range t.Fields {
		if f.Type != catalog.FieldURL || values[f.Key] == "" {
			continue
		}
		if u, err := neturl.Parse(values[f.Key]); err == nil && loopbackHost(u.Hostname()) {
			hits = append(hits, hit{fieldName(f), u.Hostname()})
			byItem[f.Key] = true
		}
	}
	if t.Test != nil {
		if tcp := t.Test.TCP; tcp != nil {
			if host, refs, missing := renderTest(tcp.Host, values); len(missing) == 0 && host != "" && loopbackHost(addrHost(host)) && !anyOf(refs, byItem) {
				hits = append(hits, hit{labels(refs), addrHost(host)})
			}
		}
		if hp := t.Test.HTTP; hp != nil {
			if raw, refs, missing := renderTest(hp.URL, values); len(missing) == 0 && !anyOf(refs, byItem) {
				if u, err := neturl.Parse(raw); err == nil && u.Hostname() != "" && loopbackHost(u.Hostname()) {
					hits = append(hits, hit{labels(refs), u.Hostname()})
				}
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	var parts []string
	for _, h := range hits {
		parts = append(parts, fmt.Sprintf("%s 写的是 %s", h.items, h.host))
	}
	return fault(CodeSvcLoopback, nil, "%s：这是本机回环地址，应用在容器里连不到这个地址", strings.Join(parts, "；"))
}

func anyOf(keys []string, set map[string]bool) bool {
	for _, k := range keys {
		if set[k] {
			return true
		}
	}
	return false
}

// ---- writing ----

// AddService registers a service of the user's.
func (e *Engine) AddService(ctx context.Context, req AddServiceRequest) (ServiceView, error) {
	env, err := e.svcEnv(ctx, true)
	if err != nil {
		return ServiceView{}, err
	}
	name, kind := strings.TrimSpace(req.Name), strings.TrimSpace(req.Kind)
	if err := checkServiceName(name); err != nil {
		return ServiceView{}, err
	}
	if kind == "" {
		return ServiceView{}, fault(CodeSvcBadValue, nil, "要写服务类型（acornfox services kinds 看有哪些）")
	}
	t, ok := env.types[kind]
	if !ok {
		if env.catErr != nil && len(env.kinds) == 0 {
			return ServiceView{}, fault(CodeSvcUnknownKind, env.catErr, "应用目录读不了，不知道有哪些服务类型")
		}
		return ServiceView{}, fault(CodeSvcUnknownKind, nil, "目录里没有 %s 这种服务类型（有：%s）", kind, strings.Join(kindNames(env.kinds), "、"))
	}
	if _, err := env.reg.GetService(ctx, name); err == nil {
		return ServiceView{}, fault(CodeSvcNameTaken, nil, "这台机器上已经有叫 %s 的服务了", name)
	}
	values, err := valuesForNew(t, req.Values)
	if err != nil {
		return ServiceView{}, err
	}
	if err := loopbackCheck(t, values); err != nil {
		return ServiceView{}, err
	}
	sv, err := env.reg.CreateService(ctx, Service{Name: name, Kind: kind, Source: state.ServiceFromUser, Values: values})
	switch {
	case errors.Is(err, state.ErrServiceNameTaken):
		return ServiceView{}, fault(CodeSvcNameTaken, nil, "这台机器上已经有叫 %s 的服务了", name)
	case errors.Is(err, state.ErrInvalid):
		return ServiceView{}, fault(CodeSvcBadValue, nil, "登记不了：%s", strings.TrimPrefix(err.Error(), state.ErrInvalid.Error()+": "))
	case err != nil:
		return ServiceView{}, fault(CodeInternal, err, "写不进服务登记表：%v", err)
	}
	return env.view(ctx, sv), nil
}

func kindNames(ts []catalog.ServiceType) []string {
	out := []string{}
	for _, t := range ts {
		out = append(out, t.Kind)
	}
	if len(out) == 0 {
		out = append(out, "目录里一种都没有")
	}
	return out
}

// checkServiceName applies the registry's rule for names.
func checkServiceName(name string) error {
	switch {
	case name == "":
		return fault(CodeSvcBadValue, nil, "要给服务起个名字（--name 名字）")
	case state.ReservedServiceName(name):
		return fault(CodeSvcBadValue, nil, "%s 是保留的词，不能当服务名，换一个", name)
	case !state.ValidServiceName(name):
		return fault(CodeSvcBadValue, nil, "名字 %q 不行：只能用文字和数字，第二个字起也可以用 - _ .；不能有空格，最多 %d 个字", name, state.ServiceNameMaxLen)
	}
	return nil
}

// PatchService changes items of a service: only the given ones, "" clears
// one. A user's service: any item of its kind (a required one cannot be
// cleared; one with a default goes back to it). An app's service: only its
// ask items.
func (e *Engine) PatchService(ctx context.Context, name string, req PatchServiceRequest) (PatchedService, error) {
	env, err := e.svcEnv(ctx, true)
	if err != nil {
		return PatchedService{}, err
	}
	sv, err := env.get(ctx, name)
	if err != nil {
		return PatchedService{}, err
	}
	if len(req.Values) == 0 {
		return PatchedService{}, fault(CodeSvcBadValue, nil, "要改哪一项？values 里至少写一项（项=值，空值表示清掉）")
	}
	t, known, fields, _, asks, pv, havePv := env.itemsOf(ctx, sv)
	// What is secret is the kind's flag or the providing app's (an app may
	// mark an item secret the kind does not): both decide how a value is
	// quoted back.
	secret := secretKeys(fields)
	fromApp := sv.Source == state.ServiceFromApp
	if fromApp {
		if env.providerState(sv) == ProviderGone {
			return PatchedService{}, fault(CodeSvcFromApp, nil, "提供 %s 的应用 %s 已经删了，这条服务不能再改；用不上了就 acornfox services remove %s", name, sv.App, name)
		}
		if !havePv {
			return PatchedService{}, fault(CodeSvcFromApp, nil, "读不到应用 %s 保存的清单，不知道 %s 哪几项要你填，这条服务现在改不了", sv.App, name)
		}
	} else if !known {
		return PatchedService{}, fault(CodeSvcUnknownKind, nil, "%s 的类型 %s 现在的目录里没有定义，改不了（acornfox catalog sync 后再试）", name, sv.Kind)
	}
	set, clear := map[string]string{}, map[string]bool{}
	var notes, problems, unknown, locked []string
	keys := make([]string, 0, len(req.Values))
	for k := range req.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := req.Values[k]
		f, isField := t.Field(k)
		if !isField {
			f = catalog.ServiceField{Key: k, Label: k, Type: catalog.FieldString}
		}
		f.Secret = f.Secret || secret[k] || (havePv && pv.Values[k].Secret)
		if fromApp {
			if _, ask := asks[k]; !ask {
				if isField || env.pvHas(ctx, sv, k) {
					locked = append(locked, fieldName(f))
				} else {
					unknown = append(unknown, k)
				}
				continue
			}
		} else if !isField {
			unknown = append(unknown, k)
			continue
		}
		if !f.Secret {
			v = strings.TrimSpace(v)
		}
		if v == "" {
			def, hasDef := f.DefaultText()
			switch {
			case !fromApp && f.Required:
				problems = append(problems, fieldName(f)+" 是必填项，不能清掉")
			case !fromApp && hasDef && def != "":
				set[k] = def
				notes = append(notes, fmt.Sprintf("%s 清掉后回到默认值 %s", fieldName(f), def))
			default:
				clear[k] = true
			}
			continue
		}
		c, err := cleanServiceValue(f, v)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		set[k] = c
	}
	if len(locked) > 0 {
		can := make([]string, 0, len(asks))
		for k := range asks {
			can = append(can, k)
		}
		sort.Strings(can)
		what := "这条服务没有要你填的项"
		if len(can) > 0 {
			what = "只有要你在应用里做一步再填进来的这几项能填：" + strings.Join(can, "、")
		}
		return PatchedService{}, fault(CodeSvcFromApp, nil, "%s 由应用 %s 提供，%s 由应用算出来或者按使用方单独发，不能改；%s", name, sv.App, strings.Join(locked, "、"), what)
	}
	if len(unknown) > 0 {
		if known {
			problems = append([]string{unknownItems(t, unknown)}, problems...)
		} else {
			problems = append([]string{"没有这些项：" + strings.Join(unknown, "、")}, problems...)
		}
	}
	if len(problems) > 0 {
		return PatchedService{}, badValues(problems)
	}
	merged := map[string]string{}
	for k, v := range sv.Values {
		merged[k] = v
	}
	apply := func(vals map[string]string) {
		for k, v := range set {
			vals[k] = v
		}
		for k := range clear {
			delete(vals, k)
		}
	}
	apply(merged)
	if known {
		if err := loopbackCheck(t, merged); err != nil {
			return PatchedService{}, err
		}
	}
	updated, err := env.reg.UpdateService(ctx, name, func(s *Service) error {
		if s.Values == nil {
			s.Values = map[string]string{}
		}
		apply(s.Values)
		return nil
	})
	switch {
	case errors.Is(err, state.ErrNotFound):
		return PatchedService{}, &NoServiceError{Name: name}
	case errors.Is(err, state.ErrInvalid):
		return PatchedService{}, fault(CodeSvcBadValue, nil, "改不了：%s", strings.TrimPrefix(err.Error(), state.ErrInvalid.Error()+": "))
	case err != nil:
		return PatchedService{}, fault(CodeInternal, err, "写不进服务登记表：%v", err)
	}
	if apps := distinctApps(env.users[name]); len(apps) > 0 {
		notes = append([]string{fmt.Sprintf("有 %d 个应用在用它，重新应用设置后才生效：%s", len(apps), strings.Join(apps, "、"))}, notes...)
	}
	if notes == nil {
		notes = []string{}
	}
	return PatchedService{ServiceView: env.view(ctx, updated), Notes: notes}, nil
}

// pvHas reports whether the providing app's manifest gives item k a value.
func (env *svcEnv) pvHas(ctx context.Context, sv Service, k string) bool {
	pv, ok := env.provide(ctx, sv)
	if !ok {
		return false
	}
	_, has := pv.Values[k]
	return has
}

func distinctApps(us []ServiceUse) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range us {
		if !seen[u.App] {
			seen[u.App] = true
			out = append(out, u.App)
		}
	}
	sort.Strings(out)
	return out
}

func usesText(us []ServiceUse) string {
	var parts []string
	for _, u := range us {
		parts = append(parts, u.App+"（"+u.Use+"）")
	}
	return strings.Join(parts, "、")
}

// RemovedService is what DELETE /v1/services/{name} says beyond "done".
type RemovedService struct {
	Name  string   `json:"name"`
	Notes []string `json:"notes"`
}

// RemoveService deletes a service: a user's one, or one whose providing app
// is gone. Refused for the service of an installed app (CAT-SVC-05) and
// while installed apps use it (CAT-SVC-04). Apps deleted with their data
// kept do not keep it: their bindings to it go with it, and the notes say
// that those apps have to be connected to a service anew when installed
// again. What was issued to them is not taken back: a user's service issues
// nothing, and an app's service is only removed here once its provider is
// gone.
func (e *Engine) RemoveService(ctx context.Context, name string) (RemovedService, error) {
	env, err := e.svcEnv(ctx, true)
	if err != nil {
		return RemovedService{}, err
	}
	sv, err := env.get(ctx, name)
	if err != nil {
		return RemovedService{}, err
	}
	if sv.Source == state.ServiceFromApp && env.providerState(sv) != ProviderGone {
		return RemovedService{}, fault(CodeSvcFromApp, nil, "%s 是应用 %s 提供的服务，不能单独删：删掉那个应用它才会消失（acornfox delete %s）", name, sv.App, sv.App)
	}
	var installed, gone []ServiceUse
	for _, u := range env.users[name] {
		if u.Gone {
			gone = append(gone, u)
		} else {
			installed = append(installed, u)
		}
	}
	if len(installed) > 0 {
		return RemovedService{}, &ServiceInUseFault{Fault: fault(CodeSvcInUse, nil, "还有应用在用 %s，不能删：%s", name, usesText(installed)), UsedBy: installed}
	}
	// The bindings of apps that are gone go first: the registry refuses to
	// remove a service anything is plugged into. An app installed meanwhile
	// keeps its binding, and the removal below is refused for it.
	//
	// The provider of a service still here is gone with its data kept (the
	// only way an app's service outlives it): what it issued to those apps
	// is still in that data, and nothing takes it back once the binding
	// goes, so the user hears what to remove there (CAT-INST-35).
	var dropped, kept []string
	for _, u := range gone {
		if sv.Source == state.ServiceFromApp {
			if b, err := env.reg.GetBinding(ctx, u.App, u.Use); err == nil && len(b.Issued) > 0 {
				kept = append(kept, keptDataWarning(u.App, name, sv.App, b.Issued))
			}
		}
		if _, err := env.reg.DeleteBinding(ctx, u.App, u.Use); err != nil {
			return RemovedService{}, fault(CodeInternal, err, "删不了 %s 接 %s 的记录：%v", u.App, name, err)
		}
		dropped = append(dropped, u.App)
	}
	err = env.reg.DeleteService(ctx, name)
	var inUse *state.ServiceInUseError
	switch {
	case errors.As(err, &inUse):
		var us []ServiceUse
		for _, u := range inUse.Users {
			us = append(us, ServiceUse{App: u.App, Use: u.UseName})
		}
		return RemovedService{}, &ServiceInUseFault{Fault: fault(CodeSvcInUse, nil, "还有应用在用 %s，不能删：%s", name, usesText(us)), UsedBy: us}
	case errors.Is(err, state.ErrNotFound):
		return RemovedService{}, &NoServiceError{Name: name}
	case err != nil:
		return RemovedService{}, fault(CodeInternal, err, "删不了服务 %s：%v", name, err)
	}
	out := RemovedService{Name: name, Notes: []string{}}
	if len(dropped) > 0 {
		out.Notes = append(out.Notes, goneConsumersNote(name, dropped))
	}
	out.Notes = append(out.Notes, kept...)
	return out, nil
}

// ---- trying ----

// serviceTestTimeout is how long one try may take (a variable for tests).
var serviceTestTimeout = 5 * time.Second

// TestService tries a service the way its kind's test says.
func (e *Engine) TestService(ctx context.Context, name string) (ServiceTestResult, error) {
	env, err := e.svcEnv(ctx, true)
	if err != nil {
		return ServiceTestResult{}, err
	}
	sv, err := env.get(ctx, name)
	if err != nil {
		return ServiceTestResult{}, err
	}
	res, used := e.tryService(ctx, env, sv)
	secrets := append(env.secretsOf(ctx, sv), used...)
	res.Detail, res.Target = scrub(res.Detail, secrets), scrub(res.Target, secrets)
	return res, nil
}

func skipped(format string, a ...any) ServiceTestResult {
	return ServiceTestResult{Skipped: true, Detail: fmt.Sprintf(format, a...)}
}

// scrub masks secrets that slipped into a text. Values shorter than 4
// characters are left alone: replacing them would garble the text (a "25"
// in every port) while masking nothing worth hiding.
func scrub(s string, secrets []string) string {
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, v := range secrets {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, MaskServiceSecret(v))
		}
	}
	return s
}

// tryService tries sv; used are the values taken from elsewhere (what a
// consumer was issued), to scrub from the answer like the service's own
// secrets.
func (e *Engine) tryService(ctx context.Context, env *svcEnv, sv Service) (res ServiceTestResult, used []string) {
	t, known, _, perConsumer, _, pv, havePv := env.itemsOf(ctx, sv)
	if !known {
		return skipped("现在的目录里没有 %s 这种服务类型的定义，不知道怎么试", sv.Kind), nil
	}
	if t.Test == nil || (t.Test.TCP == nil && t.Test.HTTP == nil) {
		return skipped("%s 这种服务类型没有试连的办法", kindName(t)), nil
	}
	values := map[string]string{}
	for _, f := range t.Fields {
		if def, ok := f.DefaultText(); ok {
			values[f.Key] = def
		}
	}
	for k, v := range sv.Values {
		if v != "" {
			values[k] = v
		}
	}
	var via *hostRoute
	if sv.Source == state.ServiceFromApp {
		switch env.providerState(sv) {
		case ProviderGone:
			return skipped("这条服务由应用 %s 提供，那个应用已经删了", sv.App), nil
		case ProviderStopped:
			return skipped("这条服务由应用 %s 提供，那个应用停着：请在应用详情里看它的状态（acornfox status %s）", sv.App, sv.App), nil
		}
		// What the provider issues per consumer: take what the first
		// consumer got, when there is one.
		if havePv {
			var first *Binding
			for _, u := range env.users[sv.Name] {
				if b, err := env.reg.GetBinding(ctx, u.App, u.Use); err == nil {
					first = &b
					break
				}
			}
			if first != nil {
				got := e.consumerItems(ctx, sv, pv, first.Issued)
				for _, k := range perConsumer {
					if v := got[k]; v != "" {
						values[k] = v
						used = append(used, v)
					}
				}
			}
		}
		r, why := e.routeToProvider(ctx, env, sv)
		if r == nil {
			return skipped("这条服务由应用 %s 提供，%s：请在应用详情里看它的状态（acornfox status %s）", sv.App, why, sv.App), used
		}
		via = r
	}
	label := func(keys []string) string {
		var out []string
		for _, k := range keys {
			if f, ok := t.Field(k); ok {
				out = append(out, fieldName(f))
			} else {
				out = append(out, k)
			}
		}
		return strings.Join(out, "、")
	}
	issued := map[string]bool{}
	for _, k := range perConsumer {
		issued[k] = true
	}
	missingWhy := func(missing []string) ServiceTestResult {
		for _, k := range missing {
			if issued[k] {
				return skipped("%s 是应用 %s 给每个使用它的应用单独发的，现在还没有应用在用它，没法从这里试：请在应用详情里看它的状态（acornfox status %s）", label([]string{k}), sv.App, sv.App)
			}
		}
		return skipped("%s 还没有值，填上再试（acornfox services set %s 项=值）", label(missing), sv.Name)
	}
	if tcp := t.Test.TCP; tcp != nil {
		host, _, m1 := renderTest(tcp.Host, values)
		port, _, m2 := renderTest(tcp.Port, values)
		if missing := append(m1, m2...); len(missing) > 0 {
			return missingWhy(missing), used
		}
		host = addrHost(host)
		p, err := strconv.Atoi(strings.TrimSpace(port))
		if err != nil || p < 1 || p > 65535 {
			return ServiceTestResult{Code: CodeSvcTestFailed, Detail: fmt.Sprintf("端口 %q 不是 1～65535 的数字，没法连", port)}, used
		}
		return tryTCP(ctx, host, p, via), used
	}
	hp := t.Test.HTTP
	raw, _, missing := renderTest(hp.URL, values)
	bearer, _, m2 := renderTest(hp.Bearer, values)
	if missing = append(missing, m2...); len(missing) > 0 {
		return missingWhy(missing), used
	}
	return e.tryHTTP(ctx, raw, bearer, hp.ExpectStatus, via), used
}

// hostRoute is how this machine reaches an app's container: the name and
// port other containers use, and the loopback port it is published on here.
type hostRoute struct {
	app      string
	name     string // the container's name on the app network (the app's name)
	port     int    // the container port
	hostPort int    // published on 127.0.0.1
	ip       string // the container's address on its app network, when it publishes no port (an internal app)
}

// map returns the address to connect to for host:port, and whether it went
// to the provider's container: its published port (only that one port is
// published), or, for an internal app that publishes none, its address on
// the app network, where every port of the container is reachable (a
// service may be on another endpoint than the primary one).
func (r *hostRoute) mapAddr(host string, port int) (string, bool) {
	if r != nil && strings.EqualFold(host, r.name) {
		if r.hostPort <= 0 && r.ip != "" && port > 0 {
			return net.JoinHostPort(r.ip, strconv.Itoa(port)), true
		}
		if r.hostPort > 0 && port == r.port {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(r.hostPort)), true
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), false
}

// note says how the address for the container port port was reached.
func (r *hostRoute) note(port int) string {
	if r.hostPort <= 0 {
		return fmt.Sprintf("（应用 %s 只在内部网络，不映射到这台机器的端口；从这台机器连的是它在内部网络里的地址 %s，别的容器里的地址是 %s:%d）", r.app, net.JoinHostPort(r.ip, strconv.Itoa(port)), r.name, port)
	}
	return fmt.Sprintf("（应用 %s 的 %d 端口在这台机器上映射到 127.0.0.1:%d；容器里的地址是 %s:%d）", r.app, r.port, r.hostPort, r.name, r.port)
}

// routeToProvider finds the running container of the providing app and the
// port it publishes on this machine; nil with the reason when there is none.
func (e *Engine) routeToProvider(ctx context.Context, env *svcEnv, sv Service) (*hostRoute, string) {
	a, ok := env.apps[sv.App]
	if !ok {
		return nil, "那个应用已经删了"
	}
	if a.CurrentDeployment == "" {
		return nil, "那个应用还没有上线的版本"
	}
	cs, err := e.cfg.Runner.ListContainers(ctx, a.Name)
	if err != nil {
		return nil, "查不到它的容器"
	}
	for _, c := range cs {
		if c.DeploymentID != a.CurrentDeployment || !c.Running {
			continue
		}
		if c.HostPort <= 0 {
			// An internal app publishes no port: this machine reaches it at
			// its address on the app network (runner.ProbeAddr).
			if a.Internal && c.IP != "" && a.Port > 0 {
				return &hostRoute{app: a.Name, name: a.Name, port: a.Port, ip: c.IP}, ""
			}
			continue
		}
		port := c.Port
		if port == 0 {
			port = a.Port
		}
		return &hostRoute{app: a.Name, name: a.Name, port: port, hostPort: c.HostPort}, ""
	}
	return nil, "它的容器现在没在运行"
}

func tryTCP(ctx context.Context, host string, port int, via *hostRoute) ServiceTestResult {
	addr, mapped := via.mapAddr(host, port)
	shown := net.JoinHostPort(host, strconv.Itoa(port))
	if via != nil && !mapped {
		return skipped("这条服务由应用 %s 提供，地址 %s 不是它在容器网络里的入口，从这台机器上试不了：请在应用详情里看它的状态（acornfox status %s）", via.app, shown, via.app)
	}
	extra := ""
	if mapped {
		shown, extra = addr, via.note(port)
	}
	d := net.Dialer{Timeout: serviceTestTimeout}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return ServiceTestResult{Code: CodeSvcTestFailed, Target: shown, Detail: fmt.Sprintf("连不上 %s：%s%s", shown, netReason(err), extra)}
	}
	_ = c.Close()
	return ServiceTestResult{OK: true, Target: shown, Detail: fmt.Sprintf("连上了 %s%s", shown, extra)}
}

func (e *Engine) tryHTTP(ctx context.Context, raw, bearer string, expect []int, via *hostRoute) ServiceTestResult {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ServiceTestResult{Code: CodeSvcTestFailed, Detail: "要试的地址不是 http:// 或 https:// 开头的网址，没法试"}
	}
	shown := showURL(u)
	extra := ""
	if via != nil {
		port := 80
		if u.Scheme == "https" {
			port = 443
		}
		if p := u.Port(); p != "" {
			port, _ = strconv.Atoi(p)
		}
		addr, mapped := via.mapAddr(u.Hostname(), port)
		if !mapped {
			return skipped("这条服务由应用 %s 提供，地址 %s 不是它在容器网络里的入口，从这台机器上试不了：请在应用详情里看它的状态（acornfox status %s）", via.app, shown, via.app)
		}
		u.Host = addr
		shown, extra = showURL(u), via.note(port)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ServiceTestResult{Code: CodeSvcTestFailed, Target: shown, Detail: "要试的地址不对，没法试"}
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: serviceTestTimeout}).DialContext
	if via != nil {
		tr.Proxy = nil // the provider's port is on this machine
	}
	client := &http.Client{Timeout: serviceTestTimeout, Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return ServiceTestResult{Code: CodeSvcTestFailed, Target: shown, Detail: fmt.Sprintf("GET %s 没有回应：%s%s", shown, netReason(err), extra)}
	}
	_ = resp.Body.Close()
	tr.CloseIdleConnections()
	ok := false
	if len(expect) == 0 {
		ok = resp.StatusCode >= 200 && resp.StatusCode < 300
	}
	for _, s := range expect {
		ok = ok || s == resp.StatusCode
	}
	if ok {
		return ServiceTestResult{OK: true, Target: shown, Detail: fmt.Sprintf("GET %s 回了 %d%s", shown, resp.StatusCode, extra)}
	}
	want := "2xx"
	if len(expect) > 0 {
		var ws []string
		for _, s := range expect {
			ws = append(ws, strconv.Itoa(s))
		}
		want = strings.Join(ws, " 或 ")
	}
	return ServiceTestResult{Code: CodeSvcTestFailed, Target: shown, Detail: fmt.Sprintf("GET %s 回了 %d，要的是 %s%s", shown, resp.StatusCode, want, extra)}
}

// waitText is a wait in words.
func waitText(d time.Duration) string {
	if d < time.Second {
		return "不到 1 秒"
	}
	return strconv.Itoa(int(d/time.Second)) + " 秒"
}

// showURL is a URL fit to show: no user, password or query.
func showURL(u *neturl.URL) string {
	c := *u
	c.User, c.RawQuery, c.Fragment = nil, "", ""
	return c.String()
}

// netReason says in words why a connection failed. It never quotes the
// request (which may carry a key), only the cause.
func netReason(err error) string {
	var ue *neturl.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var ne net.Error
	switch {
	case errors.As(err, &dnsErr):
		return "找不到这个地址（域名解析不出来）"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "拒绝连接（那台机器上这个端口没有程序在听，或者被挡住了）"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "网络不通（到不了那台机器）"
	case errors.As(err, &certErr):
		return "证书校验没过（证书过期、自签名或者和域名对不上）"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "超时：" + waitText(serviceTestTimeout) + "内没有回应（地址不对、网络不通，或者被防火墙挡了）"
	}
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Err != nil {
		return "连不上（" + oe.Err.Error() + "）"
	}
	return "连不上（" + err.Error() + "）"
}

// consumerItems are the items sv's provider handed one consumer, worked out
// from the credential it was issued (providerConn.perConsumer): an item
// written { issued: k } and a template that reads issued.<k>. A template is
// left out when the provider's manifest or settings cannot be read.
func (e *Engine) consumerItems(ctx context.Context, sv Service, pv catalog.Provide, issued map[string]string) map[string]string {
	pc := providerConn{pv: pv, app: state.App{Name: sv.App}}
	if m := e.appManifest(ctx, sv.App); m != nil {
		if saved, err := e.savedParams(ctx, sv.App); err == nil {
			pc.m, pc.vals = m, Values(saved)
		}
	}
	return pc.perConsumer(issued)
}
