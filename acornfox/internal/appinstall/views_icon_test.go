package appinstall

import (
	"encoding/base64"
	"testing"
)

func TestIconView(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff}
	cases := []struct {
		name string
		b    []byte
		icon string
		typ  string
	}{
		{"icon.svg", []byte("<svg/>"), "<svg/>", "svg"},
		{"Icon.SVG", []byte("<svg/>"), "<svg/>", "svg"},
		{"icon.png", png, base64.StdEncoding.EncodeToString(png), "png"},
		{"icon.gif", []byte("GIF89a"), "", ""},
	}
	for _, c := range cases {
		icon, typ := iconView(c.name, c.b)
		if icon != c.icon || typ != c.typ {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, icon, typ, c.icon, c.typ)
		}
	}
}
