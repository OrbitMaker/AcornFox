package macvm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Choosing between Gitee (mainland China) and GitHub when the person did not say --china / --global.
//
// Same method as get.sh, get.ps1 and install-mac.sh (keep the four in step): take the beginning of the same
// release file from both at the same time (up to 1 MiB, up to a window of 5 seconds) and compare bytes per
// second; the faster is tried first. "GitHub can be reached" is not a good test: from mainland China it often
// can, at 30 KB/s. A window is used instead of only a Range request because Gitee ignores Range and keeps sending.
// A source that brought no data is asked once more (Gitee sometimes stalls in the middle and the next
// connection is fine). Within 1.3 times of each other GitHub is tried first: new files appear there first.

const (
	probeBytes      = 1 << 20
	probeAssetMac   = "acornfox_linux_arm64" // the program that goes into the virtual machine
	probeFasterNeed = 13                     // Gitee has to be more than 13/10 of GitHub to win
)

// SpeedResult is what the speed test found.
type SpeedResult struct {
	GiteeBps  int64
	GithubBps int64
	Pick      string // "gitee", "github", or "" when neither brought data
	Note      string // one line for a person
}

func mbps(b int64) string { return fmt.Sprintf("%.2f", float64(b)/1048576) }

// pickBySpeed turns the two measurements into a decision and the line shown on the screen.
func pickBySpeed(gitee, github int64) SpeedResult {
	r := SpeedResult{GiteeBps: gitee, GithubBps: github}
	switch {
	case gitee == 0 && github == 0:
		r.Note = "测速：Gitee、GitHub 都没取到数据（各试了两次），先用 Gitee，取不到再换 GitHub"
	case github == 0:
		r.Pick, r.Note = "gitee", fmt.Sprintf("测速：Gitee %s MB/s、GitHub 没测通，先用 Gitee", mbps(gitee))
	case gitee == 0:
		r.Pick, r.Note = "github", fmt.Sprintf("测速：Gitee 没测通、GitHub %s MB/s，先用 GitHub", mbps(github))
	case gitee*10 > github*probeFasterNeed:
		r.Pick, r.Note = "gitee", fmt.Sprintf("测速：Gitee %s MB/s、GitHub %s MB/s，先用 Gitee", mbps(gitee), mbps(github))
	default:
		r.Pick, r.Note = "github", fmt.Sprintf("测速：Gitee %s MB/s、GitHub %s MB/s，先用 GitHub", mbps(gitee), mbps(github))
	}
	return r
}

// probeOne reads the beginning of url for at most window and returns bytes per second (0 = nothing came).
func probeOne(ctx context.Context, c *http.Client, url string, window time.Duration) int64 {
	ctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", probeBytes-1))
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0
	}
	n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, probeBytes)) // stops at the limit, or when the window is over
	el := time.Since(start).Seconds()
	if n <= 0 || el <= 0 {
		return 0
	}
	return int64(float64(n) / el)
}

// ProbeSources measures both sources at the same time, then asks the ones that brought nothing once more.
func ProbeSources(ctx context.Context, c *http.Client, giteeURL, githubURL string, window time.Duration) SpeedResult {
	urls := map[string]string{"gitee": giteeURL, "github": githubURL}
	got := map[string]int64{}
	todo := []string{"gitee", "github"}
	for try := 0; try < 2 && len(todo) > 0; try++ {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, s := range todo {
			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				v := probeOne(ctx, c, urls[s], window)
				mu.Lock()
				got[s] = v
				mu.Unlock()
			}(s)
		}
		wg.Wait()
		todo = todo[:0]
		for _, s := range []string{"gitee", "github"} {
			if got[s] == 0 {
				todo = append(todo, s)
			}
		}
	}
	return pickBySpeed(got["gitee"], got["github"])
}

func probeWindow() time.Duration {
	if v := os.Getenv("ACORNFOX_PROBE_SECONDS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Second
}

// probeURLs are the two addresses of the file used for the speed test.
func (in *Installer) probeURLs() (gitee, github string) {
	ver := in.releaseVersion()
	gh := in.Opt.GithubRepo
	if gh == "" {
		gh = "OrbitMaker/AcornFox"
	}
	ge := in.Opt.GiteeRepo
	if ge == "" {
		ge = "VIP13390/AcornFox"
	}
	giteeHost, githubHost := os.Getenv("ACORNFOX_GITEE_HOST"), os.Getenv("ACORNFOX_GITHUB_HOST") // tests: a fake server
	if giteeHost == "" {
		giteeHost = "https://gitee.com"
	}
	if githubHost == "" {
		githubHost = "https://github.com"
	}
	file := "/releases/download/" + ver + "/" + probeAssetMac
	return strings.TrimRight(giteeHost, "/") + "/" + ge + file, strings.TrimRight(githubHost, "/") + "/" + gh + file
}
