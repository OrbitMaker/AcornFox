package gatewayclient

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// W10: private app catalogs.
//
//   - Connect sends a connection code once; the gateway grants this machine
//     the catalog the code was made for. The code itself is never logged or
//     stored by this client.
//   - CatalogIndex asks which private catalogs this machine may see. known is
//     the serial the client holds per catalog; catalogs at that serial come
//     back without their index. Revoked lists catalogs this machine had but
//     may no longer see (access revoked or catalog disabled).
//
// The gateway never checks an index's signature; the caller verifies each
// index with the program's trusted keys before using it.

// Connected is Connect's answer.
type Connected struct {
	CatalogID string `json:"catalog_id"`
	Name      string `json:"name"`
	Subject   string `json:"subject"`
}

// PrivateCatalog is one catalog in CatalogIndex's answer. Index and Sig are
// empty when the client already holds this serial.
type PrivateCatalog struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Serial int64  `json:"serial"`
	Index  string `json:"index,omitempty"`
	Sig    string `json:"sig,omitempty"`
}

// Full reports whether the answer carries the index itself.
func (p PrivateCatalog) Full() bool { return p.Index != "" }

// PrivateIndex is CatalogIndex's answer.
type PrivateIndex struct {
	Catalogs []PrivateCatalog `json:"catalogs"`
	Revoked  []string         `json:"revoked"`
}

var catalogIDRe = regexp.MustCompile(`^cat-[A-Za-z0-9_-]{1,64}$`)

// Connect runs POST /v1/catalog/connect with code. GW-AUTH-02 registers and
// retries once; GW-CAT-04 (too many tries) is returned at once with its
// RetryAfter, never retried here.
func (c *Client) Connect(ctx context.Context, code string) (Connected, error) {
	body, _ := json.Marshal(struct {
		Code         string  `json:"code"`
		AccountToken *string `json:"account_token"`
	}{Code: code})
	var out struct {
		Catalog struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"catalog"`
		Subject string `json:"subject"`
	}
	err := c.retry(ctx, func() error { return c.call(ctx, "/v1/catalog/connect", body, &out, maxAnswer) })
	if err != nil {
		return Connected{}, err
	}
	if !catalogIDRe.MatchString(out.Catalog.ID) {
		return Connected{}, &Error{Code: "GW-RESP-01", Message: "网关返回的目录编号不对"}
	}
	return Connected{CatalogID: out.Catalog.ID, Name: CleanCatalogName(out.Catalog.Name), Subject: out.Subject}, nil
}

// CatalogIndex runs POST /v1/catalog/index with the serials the client holds.
func (c *Client) CatalogIndex(ctx context.Context, known map[string]int64) (PrivateIndex, error) {
	if known == nil {
		known = map[string]int64{}
	}
	body, _ := json.Marshal(struct {
		Known        map[string]int64 `json:"known"`
		AccountToken *string          `json:"account_token"`
	}{Known: known})
	var out PrivateIndex
	err := c.retry(ctx, func() error { return c.call(ctx, "/v1/catalog/index", body, &out, maxIndexAnswer) })
	if err != nil {
		return PrivateIndex{}, err
	}
	for i, p := range out.Catalogs {
		out.Catalogs[i].Name = CleanCatalogName(p.Name)
		if !catalogIDRe.MatchString(p.ID) || p.Serial < 1 || (p.Index != "") != (p.Sig != "") {
			return PrivateIndex{}, &Error{Code: "GW-RESP-01", Message: "网关返回的私有目录条目不对"}
		}
	}
	for _, id := range out.Revoked {
		if !catalogIDRe.MatchString(id) {
			return PrivateIndex{}, &Error{Code: "GW-RESP-01", Message: "网关返回的已撤销目录编号不对"}
		}
	}
	if out.Catalogs == nil {
		out.Catalogs = []PrivateCatalog{}
	}
	if out.Revoked == nil {
		out.Revoked = []string{}
	}
	return out, nil
}

// maxCatalogName is the most runes of a catalog name kept (the gateway takes
// at most 100 when a catalog is published).
const maxCatalogName = 100

// CleanCatalogName makes a catalog name from the gateway safe to store and
// show: the name is not signed, so control and formatting characters (which
// could drive a terminal or reorder text) are dropped, runs of white space
// become one space, and it is cut to 100 characters. It may return "";
// callers then show the catalog id.
func CleanCatalogName(s string) string {
	var out []rune
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
		case r == utf8.RuneError, unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		default:
			if space && len(out) > 0 {
				out = append(out, ' ')
			}
			space = false
			out = append(out, r)
		}
		if len(out) >= maxCatalogName {
			out = out[:maxCatalogName]
			break
		}
	}
	return strings.TrimSpace(string(out))
}
