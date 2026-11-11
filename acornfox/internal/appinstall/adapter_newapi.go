package appinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The new-api adapter issues a token in an installed New API for another app
// (provides: llm_gateway, adapter: new-api). The steps were measured on New
// API v1.0.0-rc.48:
//
//  1. POST /api/user/login {username, password} → data.access_token and
//     data.user.id. Management calls then need both
//     "Authorization: Bearer <access_token>" and "New-Api-User: <id>"; the
//     login cookie alone is not enough.
//  2. POST /api/token/ {name, remain_quota:0, unlimited_quota:true,
//     expired_time:-1} creates the token (the answer has no id).
//  3. GET /api/token/?p=0&size=100 lists tokens; take the newest with that name.
//  4. POST /api/token/{id}/key → data.key (48 characters). Clients send it as
//     "sk-<key>".
//
// Right after the install's setup it also turns off self-service sign-up:
// PUT /api/option/ {key: "RegisterEnabled", value: false} (root only; the
// sign-up page then answers "registration has been disabled"; the option is
// kept across restarts).
//
// Base is the gateway's management address reachable from the AcornFox
// server (its loopback port), not the address apps use inside Docker.

type newAPIAdapter struct {
	http *http.Client
}

type newAPISession struct {
	base   string
	token  string
	userID string
	a      *newAPIAdapter
}

type newAPIEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (a *newAPIAdapter) login(ctx context.Context, base, user, pass string) (*newAPISession, error) {
	var data struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID int `json:"id"`
		} `json:"user"`
		ID int `json:"id"`
	}
	s := &newAPISession{base: strings.TrimRight(base, "/"), a: a}
	if err := s.call(ctx, http.MethodPost, "/api/user/login", map[string]string{"username": user, "password": pass}, &data); err != nil {
		return nil, fmt.Errorf("登录 New API 失败：%w", err)
	}
	id := data.User.ID
	if id == 0 {
		id = data.ID
	}
	if data.AccessToken == "" || id == 0 {
		return nil, fmt.Errorf("登录 New API 的应答里没有访问令牌或用户编号")
	}
	s.token, s.userID = data.AccessToken, strconv.Itoa(id)
	return s, nil
}

func (s *newAPISession) call(ctx context.Context, method, p string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+p, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("New-Api-User", s.userID)
	}
	resp, err := s.a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env newAPIEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s %s 返回 HTTP %d，不是 JSON", method, p, resp.StatusCode)
	}
	if !env.Success {
		return fmt.Errorf("%s %s：%s", method, p, env.Message)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// IssueToken creates an unlimited, non-expiring token named name and returns
// its value ("sk-…") and id. It spends from the user's own New API, whose
// channels and quotas the user controls; the token is revoked when the app
// it was issued for is removed with its data (or its install fails). An app
// deleted with its data kept keeps its token, which a later install on that
// data takes over (CheckToken checks it is still the same token).
func (a *newAPIAdapter) IssueToken(ctx context.Context, base, user, pass, name string) (string, int, error) {
	s, err := a.login(ctx, base, user, pass)
	if err != nil {
		return "", 0, err
	}
	if err := s.call(ctx, http.MethodPost, "/api/token/", map[string]any{
		"name": name, "remain_quota": 0, "unlimited_quota": true, "expired_time": -1,
	}, nil); err != nil {
		return "", 0, fmt.Errorf("建令牌失败：%w", err)
	}
	// The create answer has no id: look it up by name. Once more after a
	// moment when it is not listed yet, so a created token is not left behind
	// unknown (an unknown id could not be removed on failure).
	id := 0
	for attempt := 0; attempt < 2 && id == 0; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", 0, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		var list json.RawMessage
		if err := s.call(ctx, http.MethodGet, "/api/token/?p=0&size=100", nil, &list); err != nil {
			return "", 0, fmt.Errorf("列令牌失败：%w", err)
		}
		id = newestTokenID(list, name)
	}
	if id == 0 {
		return "", 0, fmt.Errorf("刚建的令牌 %s 在列表里找不到，请在 New API 里查看并删掉它", name)
	}
	var key struct {
		Key string `json:"key"`
	}
	if err := s.call(ctx, http.MethodPost, "/api/token/"+strconv.Itoa(id)+"/key", nil, &key); err != nil {
		return "", id, fmt.Errorf("取令牌值失败：%w", err)
	}
	if key.Key == "" {
		return "", id, fmt.Errorf("令牌值是空的")
	}
	k := key.Key
	if !strings.HasPrefix(k, "sk-") {
		k = "sk-" + k
	}
	return k, id, nil
}

// Checking a token without revealing it: GET /api/token/{id} answers the
// token with its value masked ("abcd**********wxyz": the first and last 4
// characters) plus its name and status (1 = enabled), or success false
// ("record not found") when there is no such token. Unlike POST
// /api/token/{id}/key (and login), it is not in New API's limit for
// critical calls (20 per 20 minutes from one address, which every call
// from this server shares), so a check costs one login only.

// tokenInfo is GET /api/token/{id} (the value masked).
type tokenInfo struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Key    string `json:"key"`
	Status int    `json:"status"`
}

// tokenStatusEnabled is New API's status of a usable token.
const tokenStatusEnabled = 1

func (s *newAPISession) getToken(ctx context.Context, id int) (tokenInfo, error) {
	var t tokenInfo
	if err := s.call(ctx, http.MethodGet, "/api/token/"+strconv.Itoa(id), nil, &t); err != nil {
		return tokenInfo{}, fmt.Errorf("查令牌失败：%w", err)
	}
	return t, nil
}

// CheckToken reports whether token id is still the token whose value is
// want (the masked value New API shows matches it), and its status. ok is
// false, with no error, when the gateway has no such token or it is another
// one now.
func (a *newAPIAdapter) CheckToken(ctx context.Context, base, user, pass string, id int, want string) (ok bool, status int, err error) {
	s, err := a.login(ctx, base, user, pass)
	if err != nil {
		return false, 0, err
	}
	t, err := s.getToken(ctx, id)
	if err != nil {
		return false, 0, nil // gone (or unreadable): not usable
	}
	return maskedTokenMatch(t.Key, want), t.Status, nil
}

// RevokeIfSame deletes token id only when it is still the token whose value
// is want: a gateway set up anew may use the number for another app's
// token. revoked is false, with no error, when it is gone or another one.
func (a *newAPIAdapter) RevokeIfSame(ctx context.Context, base, user, pass string, id int, want string) (revoked bool, err error) {
	s, err := a.login(ctx, base, user, pass)
	if err != nil {
		return false, err
	}
	t, err := s.getToken(ctx, id)
	if err != nil || !maskedTokenMatch(t.Key, want) {
		return false, nil
	}
	if err := s.call(ctx, http.MethodDelete, "/api/token/"+strconv.Itoa(id), nil, nil); err != nil {
		return false, err
	}
	return true, nil
}

// maskedTokenMatch reports whether the value New API shows for a token
// (masked like "abcd**********wxyz", with or without "sk-") is full. At least
// 8 characters must be shown to tell: with fewer it says no. A value shown
// unmasked must be equal.
func maskedTokenMatch(masked, full string) bool {
	masked, full = strings.TrimPrefix(masked, "sk-"), strings.TrimPrefix(full, "sk-")
	if masked == "" || full == "" {
		return false
	}
	i := strings.Index(masked, "*")
	if i < 0 {
		return masked == full
	}
	pre, suf := masked[:i], masked[strings.LastIndex(masked, "*")+1:]
	if len(pre)+len(suf) < 8 || len(full) <= len(pre)+len(suf) {
		return false
	}
	return strings.HasPrefix(full, pre) && strings.HasSuffix(full, suf)
}

// DisableRegistration turns off self-service sign-up in a freshly set up New
// API (PUT /api/option/ {key: RegisterEnabled, value: false}; root only).
// Upstream allows anyone who reaches the page to register an account by
// default; with sign-up off only the admin the install created can log in,
// so opening the app on the LAN does not let other devices make accounts.
func (a *newAPIAdapter) DisableRegistration(ctx context.Context, base, user, pass string) error {
	s, err := a.login(ctx, base, user, pass)
	if err != nil {
		return err
	}
	if err := s.call(ctx, http.MethodPut, "/api/option/", map[string]any{"key": "RegisterEnabled", "value": false}, nil); err != nil {
		return fmt.Errorf("关闭自助注册失败：%w", err)
	}
	return nil
}

// DeleteToken removes a token issued by IssueToken (cleanup after a failed
// install). Best effort.
func (a *newAPIAdapter) DeleteToken(ctx context.Context, base, user, pass string, id int) error {
	s, err := a.login(ctx, base, user, pass)
	if err != nil {
		return err
	}
	return s.call(ctx, http.MethodDelete, "/api/token/"+strconv.Itoa(id), nil, nil)
}

// newestTokenID finds the highest id named name in a token list: either
// {"items": [...]} (newer versions) or a bare array.
func newestTokenID(raw json.RawMessage, name string) int {
	type tok struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	var items []tok
	var paged struct {
		Items []tok `json:"items"`
	}
	if json.Unmarshal(raw, &paged) == nil && paged.Items != nil {
		items = paged.Items
	} else {
		_ = json.Unmarshal(raw, &items)
	}
	best := 0
	for _, t := range items {
		if t.Name == name && t.ID > best {
			best = t.ID
		}
	}
	return best
}
