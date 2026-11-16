package publicconsole

// Which door a console request came through, so the console can ask for the
// right password before it opens or closes the public console.
//
// The browser console is reached three ways:
//
//   - straight on the loopback console listener, after `acornfox open` over
//     SSH (a one-time link, no password): someone who can do that can also
//     run commands on the server, so nothing more is asked;
//   - through the LAN gateway (EntryLAN): the LAN console password;
//   - through the public console gateway (EntryPublic): the public console
//     password.
//
// The gateways put an *Entry on the request's context before handing it to
// the console. It lives only inside this process: nothing a client sends can
// set or change it, so a request without one came straight in on loopback.

import (
	"context"
	"time"
)

// Entry kinds.
const (
	EntryLAN    = "lan"
	EntryPublic = "public"
)

// Entry is the gateway a console request came through.
type Entry struct {
	Kind   string // EntryLAN or EntryPublic
	Client string // the visitor's address, as the gateway's login sees it
	check  func(client, password string) (bool, time.Duration)
}

// NewEntry returns an entry whose password check is check. check reports
// whether the password is right and, when refused because of too many
// failures, how long to wait. The gateways build theirs; tests use this.
func NewEntry(kind, client string, check func(client, password string) (bool, time.Duration)) *Entry {
	return &Entry{Kind: kind, Client: client, check: check}
}

// CheckPassword checks password against the password of this entry, with
// the same failure limits as its login page. wait > 0 means the attempt was
// not even checked: too many failures, try again after wait.
func (e *Entry) CheckPassword(password string) (ok bool, wait time.Duration) {
	if e == nil || e.check == nil {
		return false, 0
	}
	return e.check(e.Client, password)
}

type entryKey struct{}

// WithEntry returns ctx carrying e.
func WithEntry(ctx context.Context, e *Entry) context.Context {
	return context.WithValue(ctx, entryKey{}, e)
}

// EntryFrom returns the entry a request came through, or nil when it came
// straight in on the loopback console listener.
func EntryFrom(ctx context.Context) *Entry {
	e, _ := ctx.Value(entryKey{}).(*Entry)
	return e
}

// checkPassword is the gateways' Entry check: the same limiter as the login
// page (failures here count towards the same lockout) and the password of
// this gateway's configuration. Neither the password nor anything derived
// from it is logged.
func (g *gateway) checkPassword(client, password string) (bool, time.Duration) {
	if ok, wait := g.cfg.Limiter.Allow(client); !ok {
		return false, wait
	}
	cfg, err := g.cfg.Store.Load()
	if err != nil {
		g.cfg.Logger.Error("console: load config for password check", "err", err)
		return false, 0
	}
	if cfg == nil || password == "" || !cfg.CheckPassword(password) {
		g.cfg.Limiter.Fail(client)
		g.cfg.Logger.Warn("console: wrong password when opening or closing the public console", "client", client, "lan", g.cfg.LAN, "code", "PUBCON-01")
		return false, 0
	}
	g.cfg.Limiter.Success(client)
	return true, 0
}
