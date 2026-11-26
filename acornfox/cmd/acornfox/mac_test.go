package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/macvm"
)

func runMacCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(append([]string{"mac"}, args...), strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func TestMacHelpAndUsage(t *testing.T) {
	t.Setenv("ACORNFOX_MAC_HOME", t.TempDir())
	code, out, _ := runMacCmd(t, "--help")
	if code != 0 || !strings.Contains(out, "acornfox mac") || !strings.Contains(out, "uninstall") || !strings.Contains(out, "--plan") {
		t.Fatalf("帮助不对：%d %q", code, out)
	}
	if code, _, _ := runMacCmd(t); code != 2 {
		t.Errorf("没有动作应是用法错误，退出码 %d", code)
	}
	if code, _, errb := runMacCmd(t, "bogus"); code != 2 || !strings.Contains(errb, "不认识") {
		t.Errorf("不认识的动作：%d %q", code, errb)
	}
	if code, _, _ := runMacCmd(t, "set"); code != 2 {
		t.Errorf("set 什么都不改应是用法错误，退出码 %d", code)
	}
	if code, _, _ := runMacCmd(t, "set", "--idle", "abc"); code != 2 {
		t.Errorf("--idle abc 应是用法错误，退出码 %d", code)
	}
	if code, _, _ := runMacCmd(t, "install", "--china", "--global"); code != 2 {
		t.Errorf("--china 和 --global 同时写应是用法错误，退出码 %d", code)
	}
}

func TestMacStatusNotInstalled(t *testing.T) {
	t.Setenv("ACORNFOX_MAC_HOME", t.TempDir())
	code, out, _ := runMacCmd(t, "status", "--json")
	if code != 1 {
		t.Errorf("没装时退出码应是 1：%d", code)
	}
	var st macvm.Status
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &st); err != nil || st.Installed {
		t.Fatalf("没装时 installed 应为 false：%v %q", err, out)
	}
	if macVMInstalled() {
		t.Error("没有 mac.json 不算装了")
	}
}

func TestMacLocalStopLetsDoctorThroughWhenInstalled(t *testing.T) {
	// Where the old local mode is off (a Mac build), doctor is only stopped when no virtual machine is installed.
	home := t.TempDir()
	t.Setenv("ACORNFOX_MAC_HOME", home)
	cfg := macvm.DefaultConfig(18 << 30)
	if err := cfg.Save(macvm.Paths{Root: home}); err != nil {
		t.Fatal(err)
	}
	if !macVMInstalled() {
		t.Fatal("有 mac.json 应算装了")
	}
	var out, errb bytes.Buffer
	if _, stopped := macLocalStop("doctor", false, &out, &errb); stopped {
		t.Error("装了 Mac 本机版时 doctor 不该被拦")
	}
}

// --from (the install source tag) is a known option of acornfox mac install; a bad one is dropped with a
// line on standard error and the command still runs (here as a dry run).
func TestMacInstallAcceptsTheSourceTag(t *testing.T) {
	t.Setenv("ACORNFOX_MAC_HOME", t.TempDir())
	if code, _, errb := runMacCmd(t, "install", "--plan", "--json", "--from", "bili-1201"); code == 2 || strings.Contains(errb, "flag provided but not defined") {
		t.Fatalf("--from 应该是认得的选项：%d %q", code, errb)
	}
	_, _, errb := runMacCmd(t, "install", "--plan", "--json", "--from", "Bad_Tag")
	if !strings.Contains(errb, "不合规") || !strings.Contains(errb, "照常安装") {
		t.Fatalf("不合规的来源标记要提醒一句：%q", errb)
	}
	var c macCommon
	fs := macFlags("install", &c, &bytes.Buffer{})
	if err := fs.Parse([]string{"--from", "Bad_Tag"}); err != nil {
		t.Fatal(err)
	}
	if err := c.finish(); err != nil || c.opt.From != "" {
		t.Fatalf("不合规的标记当没有：%v %q", err, c.opt.From)
	}
}
