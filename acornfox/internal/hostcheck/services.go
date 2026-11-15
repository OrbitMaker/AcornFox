package hostcheck

// The services check shared by `acornfox doctor` and the console's runtime
// page (fault CAT-SVC-08): every service an installed app is plugged into
// should be usable. A service that is not (the app providing it is stopped
// or deleted, a required item has no value) gets a warning naming the apps
// that use it, the need they plug in with, the service's state in the user's
// words and what to do. It reads the service registry as acornfox-server
// shows it (GET /v1/services); nothing is changed and no service is
// contacted (trying one is `acornfox services test`).
//
// Only a warning: the apps keep running; what breaks is what they do with
// the service (no model calls, no mail), and only the user knows whether the
// provider was stopped on purpose.
//
// A service whose providing app's readiness check does not pass now (status
// not_ready: the provider runs, but the service cannot really be used yet)
// gets a warning of its own (CAT-SVC-09) with the provider's sentence,
// whether an app uses it yet or not: an app installed later would be
// plugged into it too. The check is run by acornfox-server when the list is
// asked for (GET /v1/services); nothing is stored.

import (
	"fmt"
	"strings"
)

// CodeServiceUnusable is the fault code of the services check.
const CodeServiceUnusable = "CAT-SVC-08"

// CodeServiceNotReady is the fault code of a service whose providing app's
// readiness check does not pass now.
const CodeServiceNotReady = "CAT-SVC-09"

// Service states as the registry reports them (the "status" of GET
// /v1/services).
const (
	ServiceStatusOK              = "ok"
	ServiceStatusIncomplete      = "incomplete"
	ServiceStatusProviderStopped = "provider_stopped"
	ServiceStatusProviderGone    = "provider_gone"
	ServiceStatusNotReady        = "not_ready"
)

// ServiceInUse is one service of the registry with the apps plugged into it.
type ServiceInUse struct {
	Name        string
	KindLabel   string // the service type in the user's words ("" falls back to Kind)
	Kind        string
	Status      string // ok | incomplete | provider_stopped | provider_gone (others are reported as they are)
	StatusLabel string // Status in the user's words
	Provider    string // the app providing it ("" for a service the user registered)
	Missing     []string
	Users       []ServiceUser
	// ReadySay is the providing app's sentence when Status is not_ready.
	ReadySay string
}

// ServiceUser is one need of an app plugged into the service.
type ServiceUser struct {
	App string
	Use string
}

// CheckAppServices adds the services check. installed reports whether an
// app is installed now: plugs of apps deleted with their data kept are kept
// for when they come back and are not reported. unreadable are services
// whose stored values cannot be decrypted (the registry leaves them out).
func CheckAppServices(r Reporter, svcs []ServiceInUse, unreadable []string, installed func(app string) bool) {
	const code = CodeServiceUnusable
	if installed == nil {
		installed = func(string) bool { return true }
	}
	apps := map[string]bool{}
	bad := 0
	for _, sv := range svcs {
		if sv.Status == ServiceStatusNotReady {
			bad++
			r.Warn(CodeServiceNotReady, "%s", notReadyProblem(sv, installed))
			for _, u := range sv.Users {
				if installed(u.App) {
					apps[u.App] = true
				}
			}
			continue
		}
		var users []ServiceUser
		for _, u := range sv.Users {
			if installed(u.App) {
				users = append(users, u)
				apps[u.App] = true
			}
		}
		if len(users) == 0 || sv.Status == ServiceStatusOK {
			continue
		}
		bad++
		r.Warn(code, "%s", serviceProblem(sv, users))
	}
	if len(unreadable) > 0 {
		bad++
		r.Warn(code, "应用用的服务：%s 存的值解不开（数据目录里的加密钥匙换过或丢了），用到它们的应用重装时接不上。先不要删，把这一行交给维护的人看", strings.Join(unreadable, "、"))
	}
	if bad > 0 {
		return
	}
	if len(apps) == 0 {
		r.Add(true, code, "应用用的服务：没有应用接着服务")
		return
	}
	r.Add(true, code, "应用用的服务：%d 个应用接着的服务都可用", len(apps))
}

// notReadyProblem is the warning for one service that is not ready.
func notReadyProblem(sv ServiceInUse, installed func(string) bool) string {
	label := sv.KindLabel
	if label == "" {
		label = sv.Kind
	}
	say := strings.TrimRight(strings.TrimSpace(sv.ReadySay), "。.；; ")
	if say == "" {
		say = "提供它的应用的就绪检查没通过"
	}
	var who []string
	for _, u := range sv.Users {
		if installed(u.App) {
			who = append(who, fmt.Sprintf("应用 %s 的需要 %s", u.App, u.Use))
		}
	}
	used := "还没有应用接着它"
	if len(who) > 0 {
		used = strings.Join(who, "、") + " 接着它，用到它的功能现在用不了"
	}
	return fmt.Sprintf("服务 %s（%s，由应用 %s 提供）还不能用：%s。%s。把这句话告诉用户，照它说的做；acornfox services show %s 看检查哪一步没过", sv.Name, label, sv.Provider, say, used, sv.Name)
}

// serviceProblem is the warning for one service that is not usable.
func serviceProblem(sv ServiceInUse, users []ServiceUser) string {
	label := sv.KindLabel
	if label == "" {
		label = sv.Kind
	}
	state := sv.StatusLabel
	if state == "" {
		state = sv.Status
	}
	who := make([]string, 0, len(users))
	for _, u := range users {
		who = append(who, fmt.Sprintf("应用 %s 的需要 %s", u.App, u.Use))
	}
	head := fmt.Sprintf("应用用的服务：%s 接的服务 %s（%s）现在是“%s”", strings.Join(who, "、"), sv.Name, label, state)
	var how string
	switch sv.Status {
	case ServiceStatusProviderStopped:
		how = fmt.Sprintf("提供它的应用 %s 停着，用到这项服务的功能现在用不了。问用户是不是有意停的，不是就启动它：acornfox start %s", sv.Provider, sv.Provider)
	case ServiceStatusProviderGone:
		how = fmt.Sprintf("提供它的应用 %s 已经删了（数据还留着）。要接着用就把它接上旧数据装回来：acornfox install --name %s --reuse-data（平台从删除时保留的记录里找到它原来是哪个应用；可以先加 --plan 看一次）。不用了就让这些应用改接别的服务（重装它们时用 --use 指定）", sv.Provider, sv.Provider)
	case ServiceStatusIncomplete:
		items := strings.Join(sv.Missing, "、")
		if items == "" {
			items = "必填项"
		}
		how = fmt.Sprintf("还差 %s 没填。acornfox services show %s 看是哪几项、怎么拿到，再 acornfox services set %s 项=值（密码、Key 写成 项=-，让用户自己在终端输入）", items, sv.Name, sv.Name)
	default:
		how = fmt.Sprintf("acornfox services show %s 看它怎么了", sv.Name)
	}
	return head + "。" + how
}
