package catalog

import (
	"gopkg.in/yaml.v3"
)

// A former manifest is one written in a format before ManifestSchema. This
// platform does not install it and does not convert it: the app has to be
// published again in the current format. Apps installed with one earlier
// keep running and can be started, stopped and deleted, but stay out of
// everything that came with the current format (services, connecting apps).

// ManifestSchemaOf returns the "schema" written in manifest.yaml bytes, or 0
// when it cannot be read. It reads only that one field and tolerates
// anything else in the file, so it works on a manifest of any format.
func ManifestSchemaOf(raw []byte) int {
	var doc struct {
		Schema any `yaml:"schema"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return 0
	}
	if n, ok := doc.Schema.(int); ok {
		return n
	}
	return 0
}

// FormerManifest reports whether manifest.yaml bytes are of a format before
// ManifestSchema, and which one. A manifest whose schema cannot be read is
// not called former: the schema check says what is wrong with it.
func FormerManifest(raw []byte) (int, bool) {
	n := ManifestSchemaOf(raw)
	return n, n > 0 && n < ManifestSchema
}

// FormerIndexApp reports whether an index entry is an app published in a
// former format, without downloading its package: every manifest of the
// current format needs at least BasePlatform (catalog lint refuses one that
// declares less), so an entry asking for less was published before it.
func FormerIndexApp(a IndexApp) bool {
	return platformVersionRe.MatchString(a.MinPlatform) && CompareVersions(a.MinPlatform, BasePlatform) < 0
}

// NothingToInstall is what is said, in the list and when an install asks for
// an app that is not there, when the catalog in use has no app this
// platform could install: none at all, or only apps published in a former
// format. That is the state right after an upgrade to a platform of the
// current format (the built-in catalog is empty and newer than the last
// catalog of the former format, so that one is never taken up). Syncing
// cannot help then, so nothing tells the user to sync.
const NothingToInstall = "应用中心现在还没有可以安装的应用，要等应用按新格式重新上架"

// NothingToInstallHint goes with NothingToInstall where an error carries a
// hint of what to do.
const NothingToInstallHint = "不是你的操作不对，现在也没有别的办法装：等应用按新格式重新上架后再装。已经装着的应用照常用"

// HasInstallable reports whether apps has an app this platform could
// install: one not published in a former format (FormerIndexApp).
func HasInstallable(apps []IndexApp) bool {
	for _, a := range apps {
		if !FormerIndexApp(a) {
			return true
		}
	}
	return false
}
