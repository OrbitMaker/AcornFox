package macvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// State is what the daemon needs to know about the machine.
type State struct {
	Exists  bool
	Running bool
	IP      string
}

// Probe is what a look inside the running machine found.
type Probe struct {
	IP        string
	Listening []int // managed ports that listen on all interfaces
	LocalOnly []int // ports of apps that only open on this computer
	AppsOK    bool  // the list of apps was read
	Busy      bool  // an app is being installed or upgraded
	Complete  bool  // the whole output of ProbeScript came back (a look cut short by a stop is not trusted)
}

// VM is the machine as the daemon sees it. CtrVM is the real one.
type VM interface {
	Status(ctx context.Context) (State, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Probe(ctx context.Context) (Probe, error)
	// Ready says whether AcornFox in the machine answers (right after a start it takes
	// a few seconds longer than sshd does).
	Ready(ctx context.Context) bool
}

// ProbeScript is run inside the machine (as root, on standard input). It
// prints four sections: the address of the machine, the listening TCP
// sockets, the apps (acornfox apps --json) and the running install tasks.
// Read only.
const ProbeScript = `echo ==IP==
ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | head -1
echo ==TCP==
cat /proc/net/tcp /proc/net/tcp6 2>/dev/null
echo ==APPS==
/usr/local/bin/acornfox apps --json 2>/dev/null
echo
echo ==TASKS==
curl -s -m 5 --unix-socket /run/acornfox/api.sock http://acornfox/v1/catalog/install 2>/dev/null
echo
echo ==END==
`

var (
	appURLPortRE = regexp.MustCompile(`:(\d+)(?:/|$)`)
	ipLineRE     = regexp.MustCompile(`(\d+\.\d+\.\d+\.\d+)`)
)

// ParseProbe turns the output of ProbeScript into a Probe.
func ParseProbe(out string) Probe {
	secs := map[string]string{}
	cur := ""
	complete := false
	var b strings.Builder
	flush := func() {
		if cur != "" {
			secs[cur] = b.String()
		}
		b.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimRight(line, "\r")
		if strings.HasSuffix(t, "==END==") && !strings.HasPrefix(t, "==") {
			// the last command's output had no newline: the marker is stuck to it
			b.WriteString(strings.TrimSuffix(t, "==END=="))
			b.WriteString("\n")
			t = "==END=="
		}
		if strings.HasPrefix(t, "==") && strings.HasSuffix(t, "==") && len(t) > 4 {
			flush()
			cur = strings.Trim(t, "=")
			if cur == "END" {
				complete = true
			}
			continue
		}
		b.WriteString(t)
		b.WriteString("\n")
	}
	flush()
	var p Probe
	p.Complete = complete
	if m := ipLineRE.FindString(secs["IP"]); m != "" {
		p.IP = m
	}
	// Listening sockets: state 0A is LISTEN; a local address of all zeros is "all interfaces".
	seen := map[int]bool{}
	for _, line := range strings.Split(secs["TCP"], "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		a := strings.Split(f[1], ":")
		if len(a) != 2 || strings.Trim(a[0], "0") != "" {
			continue
		}
		port, err := strconv.ParseInt(a[1], 16, 32)
		if err != nil || !Managed(int(port)) || seen[int(port)] {
			continue
		}
		seen[int(port)] = true
		p.Listening = append(p.Listening, int(port))
	}
	sort.Ints(p.Listening)
	if body := strings.TrimSpace(secs["APPS"]); strings.HasPrefix(body, "{") {
		var j struct {
			OK   bool `json:"ok"`
			Apps []struct {
				URL       string `json:"url"`
				LocalOnly bool   `json:"local_only"`
			} `json:"apps"`
		}
		if json.Unmarshal([]byte(body), &j) == nil && j.OK {
			p.AppsOK = true
			for _, a := range j.Apps {
				if !a.LocalOnly {
					continue
				}
				if m := appURLPortRE.FindStringSubmatch(a.URL); m != nil {
					n, _ := strconv.Atoi(m[1])
					p.LocalOnly = append(p.LocalOnly, n)
				}
			}
			sort.Ints(p.LocalOnly)
		}
	}
	if t := secs["TASKS"]; strings.Contains(t, `"status":"running"`) || strings.Contains(t, `"status":"queued"`) || strings.Contains(t, `"status": "running"`) {
		p.Busy = true
	}
	return p
}

// WantedPorts is the list of ports the daemon should listen on for a probe:
// every managed port the machine listens on, "LAN" unless its app is
// only-this-computer. When the list of apps could not be read, only the LAN
// console is taken (nothing else is guessed) and old[...] ports stay as they were.
func WantedPorts(p Probe, old []PortSpec) []PortSpec {
	local := map[int]bool{}
	for _, n := range p.LocalOnly {
		local[n] = true
	}
	var out []PortSpec
	for _, port := range p.Listening {
		if !p.AppsOK && port != LANConsolePort {
			continue
		}
		out = append(out, PortSpec{Port: port, LAN: !local[port]})
	}
	if !p.AppsOK {
		for _, o := range old {
			if o.Port != LANConsolePort {
				out = append(out, o)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	}
	return out
}

// CtrVM is the real machine, driven through Apple's container.
type CtrVM struct {
	C    *Container
	Name string
	// Lock, when set, is taken around every start, stop and look inside (see LockVM).
	Lock func() (unlock func(), err error)
	// WaitReady is how long Start waits for the machine to answer and to get
	// an address (default 2 minutes).
	WaitReady time.Duration
}

// Status implements VM.
func (v *CtrVM) Status(ctx context.Context) (State, error) {
	m, ok, err := v.C.Find(ctx, v.Name)
	if err != nil {
		return State{}, err
	}
	if !ok {
		return State{}, nil
	}
	return State{Exists: true, Running: m.Running, IP: m.IP}, nil
}

func (v *CtrVM) lock() (func(), error) {
	if v.Lock == nil {
		return func() {}, nil
	}
	return v.Lock()
}

// errNotRunning: a look inside was asked for while the machine is not running.
var errNotRunning = errors.New("虚拟机没在运行")

// RunScript runs a script as root in the machine and returns what it printed.
// It never boots a stopped machine: with the lock held it first checks that the
// machine is running (`container machine run` would start a stopped one).
func (v *CtrVM) RunScript(ctx context.Context, script string) (Result, error) {
	unlock, err := v.lock()
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	st, err := v.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !st.Running {
		return Result{}, errNotRunning
	}
	return v.C.Script(ctx, v.Name, script, nil)
}

// Start boots the machine (the container service first when it is not
// running) and returns when it answers and has an address. When Apple's
// container is left with a half-started instance of this machine ("hostname(s)
// already exist"), that instance's process is ended and the start tried once more.
func (v *CtrVM) Start(ctx context.Context) error {
	unlock, err := v.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if !v.C.SystemRunning(ctx) {
		if err := v.C.SystemStart(ctx); err != nil {
			return fmt.Errorf("苹果的 container 服务启动不了：%w", err)
		}
	}
	wait := v.WaitReady
	if wait <= 0 {
		wait = 2 * time.Minute
	}
	deadline := time.Now().Add(wait)
	var res Result
	for attempt := 0; ; attempt++ {
		res, err = v.C.Script(ctx, v.Name, "echo afx-up\n", nil)
		if err != nil {
			return err
		}
		if res.Code == 0 && strings.Contains(res.Stdout, "afx-up") {
			break
		}
		msg := res.Stderr + res.Stdout
		if attempt == 0 && strings.Contains(msg, "already exist") {
			v.recoverStale(ctx)
			continue
		}
		return fmt.Errorf("虚拟机没有应答（退出码 %d）：%s", res.Code, firstLine(msg))
	}
	for {
		st, err := v.Status(ctx)
		if err == nil && st.Running && st.IP != "" {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("虚拟机起来了但一直没有拿到地址")
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
}

// recoverStale clears the half-started instance: stop the machine, end the
// runtime process of this machine (only this machine's: its folder name starts
// with the machine's name), give Apple's container a moment.
func (v *CtrVM) recoverStale(ctx context.Context) {
	_ = v.C.Stop(ctx, v.Name)
	_, _ = Capture(ctx, v.C.runner(), nil, "pkill", "-f", "container-runtime-linux start --root .*/containers/"+regexp.QuoteMeta(v.Name)+"-")
	_ = sleepCtx(ctx, 3*time.Second)
}

// Ready implements VM.
func (v *CtrVM) Ready(ctx context.Context) bool {
	res, err := v.RunScript(ctx, "curl -s -m 3 --unix-socket /run/acornfox/api.sock http://acornfox/v1/status >/dev/null && echo afx-ready\n")
	return err == nil && strings.Contains(res.Stdout, "afx-ready")
}

// Stop implements VM.
func (v *CtrVM) Stop(ctx context.Context) error {
	unlock, err := v.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return v.C.Stop(ctx, v.Name)
}

// Probe implements VM. It never boots a stopped machine.
func (v *CtrVM) Probe(ctx context.Context) (Probe, error) {
	res, err := v.RunScript(ctx, ProbeScript)
	if err != nil {
		return Probe{}, err
	}
	if res.Code != 0 {
		return Probe{}, fmt.Errorf("看虚拟机里的情况退出码 %d", res.Code)
	}
	p := ParseProbe(res.Stdout)
	if !p.Complete {
		return Probe{}, errors.New("看虚拟机里的情况没有看完（多半是它正在停）")
	}
	if p.IP == "" {
		if st, err := v.Status(ctx); err == nil {
			p.IP = st.IP
		}
	}
	return p, nil
}
