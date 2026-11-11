package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// App-center records (migrations 0010, 0012, 0013): install tasks, the
// encrypted install parameters / first-login fields of apps installed from
// the catalog, and the manifest each was installed with.
// The store keeps the ciphertext as given; encryption is the caller's
// (internal/appinstall).

// Install task statuses.
const (
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
)

// InstallTask is one install from the catalog. Data is the progress document
// (steps, apps, fault) as JSON; it never holds secrets.
type InstallTask struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`   // "app" | "recipe"
	Target    string    `json:"target"` // catalog app or recipe id
	Status    string    `json:"status"` // Task*
	FaultCode string    `json:"fault_code,omitempty"`
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ValidTaskID reports whether id has the install task id form (12 hex).
func ValidTaskID(id string) bool { return taskIDPattern.MatchString(id) }

func validTask(t InstallTask) error {
	if !ValidTaskID(t.ID) {
		return fmt.Errorf("%w: task id %q", ErrInvalid, t.ID)
	}
	if t.Kind != "app" && t.Kind != "recipe" {
		return fmt.Errorf("%w: task kind %q", ErrInvalid, t.Kind)
	}
	switch t.Status {
	case TaskRunning, TaskSucceeded, TaskFailed:
	default:
		return fmt.Errorf("%w: task status %q", ErrInvalid, t.Status)
	}
	if t.Data == "" || !json.Valid([]byte(t.Data)) {
		return fmt.Errorf("%w: task data is not JSON", ErrInvalid)
	}
	return nil
}

// CreateInstallTask inserts t (CreatedAt/UpdatedAt are set to now).
// ErrConflict when the id exists.
func (s *Store) CreateInstallTask(ctx context.Context, t InstallTask) (InstallTask, error) {
	if t.Data == "" {
		t.Data = "{}"
	}
	if err := validTask(t); err != nil {
		return InstallTask{}, err
	}
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO install_tasks (id, kind, target, status, fault_code, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, t.Kind, t.Target, t.Status, t.FaultCode, t.Data, formatTime(now), formatTime(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return InstallTask{}, ErrConflict
		}
		return InstallTask{}, fmt.Errorf("insert install task: %w", err)
	}
	return t, nil
}

// UpdateInstallTask reads, applies fn and writes the task in one transaction.
// ID, Kind, Target and CreatedAt are immutable.
func (s *Store) UpdateInstallTask(ctx context.Context, id string, fn func(*InstallTask) error) (InstallTask, error) {
	var out InstallTask
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE id=?`, id))
		if err != nil {
			return err
		}
		draft := cur
		if err := fn(&draft); err != nil {
			return err
		}
		draft.ID, draft.Kind, draft.Target, draft.CreatedAt = cur.ID, cur.Kind, cur.Target, cur.CreatedAt
		if err := validTask(draft); err != nil {
			return err
		}
		draft.UpdatedAt = time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `UPDATE install_tasks SET status=?, fault_code=?, data=?, updated_at=? WHERE id=?`,
			draft.Status, draft.FaultCode, draft.Data, formatTime(draft.UpdatedAt), id); err != nil {
			return fmt.Errorf("update install task: %w", err)
		}
		out = draft
		return nil
	})
	return out, err
}

// GetInstallTask returns the task or ErrNotFound.
func (s *Store) GetInstallTask(ctx context.Context, id string) (InstallTask, error) {
	return scanTask(s.db.QueryRowContext(ctx, taskSelect+` WHERE id=?`, id))
}

// ListInstallTasks returns tasks newest first; status "" means any.
func (s *Store) ListInstallTasks(ctx context.Context, status string, limit int) ([]InstallTask, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := taskSelect
	args := []any{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list install tasks: %w", err)
	}
	defer rows.Close()
	var out []InstallTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const taskSelect = `SELECT id, kind, target, status, fault_code, data, created_at, updated_at FROM install_tasks`

func scanTask(sc rowScanner) (InstallTask, error) {
	var t InstallTask
	var c, u string
	if err := sc.Scan(&t.ID, &t.Kind, &t.Target, &t.Status, &t.FaultCode, &t.Data, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InstallTask{}, ErrNotFound
		}
		return InstallTask{}, fmt.Errorf("scan install task: %w", err)
	}
	var err error
	if t.CreatedAt, err = parseTime(c); err != nil {
		return InstallTask{}, err
	}
	if t.UpdatedAt, err = parseTime(u); err != nil {
		return InstallTask{}, err
	}
	return t, nil
}

// CatalogSecrets are the encrypted install parameters and first-login fields
// of one catalog app.
type CatalogSecrets struct {
	App           string
	ParamsEnc     string
	FirstLoginEnc string
	UpdatedAt     time.Time
}

// SetCatalogSecrets stores (replaces) the ciphertexts for app. ErrNotFound
// when the app does not exist.
func (s *Store) SetCatalogSecrets(ctx context.Context, cs CatalogSecrets) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanAppTx(ctx, tx, cs.App); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO app_catalog_secrets (app, params_enc, first_login_enc, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(app) DO UPDATE SET params_enc=excluded.params_enc, first_login_enc=excluded.first_login_enc, updated_at=excluded.updated_at`,
			cs.App, cs.ParamsEnc, cs.FirstLoginEnc, formatTime(time.Now().UTC()))
		if err != nil {
			return fmt.Errorf("set catalog secrets: %w", err)
		}
		return nil
	})
}

// GetCatalogSecrets returns the ciphertexts of app, ErrNotFound when none.
func (s *Store) GetCatalogSecrets(ctx context.Context, app string) (CatalogSecrets, error) {
	var cs CatalogSecrets
	var u string
	err := s.db.QueryRowContext(ctx, `SELECT app, params_enc, first_login_enc, updated_at FROM app_catalog_secrets WHERE app=?`, app).
		Scan(&cs.App, &cs.ParamsEnc, &cs.FirstLoginEnc, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return CatalogSecrets{}, ErrNotFound
	}
	if err != nil {
		return CatalogSecrets{}, fmt.Errorf("get catalog secrets: %w", err)
	}
	if cs.UpdatedAt, err = parseTime(u); err != nil {
		return CatalogSecrets{}, err
	}
	return cs, nil
}

// KeptCatalogSecrets is the saved parameters of an app-center app deleted
// with its data kept (see migration0012). Manifest and ManifestSHA256 are the
// manifest the deleted app was installed with ("" when it had none saved,
// see migration0013).
type KeptCatalogSecrets struct {
	App            string
	CatalogID      string
	CatalogOrigin  string
	ParamsEnc      string
	KeptAt         time.Time
	Manifest       string
	ManifestSHA256 string
}

// GetKeptCatalogSecrets returns what was kept for app; ErrNotFound when none.
func (s *Store) GetKeptCatalogSecrets(ctx context.Context, app string) (KeptCatalogSecrets, error) {
	var k KeptCatalogSecrets
	var at string
	err := s.db.QueryRowContext(ctx, `SELECT app, catalog_id, catalog_origin, params_enc, kept_at, manifest, manifest_sha256 FROM kept_catalog_secrets WHERE app=?`, app).
		Scan(&k.App, &k.CatalogID, &k.CatalogOrigin, &k.ParamsEnc, &at, &k.Manifest, &k.ManifestSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return KeptCatalogSecrets{}, ErrNotFound
	}
	if err != nil {
		return KeptCatalogSecrets{}, fmt.Errorf("get kept catalog secrets: %w", err)
	}
	if k.KeptAt, err = parseTime(at); err != nil {
		return KeptCatalogSecrets{}, err
	}
	return k, nil
}

// DeleteKeptCatalogSecrets drops what was kept for app (once a reuse install
// has saved its own parameters); deleting nothing is not an error.
func (s *Store) DeleteKeptCatalogSecrets(ctx context.Context, app string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kept_catalog_secrets WHERE app=?`, app); err != nil {
		return fmt.Errorf("delete kept catalog secrets: %w", err)
	}
	return nil
}

// Where a saved manifest came from.
const (
	ManifestFromInstall  = "install"  // saved by the install itself
	ManifestFromBackfill = "backfill" // found on this machine later, for an app installed before manifests were saved
)

// CatalogManifest is the manifest an app-center app was installed with
// (migration0013). Manifest is the manifest.yaml text of the app package as
// it was; SHA256 is the lowercase hex sha256 of exactly those bytes. A
// manifest holds no secrets, so it is kept in clear.
type CatalogManifest struct {
	App            string
	CatalogID      string
	CatalogVersion string
	CatalogOrigin  string // "" = public catalog, else the private catalog id
	Manifest       string
	SHA256         string
	Source         string // ManifestFrom*
	SavedAt        time.Time
}

// ManifestSHA256 is the checksum CatalogManifest.SHA256 must hold.
func ManifestSHA256(manifest string) string {
	sum := sha256.Sum256([]byte(manifest))
	return hex.EncodeToString(sum[:])
}

func validCatalogManifest(m CatalogManifest) error {
	if m.CatalogID == "" || m.Manifest == "" {
		return fmt.Errorf("%w: catalog manifest needs a catalog id and the manifest text", ErrInvalid)
	}
	if m.Source != ManifestFromInstall && m.Source != ManifestFromBackfill {
		return fmt.Errorf("%w: catalog manifest source %q", ErrInvalid, m.Source)
	}
	if m.SHA256 != ManifestSHA256(m.Manifest) {
		return fmt.Errorf("%w: catalog manifest checksum does not match the text", ErrInvalid)
	}
	return nil
}

// SetCatalogManifest stores (replaces) the manifest app was installed with;
// SavedAt is set to now. ErrNotFound when the app does not exist, ErrInvalid
// when SHA256 is not the checksum of Manifest.
func (s *Store) SetCatalogManifest(ctx context.Context, m CatalogManifest) error {
	if err := validCatalogManifest(m); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanAppTx(ctx, tx, m.App); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO app_catalog_manifests (app, catalog_id, catalog_version, catalog_origin, manifest, manifest_sha256, source, saved_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(app) DO UPDATE SET catalog_id=excluded.catalog_id, catalog_version=excluded.catalog_version,
				catalog_origin=excluded.catalog_origin, manifest=excluded.manifest, manifest_sha256=excluded.manifest_sha256,
				source=excluded.source, saved_at=excluded.saved_at`,
			m.App, m.CatalogID, m.CatalogVersion, m.CatalogOrigin, m.Manifest, m.SHA256, m.Source, formatTime(time.Now().UTC()))
		if err != nil {
			return fmt.Errorf("set catalog manifest: %w", err)
		}
		return nil
	})
}

// AddCatalogManifest stores the manifest only when app has none yet and
// reports whether it did; an existing row is never touched (a backfill must
// not replace what an install saved). ErrNotFound when the app does not
// exist.
func (s *Store) AddCatalogManifest(ctx context.Context, m CatalogManifest) (bool, error) {
	if err := validCatalogManifest(m); err != nil {
		return false, err
	}
	added := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanAppTx(ctx, tx, m.App); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO app_catalog_manifests (app, catalog_id, catalog_version, catalog_origin, manifest, manifest_sha256, source, saved_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(app) DO NOTHING`,
			m.App, m.CatalogID, m.CatalogVersion, m.CatalogOrigin, m.Manifest, m.SHA256, m.Source, formatTime(time.Now().UTC()))
		if err != nil {
			return fmt.Errorf("add catalog manifest: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("add catalog manifest: %w", err)
		}
		added = n > 0
		return nil
	})
	return added, err
}

// GetCatalogManifest returns the manifest app was installed with, ErrNotFound
// when none is saved.
func (s *Store) GetCatalogManifest(ctx context.Context, app string) (CatalogManifest, error) {
	var m CatalogManifest
	var at string
	err := s.db.QueryRowContext(ctx, `SELECT app, catalog_id, catalog_version, catalog_origin, manifest, manifest_sha256, source, saved_at
		FROM app_catalog_manifests WHERE app=?`, app).
		Scan(&m.App, &m.CatalogID, &m.CatalogVersion, &m.CatalogOrigin, &m.Manifest, &m.SHA256, &m.Source, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return CatalogManifest{}, ErrNotFound
	}
	if err != nil {
		return CatalogManifest{}, fmt.Errorf("get catalog manifest: %w", err)
	}
	if m.SavedAt, err = parseTime(at); err != nil {
		return CatalogManifest{}, err
	}
	return m, nil
}

// encodeStringList stores a list column as JSON ("" for an empty list).
func encodeStringList(v []string) (string, error) {
	if len(v) == 0 {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%w: encode list: %v", ErrInvalid, err)
	}
	return string(b), nil
}

func decodeStringList(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var v []string
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}
