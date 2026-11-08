package appinstall

import (
	"context"
	"encoding/json"

	"github.com/acornfox/acornfox/internal/state"
)

// HasLogin reports whether app, installed from the catalog, asks for its own
// login: its manifest gave first-login credentials (an account and a
// password the install generated or set), so it does not open to anyone who
// reaches its port. The console offers to open a local-only app on the LAN
// only when this is true. Unknown apps, read errors and records without
// fields answer false (the safe side: the console then offers only the SSH
// tunnel). It reads the encrypted record without building the app's URL,
// so the API server can call it while building an app view.
func (e *Engine) HasLogin(ctx context.Context, app string) bool {
	if !state.ValidAppName(app) {
		return false
	}
	cs, err := e.cfg.Store.GetCatalogSecrets(ctx, app)
	if err != nil {
		return false
	}
	plain, err := e.box.open(cs.FirstLoginEnc)
	if err != nil {
		return false
	}
	var fl FirstLogin
	if err := json.Unmarshal(plain, &fl); err != nil {
		return false
	}
	for _, f := range fl.Fields {
		// A password the old data keeps (reinstall on old data) still counts.
		if (f.Secret && f.Value != "") || f.Value == ReusedLoginValue {
			return true
		}
	}
	return false
}
