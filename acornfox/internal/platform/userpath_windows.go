//go:build windows

package platform

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// EnsureUserPath appends dir to the current user's PATH (HKCU\Environment)
// when it is not there yet, so "acornfox" works in new terminals. It returns
// true when PATH was changed.
func EnsureUserPath(dir string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	cur, typ, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return false, err
	}
	for _, p := range strings.Split(cur, ";") {
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			return false, nil
		}
	}
	next := dir
	if cur != "" {
		next = strings.TrimRight(cur, ";") + ";" + dir
	}
	if typ == registry.EXPAND_SZ {
		return true, k.SetExpandStringValue("Path", next)
	}
	return true, k.SetStringValue("Path", next)
}

// RemoveUserPath removes dir from the current user's PATH; missing is fine.
func RemoveUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	cur, typ, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}
	var kept []string
	for _, p := range strings.Split(cur, ";") {
		if p != "" && !strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			kept = append(kept, p)
		}
	}
	if typ == registry.EXPAND_SZ {
		return k.SetExpandStringValue("Path", strings.Join(kept, ";"))
	}
	return k.SetStringValue("Path", strings.Join(kept, ";"))
}
