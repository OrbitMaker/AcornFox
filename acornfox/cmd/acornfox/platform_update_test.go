package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platformupdate"
)

func TestDoctorPlatformUpdate(t *testing.T) {
	// 有新版本：一行提醒（!），不算失败。
	r := doctorReport{OK: true}
	doctorPlatformUpdate(&r, &client.PlatformUpdate{Enabled: true, Current: "0.7.0", Latest: "0.7.1", Available: true})
	if !r.OK || len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "PLAT-UPD-01" ||
		!strings.Contains(r.Items[0].Text, "0.7.1") || !strings.Contains(r.Items[0].Text, "acornfox update") {
		t.Fatalf("有新版本要给一行提醒且不算失败：%+v", r)
	}
	// 一直查不到：提醒（!），不算失败。
	r = doctorReport{OK: true}
	doctorPlatformUpdate(&r, &client.PlatformUpdate{Enabled: true, Current: "0.7.0", Unreachable: true, Failures: 20, Error: "gitee：连不上"})
	if !r.OK || len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "PLAT-UPD-02" || !strings.Contains(r.Items[0].Text, "连不上") {
		t.Fatalf("一直查不到要提醒：%+v", r)
	}
	// 已是最新、偶尔一次失败、关掉了、旧服务器：什么都不说。
	for _, pu := range []*client.PlatformUpdate{
		{Enabled: true, Current: "0.7.1", Latest: "0.7.1"},
		{Enabled: true, Current: "0.7.0", Failures: 2, Error: "连不上"},
		{Enabled: false, Current: "0.7.0", Latest: "0.7.1", Available: true},
		nil,
	} {
		r = doctorReport{OK: true}
		doctorPlatformUpdate(&r, pu)
		if len(r.Items) != 0 {
			t.Errorf("%+v 不该有提醒：%+v", pu, r.Items)
		}
	}
}

func TestNewPlatformUpdaterReadsEnvAndHostKind(t *testing.T) {
	t.Setenv(platformupdate.EnvCheck, "off")
	t.Setenv(platformupdate.EnvHostKind, "")
	dir := t.TempDir()
	pv := filepath.Join(dir, "version")
	if err := os.WriteFile(pv, []byte("Linux version 5.15.1-microsoft-standard-WSL2"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := procVersionFile
	procVersionFile = pv
	defer func() { procVersionFile = old }()

	c := newPlatformUpdater(serverOptions{DataDir: dir, WSL: true})
	st := c.Status()
	if st.Enabled || st.HostKind != platformupdate.HostWindows || !strings.Contains(st.UpgradeCommand, "get.ps1") {
		t.Fatalf("关掉后 enabled=false；WSL 里是 windows，升级命令用 get.ps1：%+v", st)
	}
	t.Setenv(platformupdate.EnvCheck, "")
	if st := newPlatformUpdater(serverOptions{DataDir: dir}).Status(); !st.Enabled || st.HostKind != platformupdate.HostLinux || !strings.Contains(st.UpgradeCommand, "sudo bash -s -- upgrade") {
		t.Fatalf("普通 Linux：%+v", st)
	}
	// WSL 标志在、但不是微软内核：Mac 里的虚拟机。
	if err := os.WriteFile(pv, []byte("Linux version 6.18.56 (kata)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := newPlatformUpdater(serverOptions{DataDir: dir, WSL: true}).Status(); st.HostKind != platformupdate.HostMac || strings.Contains(st.UpgradeCommand, "sudo") {
		t.Fatalf("Mac 虚拟机：%+v", st)
	}
}
