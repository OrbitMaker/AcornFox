package appinstall

// Icons in the catalog list (GET /v1/catalog). The signed index carries no
// icons; they live in each app's package. The list reads a package only when
// it is already on this machine (the snapshot built into the program, or the
// package cache filled by an earlier detail view or install) and never
// downloads one, so listing stays fast and offline. An app whose package is
// not here yet shows its initials until the package arrives.

import (
	"path/filepath"

	"github.com/acornfox/acornfox/internal/catalog"
)

// maxListIcon caps an icon carried in the list; a larger one shows on the
// detail page only.
const maxListIcon = 64 << 10

type listIcon struct{ icon, typ string }

// listIcon returns ia's icon (see iconView) or "" when its package is not on
// this machine or has no icon. Results are cached by package hash: a
// package's content never changes under the same hash.
func (c *Catalog) listIcon(v catalogView, ia catalog.IndexApp) (string, string) {
	key := v.origin + "/" + ia.Package.SHA256
	if x, ok := c.icons.Load(key); ok {
		li := x.(listIcon)
		return li.icon, li.typ
	}
	var b []byte
	var err error
	if v.origin == "" {
		b, err = catalog.LoadPackage(nil, c.cfg.Snapshot, filepath.Join(c.cfg.CacheDir, "packages"), ia.Package)
	} else {
		b, err = catalog.LoadPackage(nil, nil, c.privatePackageDir(v.origin), ia.Package)
	}
	if err != nil {
		return "", "" // not here yet: try again next time
	}
	li := listIcon{}
	if files, err := catalog.ReadPackage(ia.ID, b); err == nil {
		if m, err := catalog.ParseManifest(files["manifest.yaml"]); err == nil && m.App.Icon != "" {
			if raw, ok := files[m.App.Icon]; ok && len(raw) <= maxListIcon {
				li.icon, li.typ = iconView(m.App.Icon, raw)
			}
		}
	}
	c.icons.Store(key, li)
	return li.icon, li.typ
}
