package macvm

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// PlistPath is where the launchd agent file of label lives for the current user.
func PlistPath(label string) string {
	if v := os.Getenv("ACORNFOX_MAC_PLIST_DIR"); v != "" {
		return filepath.Join(v, label+".plist")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// PlistXML is the launchd agent file of the daemon: it runs
// "<program> mac daemon" from login on, and starts it again when it ends.
func PlistXML(label, program string, paths Paths, env map[string]string) string {
	env2 := map[string]string{"ACORNFOX_MAC_HOME": paths.Root, "PATH": "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"}
	for k, v := range env {
		env2[k] = v
	}
	keys := make([]string, 0, len(env2))
	for k := range env2 {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var e bytes.Buffer
	for _, k := range keys {
		fmt.Fprintf(&e, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", xmlEscape(k), xmlEscape(env2[k]))
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + xmlEscape(label) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(program) + `</string>
		<string>mac</string>
		<string>daemon</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
` + e.String() + `	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(paths.Logs()) + `/daemon.out</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(paths.Logs()) + `/daemon.out</string>
</dict>
</plist>
`
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// LaunchLoaded says whether the agent is loaded in this user's launchd.
func LaunchLoaded(ctx context.Context, r Runner, label string) bool {
	res, err := Capture(ctx, r, nil, "launchctl", "print", domain()+"/"+label)
	return err == nil && res.Code == 0
}

// LaunchLoad loads the agent (taking an older copy out first, so a changed
// file or program takes effect). launchd takes an agent out asynchronously:
// loading again right away fails with "Input/output error", so it waits for
// the old one to be gone and tries the load a few times.
func LaunchLoad(ctx context.Context, r Runner, label, plist string) error {
	if LaunchLoaded(ctx, r, label) {
		_, _ = Capture(ctx, r, nil, "launchctl", "bootout", domain()+"/"+label)
		for i := 0; i < 20 && LaunchLoaded(ctx, r, label); i++ {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return err
			}
		}
	}
	var last string
	for i := 0; i < 5; i++ {
		res, err := Capture(ctx, r, nil, "launchctl", "bootstrap", domain(), plist)
		if err != nil {
			return err
		}
		if res.Code == 0 {
			return nil
		}
		last = fmt.Sprintf("launchctl bootstrap 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
		if LaunchLoaded(ctx, r, label) { // it did get loaded after all
			return nil
		}
		if err := sleepCtx(ctx, time.Second); err != nil {
			return err
		}
	}
	return errors.New(last)
}

// LaunchUnload takes the agent out; it is not an error when it is not loaded.
func LaunchUnload(ctx context.Context, r Runner, label string) error {
	if !LaunchLoaded(ctx, r, label) {
		return nil
	}
	res, err := Capture(ctx, r, nil, "launchctl", "bootout", domain()+"/"+label)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("launchctl bootout 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	return nil
}
