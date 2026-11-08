package main

import "testing"

func TestIsCatalogInstall(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"install"}, false},
		{[]string{"install", "--china"}, false},
		{[]string{"install", "--rollback", "--json"}, false},
		{[]string{"install", "new-api"}, true},
		{[]string{"install", "--recipe", "x"}, true},
		{[]string{"install", "status", "0123456789ab"}, true},
		{[]string{"--json", "install", "new-api"}, true},
		{[]string{"install", "--set", "a=b"}, true},
		{[]string{"install", "-h"}, true},
		{[]string{"install", "--help"}, true},
		{[]string{"install", "--china", "-h"}, true},
	} {
		if got := isCatalogInstall(c.args); got != c.want {
			t.Errorf("%v: %v", c.args, got)
		}
	}
}
