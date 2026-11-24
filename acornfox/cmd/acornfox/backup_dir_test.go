package main

import (
	"os"
	"regexp"
	"testing"
)

// The app data backups must not share the directory upgrade.sh uses for its
// own copies (it creates it as root and prunes all but the newest 5 entries).
func TestAppBackupsDirNotUpgradeBackups(t *testing.T) {
	b, err := os.ReadFile("../../../scripts/install/upgrade.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^BACKUP_DIR="/var/lib/acornfox/([^"/]+)"`).FindSubmatch(b)
	if m == nil {
		t.Fatal("upgrade.sh 里找不到 BACKUP_DIR")
	}
	if string(m[1]) == AppBackupsDirName {
		t.Fatalf("应用备份目录 %q 和升级脚本自己的备份目录是同一个", AppBackupsDirName)
	}
}
