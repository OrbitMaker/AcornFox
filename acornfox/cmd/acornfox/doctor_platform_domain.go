package main

import (
	"context"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platformdomain"
)

// doctorPlatformDomainQuery asks acornfox-server to check the platform
// domain now (POST /v1/platform/domain/check); ok is false when the server
// cannot be asked or is older than the platform domain. Tests replace it.
var doctorPlatformDomainQuery = queryPlatformDomain

func queryPlatformDomain(ctx context.Context, socket string) (client.PlatformDomain, bool) {
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return client.PlatformDomain{}, false
	}
	defer c.Close()
	v, err := c.CheckPlatformDomain(qctx)
	if err != nil {
		return client.PlatformDomain{}, false
	}
	return v, true
}

// doctorPlatformDomain reports the platform domain's three checks when one
// is set (nothing otherwise, and nothing when the server cannot be asked):
// the same codes as the console and `acornfox domain base`.
func doctorPlatformDomain(ctx context.Context, r *doctorReport, socket string) {
	v, ok := doctorPlatformDomainQuery(ctx, socket)
	if !ok || v.Domain == "" {
		return
	}
	if !v.Supported && v.Unsupported != nil {
		r.warn(v.Unsupported.Code, "平台域名 %s：%s。%s", v.Domain, v.Unsupported.Text, v.Unsupported.Next)
		return
	}
	if p := v.Problem; v.State == platformdomain.StateProblem && p != nil {
		text := "平台域名 " + v.Domain + "：" + p.Text
		if p.Detail != "" {
			text += "（" + p.Detail + "）"
		}
		if p.Next != "" {
			text += "。" + p.Next
		}
		r.add(false, p.Code, "%s", text)
		return
	}
	parts := make([]string, 0, len(v.Checks))
	for _, c := range v.Checks {
		parts = append(parts, c.Text)
	}
	r.add(true, platformdomain.CodeDNS, "平台域名 %s：%s", v.Domain, strings.Join(parts, "；"))
}
