package macvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Result is what a finished command printed.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Runner runs a program. The real one runs it; tests give a fake one.
type Runner interface {
	// Run runs name with args, feeding stdin (may be nil) and sending the
	// output to stdout and stderr (nil discards). A program that ran and
	// exited with a non-zero code is not an error: look at code. An error
	// means it could not be started or the context ended.
	Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (code int, err error)
}

// ExecRunner runs real programs.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = stdout, stderr, stdin
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// Capture runs a program and collects what it printed.
func Capture(ctx context.Context, r Runner, stdin io.Reader, name string, args ...string) (Result, error) {
	var out, errb bytes.Buffer
	code, err := r.Run(ctx, stdin, &out, &errb, name, args...)
	return Result{Stdout: out.String(), Stderr: errb.String(), Code: code}, err
}

// Machine is one container machine as `container machine list` shows it.
type Machine struct {
	Name    string
	Running bool
	State   string
	IP      string
}

// Container drives Apple's `container` command line.
type Container struct {
	Bin string // path of the container program; "container" when empty
	R   Runner
}

func (c *Container) bin() string {
	if c.Bin == "" {
		return "container"
	}
	return c.Bin
}

func (c *Container) run(ctx context.Context, stdin io.Reader, args ...string) (Result, error) {
	return Capture(ctx, c.runner(), stdin, c.bin(), args...)
}

func (c *Container) runner() Runner {
	if c.R == nil {
		return ExecRunner{}
	}
	return c.R
}

// Version is the version `container --version` prints ("1.7.1"), or an error
// when the program is not there.
func (c *Container) Version(ctx context.Context) (string, error) {
	res, err := c.run(ctx, nil, "--version")
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("container --version 退出码 %d", res.Code)
	}
	m := regexp.MustCompile(`version\s+(\d+(?:\.\d+)*)`).FindStringSubmatch(res.Stdout)
	if m == nil {
		return strings.TrimSpace(res.Stdout), nil
	}
	return m[1], nil
}

// SystemRunning says whether Apple's container service is running.
func (c *Container) SystemRunning(ctx context.Context) bool {
	res, err := c.run(ctx, nil, "system", "status")
	if err != nil || res.Code != 0 {
		return false
	}
	return regexp.MustCompile(`(?m)^status\s+running\b`).MatchString(res.Stdout)
}

// SystemStart starts the service without asking about the kernel (the kernel
// is installed by the installer, never by a prompt).
func (c *Container) SystemStart(ctx context.Context) error {
	res, err := c.run(ctx, nil, "system", "start", "--disable-kernel-install")
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("container system start 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	return nil
}

// Machines lists the container machines.
func (c *Container) Machines(ctx context.Context) ([]Machine, error) {
	res, err := c.run(ctx, nil, "machine", "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("container machine list 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	return ParseMachines([]byte(res.Stdout))
}

// Find returns the machine called name; ok is false when there is none.
func (c *Container) Find(ctx context.Context, name string) (Machine, bool, error) {
	ms, err := c.Machines(ctx)
	if err != nil {
		return Machine{}, false, err
	}
	for _, m := range ms {
		if m.Name == name {
			return m, true, nil
		}
	}
	return Machine{}, false, nil
}

// Stop stops a machine (about 10 seconds).
func (c *Container) Stop(ctx context.Context, name string) error {
	res, err := c.run(ctx, nil, "machine", "stop", name)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("container machine stop 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	return nil
}

// Delete deletes a machine and everything in it.
func (c *Container) Delete(ctx context.Context, name string) error {
	res, err := c.run(ctx, nil, "machine", "delete", name)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("container machine delete 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	return nil
}

// Script runs a shell script as root in the machine, booting it when it is
// stopped. The script goes in on standard input ("bash -s"), never as
// arguments: container machine run drops the arguments of the first command
// (measured on macOS 26). extra, when not nil, is appended to the script on the
// same input (a script can read it with cat).
func (c *Container) Script(ctx context.Context, name, script string, extra io.Reader) (Result, error) {
	var in io.Reader = strings.NewReader(script)
	if extra != nil {
		in = io.MultiReader(in, extra)
	}
	return c.run(ctx, in, "machine", "run", "-n", name, "--root", "-i", "--", "bash", "-s")
}

// ScriptStream is Script with the output sent on as it comes (an install that
// takes minutes). It returns the exit code.
func (c *Container) ScriptStream(ctx context.Context, name, script string, extra io.Reader, stdout, stderr io.Writer) (int, error) {
	var in io.Reader = strings.NewReader(script)
	if extra != nil {
		in = io.MultiReader(in, extra)
	}
	return c.runner().Run(ctx, in, stdout, stderr, c.bin(), "machine", "run", "-n", name, "--root", "-i", "--", "bash", "-s")
}

// ParseMachines reads the JSON of `container machine list --format json`.
// It accepts a list of objects or an object with the list in it, and finds the
// fields by their meaning (the name, the state, the IPv4 address) because the
// exact layout is Apple's to change.
func ParseMachines(b []byte) ([]Machine, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, nil
	}
	var any0 any
	if err := json.Unmarshal(b, &any0); err != nil {
		return nil, fmt.Errorf("container machine list 的输出看不懂：%w", err)
	}
	var items []any
	switch v := any0.(type) {
	case []any:
		items = v
	case map[string]any:
		for _, key := range []string{"machines", "items", "data"} {
			if l, ok := v[key].([]any); ok {
				items = l
				break
			}
		}
		if items == nil {
			items = []any{v}
		}
	}
	var out []Machine
	for _, it := range items {
		obj, ok := it.(map[string]any)
		if !ok {
			continue
		}
		m := Machine{Name: findString(obj, "id", "name"), State: findString(obj, "status", "state")}
		m.State = strings.ToLower(m.State)
		m.Running = m.State == "running"
		m.IP = findIPv4(obj)
		if m.Name != "" {
			out = append(out, m)
		}
	}
	return out, nil
}

// findString finds the first string value under one of keys, looking at the
// top level first and then one level into the objects below it.
func findString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := obj[k].(string); ok && s != "" {
			return s
		}
	}
	for _, v := range obj {
		if sub, ok := v.(map[string]any); ok {
			for _, k := range keys {
				if s, ok := sub[k].(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

var ipv4RE = regexp.MustCompile(`^(\d{1,3}(?:\.\d{1,3}){3})(?:/\d+)?$`)

// findIPv4 finds the machine's own IPv4 address: a string that is one, under
// a key that says address or ip, anywhere in the object.
func findIPv4(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			lk := strings.ToLower(k)
			if s, ok := val.(string); ok && (strings.Contains(lk, "address") || strings.Contains(lk, "ip")) && !strings.Contains(lk, "gateway") {
				if m := ipv4RE.FindStringSubmatch(s); m != nil && m[1] != "0.0.0.0" {
					return m[1]
				}
			}
		}
		for _, val := range x {
			if ip := findIPv4(val); ip != "" {
				return ip
			}
		}
	case []any:
		for _, val := range x {
			if ip := findIPv4(val); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// sleepCtx sleeps for d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
