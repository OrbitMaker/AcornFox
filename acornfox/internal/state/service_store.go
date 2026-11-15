package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Service registry records (migration 0014): the services this machine can
// offer to its apps, and which need of which app is plugged into which
// service.
//
// The store keeps the two encrypted columns (ValuesEnc, IssuedEnc) exactly as
// given and never decrypts them; encryption is the caller's
// (internal/appinstall). Whether a service is usable right now is not stored
// either: it is worked out by the caller from these rows and the current
// state of the providing app.
//
// Neither table references apps(name), and DeleteApp does not touch them: the
// caller decides when the bindings and services of an app go
// (DeleteAppBindings, DeleteAppServices), because it needs what a binding
// holds to take the credentials back first.

// Where a service comes from.
const (
	ServiceFromApp  = "app"  // provided by an installed app
	ServiceFromUser = "user" // registered by the user
)

// Service is one row of the registry.
type Service struct {
	Name string `json:"name"` // unique on this machine
	Kind string `json:"kind"` // service type
	// Source is ServiceFromApp or ServiceFromUser.
	Source string `json:"source"`
	// App is the providing app and ProvideIndex the entry of its manifest's
	// provides the service comes from (ServiceFromApp only; "" and 0 for a
	// user's service). An app has at most one service per entry.
	App          string `json:"app,omitempty"`
	ProvideIndex int    `json:"provide_index"`
	// ValuesEnc is the ciphertext of the JSON object {item: value}; "" when
	// the service has no values yet.
	ValuesEnc string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ServiceBinding says that the need UseName of App is plugged into Service.
type ServiceBinding struct {
	App     string `json:"app"`      // the consuming app
	UseName string `json:"use_name"` // the name of the need in its manifest
	Service string `json:"service"`  // Service.Name
	// IssuedEnc is the ciphertext of the JSON object of what the provider
	// issued to this consumer alone; "" when nothing was issued.
	IssuedEnc string `json:"-"`
	// Dedicated: the need takes the service for itself (its manifest's
	// uses[].dedicated); while this binding is there no other app may be
	// plugged into the service (migration 0016).
	Dedicated bool `json:"dedicated,omitempty"`
	// MaxConsumers is not stored: the providing entry's max_consumers (0: no
	// limit), checked by SetServiceBinding when the binding is written.
	MaxConsumers int       `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// ServiceFilter narrows ListServices; a zero field means any.
type ServiceFilter struct {
	Kind string
	App  string // services provided by this app
}

// BindingFilter narrows ListServiceBindings; a zero field means any.
type BindingFilter struct {
	App     string // bindings of this consuming app
	Service string // bindings to this service
}

// ServiceNameMaxLen is the most characters (not bytes) a service name can
// have.
const ServiceNameMaxLen = 128

// reservedServiceNames cannot be service names: each is a fixed word in the
// place of the address where a service name goes ("the kinds of services" is
// read at the address a service called "kinds" would have).
var reservedServiceNames = map[string]bool{"kinds": true}

var (
	serviceKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	useNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// ValidServiceName reports whether name can be a service name.
//
// A service name is what the user calls the service, in their own language:
//
//   - 1 to ServiceNameMaxLen characters;
//   - letters of any script and digits, and after the first character also
//     "-", "_", "." and combining marks;
//   - not a reserved word (ReservedServiceName).
//
// So no blanks, no "/", no "=", no "," and no control or invisible
// characters: a name can be typed on a command line without quoting, is one
// segment of an address, and two names that look the same are the same.
// Names are compared exactly; upper and lower case are different names.
//
// An app name, and an app name followed by "-" and a service kind (and a
// number), are always of this form.
func ValidServiceName(name string) bool {
	if name == "" || reservedServiceNames[name] {
		return false
	}
	n := 0
	for i, r := range name {
		n++
		switch {
		case r == utf8.RuneError:
			return false // also what invalid UTF-8 decodes to
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case i > 0 && (r == '-' || r == '_' || r == '.' || unicode.IsMark(r)):
		default:
			return false
		}
	}
	return n <= ServiceNameMaxLen
}

// ReservedServiceName reports whether name is refused as a service name only
// because the word is reserved.
func ReservedServiceName(name string) bool { return reservedServiceNames[name] }

// ValidServiceKind reports whether kind has the service type form: a lowercase
// letter followed by up to 63 of a-z, 0-9 and "_".
func ValidServiceKind(kind string) bool { return serviceKindPattern.MatchString(kind) }

// ValidUseName reports whether name has the form of a need's name in a
// manifest (the same form as a service type).
func ValidUseName(name string) bool { return useNamePattern.MatchString(name) }

// parkedUseSuffix marks a binding moved aside (ParkedUseName). A need's name
// never has "~" in it, so a parked binding is never taken for a need.
const parkedUseSuffix = "~replaced"

// ParkedUseName is where an install moves the binding a need had from before
// while it plugs that need into something else: moved back when the install
// fails, taken back and removed once it has succeeded. Until then the old
// credential stays recorded (encrypted, like any binding's).
func ParkedUseName(useName string) string { return useName + parkedUseSuffix }

// ParkedUse reports whether name is a parked binding's name, and of which
// need.
func ParkedUse(name string) (string, bool) {
	base, ok := strings.CutSuffix(name, parkedUseSuffix)
	return base, ok && ValidUseName(base)
}

// validBindingUse: a need's name, or a parked binding's.
func validBindingUse(name string) bool {
	if ValidUseName(name) {
		return true
	}
	_, ok := ParkedUse(name)
	return ok
}

// Why a service could not be created or moved. Both match ErrConflict with
// errors.Is.
var (
	// ErrServiceNameTaken: there is already a service with that name.
	ErrServiceNameTaken = fmt.Errorf("%w: service name already in use", ErrConflict)
	// ErrServiceEntryTaken: the app already has a service for that entry of
	// its provides.
	ErrServiceEntryTaken = fmt.Errorf("%w: the app already has a service for that entry", ErrConflict)
)

// ServiceUser is one binding that keeps a service from being deleted.
type ServiceUser struct {
	Service string `json:"service"`
	App     string `json:"app"`
	UseName string `json:"use_name"`
}

// ServiceInUseError is returned when a service cannot be deleted because apps
// are still plugged into it. It matches ErrConflict with errors.Is.
type ServiceInUseError struct {
	// Users are the bindings in the way, ordered by service, app and need.
	Users []ServiceUser
}

// Apps returns the distinct consuming apps, sorted.
func (e *ServiceInUseError) Apps() []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range e.Users {
		if !seen[u.App] {
			seen[u.App] = true
			out = append(out, u.App)
		}
	}
	sort.Strings(out)
	return out
}

// Services returns the distinct services that are in use, sorted.
func (e *ServiceInUseError) Services() []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range e.Users {
		if !seen[u.Service] {
			seen[u.Service] = true
			out = append(out, u.Service)
		}
	}
	sort.Strings(out)
	return out
}

func (e *ServiceInUseError) Error() string {
	return fmt.Sprintf("state: service %s is in use by %s",
		strings.Join(e.Services(), ", "), strings.Join(e.Apps(), ", "))
}

// Is makes errors.Is(err, ErrConflict) true for a ServiceInUseError.
func (e *ServiceInUseError) Is(target error) bool { return target == ErrConflict }

// Why a need cannot be plugged into a service because of the apps plugged
// into it already (ServiceTakenError.Reason).
const (
	// TakenHeld: another app's need holds the service for itself (dedicated).
	TakenHeld = "held"
	// TakenInUse: the need wants the service for itself and other apps are
	// plugged into it.
	TakenInUse = "in_use"
	// TakenFull: as many apps as the provider allows (max_consumers) are
	// plugged into it.
	TakenFull = "full"
)

// ServiceTakenError is returned by SetServiceBinding and CheckServicePlug
// when the need may not be plugged into the service because of the other
// apps plugged into it. It matches ErrConflict with errors.Is.
//
// The rule (one place, checked in the transaction that writes the binding,
// so two installs cannot both take a service only one of them may have):
// the apps counted are the other apps with a binding to the service, apps
// deleted with their data kept included (their binding waits for them);
// the app's own bindings never count.
//
//  1. Another app holds it for itself (a dedicated binding): TakenHeld.
//  2. The need is dedicated and another app is plugged in: TakenInUse.
//  3. MaxConsumers > 0, the app is not plugged into the service yet (under
//     any need) and MaxConsumers other apps are: TakenFull.
type ServiceTakenError struct {
	Service string
	Reason  string
	// Apps are the other apps plugged into the service, sorted; for
	// TakenHeld the ones that hold it.
	Apps []string
	// Max is the provider's limit (TakenFull).
	Max int
}

func (e *ServiceTakenError) Error() string {
	return fmt.Sprintf("state: service %s cannot be plugged in (%s): %s", e.Service, e.Reason, strings.Join(e.Apps, ", "))
}

// Is makes errors.Is(err, ErrConflict) true for a ServiceTakenError.
func (e *ServiceTakenError) Is(target error) bool { return target == ErrConflict }

// plugQuerier is what checkPlug reads with: the database or a transaction.
type plugQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// PlugBinding is one binding to a service as the rule of
// ServiceTakenError sees it: the consuming app and whether its need takes
// the service for itself.
type PlugBinding struct {
	App       string
	Dedicated bool
}

// PlugRule is the rule of ServiceTakenError for binding b, given the
// bindings to b.Service there are (any app's, b.App's own included: they
// do not count against it). nil when b may be written.
func PlugRule(existing []PlugBinding, b ServiceBinding) error {
	others, holders := map[string]bool{}, map[string]bool{}
	already := false
	for _, x := range existing {
		if x.App == b.App {
			already = true
			continue
		}
		others[x.App] = true
		if x.Dedicated {
			holders[x.App] = true
		}
	}
	sorted := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	switch {
	case len(holders) > 0:
		return &ServiceTakenError{Service: b.Service, Reason: TakenHeld, Apps: sorted(holders)}
	case b.Dedicated && len(others) > 0:
		return &ServiceTakenError{Service: b.Service, Reason: TakenInUse, Apps: sorted(others)}
	case b.MaxConsumers > 0 && !already && len(others) >= b.MaxConsumers:
		return &ServiceTakenError{Service: b.Service, Reason: TakenFull, Apps: sorted(others), Max: b.MaxConsumers}
	}
	return nil
}

// checkPlug applies PlugRule to binding b, with the bindings q reads.
func checkPlug(ctx context.Context, q plugQuerier, b ServiceBinding) error {
	rows, err := q.QueryContext(ctx, `SELECT app, dedicated FROM service_bindings WHERE service=? ORDER BY app, use_name`, b.Service)
	if err != nil {
		return fmt.Errorf("read service consumers: %w", err)
	}
	var existing []PlugBinding
	for rows.Next() {
		var x PlugBinding
		if err := rows.Scan(&x.App, &x.Dedicated); err != nil {
			rows.Close()
			return fmt.Errorf("scan service consumer: %w", err)
		}
		existing = append(existing, x)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("read service consumers: %w", err)
	}
	return PlugRule(existing, b)
}

// CheckServicePlug tells, without writing anything, whether b could be
// written now by the rule of ServiceTakenError (nil) or not (a
// *ServiceTakenError). It is a look ahead only: SetServiceBinding checks
// again when it writes.
func (s *Store) CheckServicePlug(ctx context.Context, b ServiceBinding) error {
	return checkPlug(ctx, s.db, b)
}

func validService(sv Service) error {
	if !ValidServiceName(sv.Name) {
		return fmt.Errorf("%w: service name %q", ErrInvalid, sv.Name)
	}
	if !ValidServiceKind(sv.Kind) {
		return fmt.Errorf("%w: service kind %q", ErrInvalid, sv.Kind)
	}
	if sv.ProvideIndex < 0 {
		return fmt.Errorf("%w: service provide index %d", ErrInvalid, sv.ProvideIndex)
	}
	switch sv.Source {
	case ServiceFromApp:
		if !ValidAppName(sv.App) {
			return fmt.Errorf("%w: service of an app needs the app name, got %q", ErrInvalid, sv.App)
		}
	case ServiceFromUser:
		if sv.App != "" || sv.ProvideIndex != 0 {
			return fmt.Errorf("%w: a user's service has no providing app", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: service source %q", ErrInvalid, sv.Source)
	}
	return nil
}

// CreateService inserts sv (CreatedAt and UpdatedAt are set to now).
// ErrInvalid for a malformed row; ErrServiceNameTaken when the name is in
// use; ErrServiceEntryTaken when the app already has a service for that
// provides entry (both are ErrConflict).
func (s *Store) CreateService(ctx context.Context, sv Service) (Service, error) {
	if err := validService(sv); err != nil {
		return Service{}, err
	}
	now := time.Now().UTC()
	sv.CreatedAt, sv.UpdatedAt = now, now
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM services WHERE name=?`, sv.Name).Scan(&n); err != nil {
			return fmt.Errorf("look up service: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("%w: %q", ErrServiceNameTaken, sv.Name)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO services (name, kind, source, app, provide_index, values_enc, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sv.Name, sv.Kind, sv.Source, sv.App, sv.ProvideIndex, sv.ValuesEnc, formatTime(now), formatTime(now)); err != nil {
			// The name is free, so the only unique rule left is one service
			// per provides entry of an app.
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("%w: entry %d of app %q", ErrServiceEntryTaken, sv.ProvideIndex, sv.App)
			}
			return fmt.Errorf("insert service: %w", err)
		}
		return nil
	})
	if err != nil {
		return Service{}, err
	}
	return sv, nil
}

// UpdateService reads, applies fn and writes the service in one transaction;
// UpdatedAt is set to now. Only ValuesEnc and ProvideIndex can change: Name,
// Kind, Source, App and CreatedAt are put back after fn. ErrNotFound when
// there is no such service; ErrServiceEntryTaken when the new ProvideIndex
// belongs to another service of the same app.
//
// fn runs inside the transaction, and the store has a single connection: fn
// must not call the store (any method of it), or the call waits for a
// connection that fn itself is holding until ctx ends.
func (s *Store) UpdateService(ctx context.Context, name string, fn func(*Service) error) (Service, error) {
	var out Service
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanService(tx.QueryRowContext(ctx, serviceSelect+` WHERE name=?`, name))
		if err != nil {
			return err
		}
		draft := cur
		if err := fn(&draft); err != nil {
			return err
		}
		draft.Name, draft.Kind, draft.Source, draft.App, draft.CreatedAt = cur.Name, cur.Kind, cur.Source, cur.App, cur.CreatedAt
		if err := validService(draft); err != nil {
			return err
		}
		draft.UpdatedAt = time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `UPDATE services SET provide_index=?, values_enc=?, updated_at=? WHERE name=?`,
			draft.ProvideIndex, draft.ValuesEnc, formatTime(draft.UpdatedAt), name); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("%w: entry %d of app %q", ErrServiceEntryTaken, draft.ProvideIndex, draft.App)
			}
			return fmt.Errorf("update service: %w", err)
		}
		out = draft
		return nil
	})
	return out, err
}

// GetService returns the service called name, ErrNotFound when there is none.
func (s *Store) GetService(ctx context.Context, name string) (Service, error) {
	return scanService(s.db.QueryRowContext(ctx, serviceSelect+` WHERE name=?`, name))
}

// ListServices returns the services matching f, ordered by name.
func (s *Store) ListServices(ctx context.Context, f ServiceFilter) ([]Service, error) {
	q := serviceSelect
	var where []string
	var args []any
	if f.Kind != "" {
		where = append(where, `kind=?`)
		args = append(args, f.Kind)
	}
	if f.App != "" {
		where = append(where, `source='app' AND app=?`)
		args = append(args, f.App)
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		sv, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

// DeleteService removes the service called name. ErrNotFound when there is
// none; a *ServiceInUseError (nothing is deleted) while any app is still
// plugged into it.
func (s *Store) DeleteService(ctx context.Context, name string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanService(tx.QueryRowContext(ctx, serviceSelect+` WHERE name=?`, name)); err != nil {
			return err
		}
		users, err := serviceUsersTx(ctx, tx, `service=?`, name)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return &ServiceInUseError{Users: users}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE name=?`, name); err != nil {
			return fmt.Errorf("delete service: %w", err)
		}
		return nil
	})
}

// DeleteAppServices removes every service app provides and returns what it
// removed (nil when the app provided none: not an error). While another app
// is still plugged into any of them it removes nothing and returns a
// *ServiceInUseError naming every such binding. A binding of the app to its
// own service does not count and goes with the service.
func (s *Store) DeleteAppServices(ctx context.Context, app string) ([]Service, error) {
	var removed []Service
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		const mine = `service IN (SELECT name FROM services WHERE source='app' AND app=?)`
		users, err := serviceUsersTx(ctx, tx, mine+` AND app<>?`, app, app)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			return &ServiceInUseError{Users: users}
		}
		rows, err := tx.QueryContext(ctx, serviceSelect+` WHERE source='app' AND app=? ORDER BY name`, app)
		if err != nil {
			return fmt.Errorf("list services of app: %w", err)
		}
		for rows.Next() {
			sv, err := scanService(rows)
			if err != nil {
				rows.Close()
				return err
			}
			removed = append(removed, sv)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return fmt.Errorf("list services of app: %w", err)
		}
		if len(removed) == 0 {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM service_bindings WHERE `+mine, app); err != nil {
			return fmt.Errorf("delete own bindings: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE source='app' AND app=?`, app); err != nil {
			return fmt.Errorf("delete services of app: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

// serviceUsersTx lists the bindings matching cond as ServiceUsers.
func serviceUsersTx(ctx context.Context, tx *sql.Tx, cond string, args ...any) ([]ServiceUser, error) {
	rows, err := tx.QueryContext(ctx, `SELECT service, app, use_name FROM service_bindings WHERE `+cond+` ORDER BY service, app, use_name`, args...)
	if err != nil {
		return nil, fmt.Errorf("list service users: %w", err)
	}
	defer rows.Close()
	var out []ServiceUser
	for rows.Next() {
		var u ServiceUser
		if err := rows.Scan(&u.Service, &u.App, &u.UseName); err != nil {
			return nil, fmt.Errorf("scan service user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

const serviceSelect = `SELECT name, kind, source, app, provide_index, values_enc, created_at, updated_at FROM services`

func scanService(sc rowScanner) (Service, error) {
	var sv Service
	var c, u string
	if err := sc.Scan(&sv.Name, &sv.Kind, &sv.Source, &sv.App, &sv.ProvideIndex, &sv.ValuesEnc, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("scan service: %w", err)
	}
	var err error
	if sv.CreatedAt, err = parseTime(c); err != nil {
		return Service{}, err
	}
	if sv.UpdatedAt, err = parseTime(u); err != nil {
		return Service{}, err
	}
	return sv, nil
}

// SetServiceBinding plugs the need b.UseName of b.App into b.Service, or
// replaces what is recorded for that need. CreatedAt is when the need was
// plugged into this service: it is kept when only IssuedEnc changes and set
// to now when the need is new or moves to another service. ErrInvalid for a
// malformed row; ErrNotFound when the service does not exist; a
// *ServiceTakenError when the apps plugged into the service already leave
// no room for it (b.Dedicated, b.MaxConsumers: the rule is
// ServiceTakenError's, checked in the same transaction as the write).
//
// What was recorded before is overwritten, IssuedEnc included: a caller that
// still has to take the old credentials back reads the binding first.
func (s *Store) SetServiceBinding(ctx context.Context, b ServiceBinding) (ServiceBinding, error) {
	if !ValidAppName(b.App) {
		return ServiceBinding{}, fmt.Errorf("%w: binding app %q", ErrInvalid, b.App)
	}
	if !ValidUseName(b.UseName) {
		return ServiceBinding{}, fmt.Errorf("%w: binding need %q", ErrInvalid, b.UseName)
	}
	if !ValidServiceName(b.Service) {
		return ServiceBinding{}, fmt.Errorf("%w: binding service %q", ErrInvalid, b.Service)
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanService(tx.QueryRowContext(ctx, serviceSelect+` WHERE name=?`, b.Service)); err != nil {
			return err
		}
		if err := checkPlug(ctx, tx, b); err != nil {
			return err
		}
		b.CreatedAt = time.Now().UTC()
		cur, err := scanBinding(tx.QueryRowContext(ctx, bindingSelect+` WHERE app=? AND use_name=?`, b.App, b.UseName))
		switch {
		case err == nil && cur.Service == b.Service:
			b.CreatedAt = cur.CreatedAt
		case err != nil && !errors.Is(err, ErrNotFound):
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO service_bindings (app, use_name, service, issued_enc, created_at, dedicated)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(app, use_name) DO UPDATE SET service=excluded.service, issued_enc=excluded.issued_enc, created_at=excluded.created_at, dedicated=excluded.dedicated`,
			b.App, b.UseName, b.Service, b.IssuedEnc, formatTime(b.CreatedAt), b.Dedicated); err != nil {
			return fmt.Errorf("set service binding: %w", err)
		}
		return nil
	})
	if err != nil {
		return ServiceBinding{}, err
	}
	return b, nil
}

// GetServiceBinding returns what the need useName of app is plugged into,
// ErrNotFound when it is not plugged into anything.
func (s *Store) GetServiceBinding(ctx context.Context, app, useName string) (ServiceBinding, error) {
	return scanBinding(s.db.QueryRowContext(ctx, bindingSelect+` WHERE app=? AND use_name=?`, app, useName))
}

// ListServiceBindings returns the bindings matching f, ordered by consuming
// app and need: f.App alone lists what one app is plugged into, f.Service
// alone lists who uses one service, the zero filter lists everything.
func (s *Store) ListServiceBindings(ctx context.Context, f BindingFilter) ([]ServiceBinding, error) {
	q := bindingSelect
	var where []string
	var args []any
	if f.App != "" {
		where = append(where, `app=?`)
		args = append(args, f.App)
	}
	if f.Service != "" {
		where = append(where, `service=?`)
		args = append(args, f.Service)
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY app, use_name`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list service bindings: %w", err)
	}
	defer rows.Close()
	var out []ServiceBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MoveServiceBinding renames the binding of app from need from to need to,
// what it holds and when it was made unchanged, in one statement: one of the
// two is a parked name (ParkedUseName). ErrNotFound when there is nothing at
// from; ErrConflict when to is taken; ErrInvalid for a malformed name.
func (s *Store) MoveServiceBinding(ctx context.Context, app, from, to string) error {
	if !ValidAppName(app) || !validBindingUse(from) || !validBindingUse(to) || from == to {
		return fmt.Errorf("%w: move binding of %q from %q to %q", ErrInvalid, app, from, to)
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanBinding(tx.QueryRowContext(ctx, bindingSelect+` WHERE app=? AND use_name=?`, app, to)); err == nil {
			return fmt.Errorf("%w: binding %s/%s exists", ErrConflict, app, to)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE service_bindings SET use_name=? WHERE app=? AND use_name=?`, to, app, from)
		if err != nil {
			return fmt.Errorf("move service binding: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("move service binding: %w", err)
		} else if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// DeleteServiceBinding unplugs the need useName of app and reports whether
// there was such a binding; unplugging nothing is not an error.
func (s *Store) DeleteServiceBinding(ctx context.Context, app, useName string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM service_bindings WHERE app=? AND use_name=?`, app, useName)
	if err != nil {
		return false, fmt.Errorf("delete service binding: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete service binding: %w", err)
	}
	return n > 0, nil
}

// DeleteAppBindings unplugs every need of app and returns how many there
// were. It only removes the rows: taking back what was issued to the app is
// the caller's, before this.
func (s *Store) DeleteAppBindings(ctx context.Context, app string) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM service_bindings WHERE app=?`, app)
	if err != nil {
		return 0, fmt.Errorf("delete bindings of app: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete bindings of app: %w", err)
	}
	return int(n), nil
}

const bindingSelect = `SELECT app, use_name, service, issued_enc, created_at, dedicated FROM service_bindings`

func scanBinding(sc rowScanner) (ServiceBinding, error) {
	var b ServiceBinding
	var c string
	if err := sc.Scan(&b.App, &b.UseName, &b.Service, &b.IssuedEnc, &c, &b.Dedicated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ServiceBinding{}, ErrNotFound
		}
		return ServiceBinding{}, fmt.Errorf("scan service binding: %w", err)
	}
	var err error
	if b.CreatedAt, err = parseTime(c); err != nil {
		return ServiceBinding{}, err
	}
	return b, nil
}
