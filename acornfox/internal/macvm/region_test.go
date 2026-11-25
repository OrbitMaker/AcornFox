package macvm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPickBySpeed(t *testing.T) {
	const mb = 1 << 20
	cases := []struct {
		name         string
		gitee, hub   int64
		pick, inNote string
	}{
		{"Gitee 快、GitHub 连得上但慢", 3 * mb, 30 << 10, "gitee", "先用 Gitee"},
		{"GitHub 快", 30 << 10, 3 * mb, "github", "先用 GitHub"},
		{"差不多快（Gitee 不到 1.3 倍）先用 GitHub", 12 * mb / 10, mb, "github", "先用 GitHub"},
		{"Gitee 刚好超过 1.3 倍", 14 * mb / 10, mb, "gitee", "先用 Gitee"},
		{"Gitee 没测通", 0, mb / 5, "github", "Gitee 没测通"},
		{"GitHub 没测通", mb, 0, "gitee", "GitHub 没测通"},
		{"都没测通", 0, 0, "", "都没取到数据"},
	}
	for _, c := range cases {
		r := pickBySpeed(c.gitee, c.hub)
		if r.Pick != c.pick || !strings.Contains(r.Note, c.inNote) || !strings.HasPrefix(r.Note, "测速：") {
			t.Errorf("%s：得到 %q %q", c.name, r.Pick, r.Note)
		}
	}
	if r := pickBySpeed(2<<20, 1<<20); !strings.Contains(r.Note, "Gitee 2.00 MB/s、GitHub 1.00 MB/s") {
		t.Errorf("写法：%q", r.Note)
	}
}

// slowServer 模拟一个站：按 rate（字节/秒，0 = 不限）发 8 MiB；ignoreRange 像真的 Gitee 一样不认 Range；fail 时一律 500。
func slowServer(rate int, ignoreRange bool, fail *atomic.Int32, hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if fail != nil && fail.Load() > 0 {
			fail.Add(-1)
			w.WriteHeader(500)
			return
		}
		total := 8 << 20
		n := total
		if r.Header.Get("Range") != "" && !ignoreRange {
			n = probeBytes
			w.WriteHeader(206)
		}
		fl, _ := w.(http.Flusher)
		buf := make([]byte, 16384)
		for sent := 0; sent < n; {
			c := len(buf)
			if n-sent < c {
				c = n - sent
			}
			if _, err := w.Write(buf[:c]); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			sent += c
			if rate > 0 {
				time.Sleep(time.Duration(float64(c) / float64(rate) * float64(time.Second)))
			}
		}
	}))
}

func TestProbeSourcesSlowGithub(t *testing.T) {
	gitee := slowServer(0, true, nil, nil)
	defer gitee.Close()
	hub := slowServer(30000, false, nil, nil)
	defer hub.Close()
	r := ProbeSources(t.Context(), http.DefaultClient, gitee.URL+"/f", hub.URL+"/f", 2*time.Second)
	if r.Pick != "gitee" {
		t.Fatalf("GitHub 连得上但很慢，应先用 Gitee：%+v", r)
	}
	if r.GiteeBps < 10*r.GithubBps {
		t.Errorf("速度差不对：%+v", r)
	}
}

func TestProbeSourcesRetriesTheOneWithoutData(t *testing.T) {
	var fail, hits atomic.Int32
	fail.Store(1) // 第一次 500，第二次好（Gitee 偶尔卡一下，P-847）
	gitee := slowServer(0, true, &fail, &hits)
	defer gitee.Close()
	hub := slowServer(30000, false, nil, nil)
	defer hub.Close()
	r := ProbeSources(t.Context(), http.DefaultClient, gitee.URL+"/f", hub.URL+"/f", 2*time.Second)
	if hits.Load() != 2 {
		t.Errorf("没取到数据的 Gitee 应再试一次，共 %d 次", hits.Load())
	}
	if r.Pick != "gitee" {
		t.Errorf("重试成功后 Gitee 快，应先用 Gitee：%+v", r)
	}
}

func TestProbeSourcesNeitherWorks(t *testing.T) {
	var f1, f2 atomic.Int32
	f1.Store(10)
	f2.Store(10)
	gitee := slowServer(0, true, &f1, nil)
	defer gitee.Close()
	hub := slowServer(0, false, &f2, nil)
	defer hub.Close()
	r := ProbeSources(t.Context(), http.DefaultClient, gitee.URL+"/f", hub.URL+"/f", time.Second)
	if r.Pick != "" || !strings.Contains(r.Note, "都没取到数据") {
		t.Errorf("两边都 500：%+v", r)
	}
}

// 没写 --china / --global：测速，快的先用，并把判断交给虚拟机里的安装脚本。
func TestChinaFromSpeedTest(t *testing.T) {
	gitee := slowServer(0, true, nil, nil)
	defer gitee.Close()
	hub := slowServer(30000, false, nil, nil)
	defer hub.Close()
	t.Setenv("ACORNFOX_GITEE_HOST", gitee.URL)
	t.Setenv("ACORNFOX_GITHUB_HOST", hub.URL)
	t.Setenv("ACORNFOX_PROBE_SECONDS", "2")
	in, _ := newInstaller(t, goodHost(), &fakeRunner{})
	chinaCache.set, chinaCache.decided, chinaCache.noted = false, false, false
	if !in.china() {
		t.Fatal("Gitee 快，应当先用国内")
	}
	if got := in.regionArg(); got != " --china" {
		t.Errorf("要把判断交给虚拟机里的脚本：%q", got)
	}
	urls := in.releaseURLs("v0.7.0", "install.sh")
	if !strings.Contains(urls[0], "gitee.com") {
		t.Errorf("Gitee 先：%v", urls)
	}

	// 反过来：GitHub 快
	gitee2 := slowServer(30000, true, nil, nil)
	defer gitee2.Close()
	hub2 := slowServer(0, false, nil, nil)
	defer hub2.Close()
	t.Setenv("ACORNFOX_GITEE_HOST", gitee2.URL)
	t.Setenv("ACORNFOX_GITHUB_HOST", hub2.URL)
	chinaCache.set, chinaCache.decided = false, false
	in2, _ := newInstaller(t, goodHost(), &fakeRunner{})
	if in2.china() {
		t.Error("GitHub 快，不该先用国内")
	}
	if got := in2.regionArg(); got != " --global" {
		t.Errorf("--global：%q", got)
	}
	chinaCache.set = false
}

func TestRegionArgExplicitAndUndecided(t *testing.T) {
	in, _ := newInstaller(t, goodHost(), &fakeRunner{})
	in.Opt.Global = true
	if got := in.regionArg(); got != " --global" {
		t.Errorf("用户写了 --global：%q", got)
	}
	in.Opt.Global, in.Opt.China = false, true
	if got := in.regionArg(); got != " --china" {
		t.Errorf("用户写了 --china：%q", got)
	}
	// 两边都没测通：先用 Gitee，但不往下传
	in.Opt.China = false
	chinaCache.set, chinaCache.decided, chinaCache.v = true, false, true
	if got := in.regionArg(); got != "" || !in.china() {
		t.Errorf("没测通：不传判断、先用 Gitee：%q", got)
	}
	chinaCache.set = false
}
