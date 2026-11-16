// Package platformdomain is this machine's platform domain: one domain name
// for the whole machine (for example apps.example.com) under which every app
// opened to the internet gets a name of its own (hello.apps.example.com),
// with HTTPS and no port to remember.
//
// What it does: keeps the platform domain (Service.Set, Service.Clear; none
// by default), and checks whether it works (Service.View, Service.Check):
// whether a name under it points at this machine (dns), whether the
// machine's 80 and 443 are reachable (ports) and whether the certificates
// were issued (cert). Who gets a name, and what an app's address is, is not
// decided here: platformvar.PlatformHost is that rule, platformvar's
// Host.Address the address, package reconcile puts the names on the entry
// proxy's routes (the entry proxy gets each certificate by itself, the same
// way as for a domain an app has bound itself).
//
// When to use it: the console's "设置 › 访问 › 域名" row, `acornfox domain
// base`, `acornfox doctor` (all through the API: GET/PUT/DELETE
// /v1/platform/domain, POST /v1/platform/domain/check).
//
// Preconditions: a server with a public address. A machine on a local
// network (no public address), and a desktop, cannot use one
// (PDOMAIN-02): certificates are validated from the internet.
//
// Inputs and outputs: Set takes a domain name as the user wrote it
// (Normalize says what is accepted, PDOMAIN-05 otherwise); every method
// returns a View (the JSON of the API). Errors are *Error (HTTP status,
// fault code, message, what to do).
//
// Side effects: Set and Clear write one file in the data directory
// (platform-domain.json, 0600: the domain, when it was set and the last
// changes) and call Config.OnChange (the server hands the entry proxy its
// routes again). Replacing or clearing a platform domain that is set needs
// confirm (PDOMAIN-07): every app's name under the old one stops working.
// Setting the same domain again changes nothing. The checks only read:
// DNS queries to public DNS servers, a TCP connection to this machine's own
// public address on 80 and 443, a TLS handshake with the entry proxy on
// this machine.
//
// Checking it: go test ./internal/platformdomain/ (every check result with
// fake DNS, connections and certificates). To stop using it: Clear (or
// delete the file and restart the server); apps go back to their address
// with the port, certificates already issued stay in the entry proxy's
// volume.
//
// What to look at first when it does not work: the View's problem (fault
// code PDOMAIN-01…07, `acornfox faults PDOMAIN-0x`), then the entry proxy
// (`acornfox doctor`, EDGE-*).
package platformdomain

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/acornfox/acornfox/internal/domainname"
	"github.com/acornfox/acornfox/internal/publicconsole"
)

// Fault codes (internal/faults has the full entries).
const (
	CodeDNS        = "PDOMAIN-01" // a name under the domain does not point at this machine
	CodeNoPublicIP = "PDOMAIN-02" // this machine has no public address (a LAN machine, a desktop)
	CodePorts      = "PDOMAIN-03" // 80 or 443 cannot be reached
	CodeCert       = "PDOMAIN-04" // a certificate was not issued
	CodeInvalid    = "PDOMAIN-05" // not a domain name (a scheme, a path, a port, an address)
	CodeAppName    = "PDOMAIN-06" // an app cannot use its name under the domain
	CodeConfirm    = "PDOMAIN-07" // replacing or clearing it needs confirmation
)

// Check keys, in the order they are run and shown.
const (
	CheckDNS   = "dns"
	CheckPorts = "ports"
	CheckCert  = "cert"
)

// States of a View.
const (
	StateUnset   = "unset"
	StateOK      = "ok"
	StateProblem = "problem"
)

// Error is a refused request: HTTP status, fault code, message and what to do.
type Error struct {
	Status  int
	Code    string
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

// UnsupportedText is what a machine without a public address is told.
const UnsupportedText = "这台机器在局域网里，没有公网地址，用不了域名"

// unsupportedHint is what to do about it.
const unsupportedHint = "域名的证书要从公网验证，局域网里的机器（还有 Mac、Windows 电脑）这一版用不了。要在外面打开应用，用 acornfox open 应用名 经安全通道打开；要用域名，把应用装到有公网地址的云主机上"

// Normalize turns what the user wrote into the platform domain: lower case,
// no trailing dot, a leading "*." dropped (the record is often written that
// way). It refuses (PDOMAIN-05) a scheme, a path, a port, an address, and
// anything that is not a host name with at least one dot.
func Normalize(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	bad := func(why string) error {
		return &Error{Status: http.StatusBadRequest, Code: CodeInvalid,
			Message: fmt.Sprintf("%q 不是域名：%s", strings.TrimSpace(raw), why),
			Hint:    "只填域名本身，例如 apps.example.com（不带 http://、不带路径和端口）"}
	}
	switch {
	case d == "":
		return "", bad("没有填")
	case strings.Contains(d, "://"):
		return "", bad("不要带 http:// 或 https://")
	case strings.ContainsAny(d, "/?#"):
		return "", bad("不要带路径")
	case strings.Contains(d, "@"):
		return "", bad("不要带账号")
	}
	d = strings.TrimSuffix(d, ".")
	d = strings.TrimPrefix(d, "*.")
	if net.ParseIP(strings.Trim(d, "[]")) != nil {
		return "", bad("这是地址，不是域名")
	}
	if strings.Contains(d, ":") {
		return "", bad("不要带端口")
	}
	if !domainname.Valid(d) {
		return "", bad("要是一个完整的域名（字母、数字、短横线，至少有一个点，不超过 253 个字符）")
	}
	return d, nil
}

// Record is the DNS record the user adds at their domain provider: every
// name under the platform domain pointing at this machine.
type Record struct {
	Name  string `json:"name,omitempty"`  // the host record as providers ask for it, e.g. *.apps
	Type  string `json:"type"`            // A or AAAA
	Value string `json:"value,omitempty"` // this machine's public address
	Full  string `json:"full,omitempty"`  // the whole name, e.g. *.apps.example.com
}

// RecordFor is the record for base pointing at ip. base "" gives only the
// type and the value (the console shows the name as an example).
func RecordFor(base, ip string) Record {
	r := Record{Type: "A", Value: ip}
	if a := net.ParseIP(ip); a != nil && a.To4() == nil {
		r.Type = "AAAA"
	}
	if base == "" {
		return r
	}
	host := publicconsole.HostRecord(base)
	r.Name = "*." + host
	if host == "@" {
		r.Name = "*"
	}
	r.Full = "*." + base
	return r
}

// ConsoleHost is the default name of the public console under base.
func ConsoleHost(base string) string {
	if base == "" {
		return ""
	}
	return "console." + base
}
