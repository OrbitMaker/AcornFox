package platform

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"
)

// MetadataAddress is the instance metadata address almost every cloud and
// VPS provider answers on (AWS, Alibaba, Tencent, Huawei, Volcengine, GCP,
// Azure, Oracle, DigitalOcean, Hetzner, ...). Home and office machines have
// nothing there.
const MetadataAddress = "169.254.169.254"

// CloudProbe tells whether this machine looks like a cloud server, by asking
// the metadata address once: any HTTP answer (even 401 or 404) means a cloud
// metadata service is there; a timeout or a refused connection means there
// is none. Other errors (no network yet, no route) leave it unknown and the
// probe is retried later. It complements install.sh, which recognizes only a
// few vendors and may record a cloud network address as the public host when
// the vendor is not one of them.
type CloudProbe struct {
	mu      sync.Mutex
	state   int // probeUnknown, probeCloud, probeNotCloud
	last    time.Time
	running bool
	probe   func(context.Context) (answered, sure bool)
	retry   time.Duration
	now     func() time.Time
}

const (
	probeUnknown = iota
	probeCloud
	probeNotCloud
)

// NewCloudProbe returns a probe of http://169.254.169.254/.
func NewCloudProbe() *CloudProbe {
	return &CloudProbe{probe: func(ctx context.Context) (bool, bool) {
		return metadataAnswers(ctx, "http://"+MetadataAddress+"/")
	}, retry: 30 * time.Second, now: time.Now}
}

// Start begins the first probe in the background.
func (p *CloudProbe) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kickLocked()
}

// OnCloud reports whether this machine should be treated as a cloud server:
// true when the metadata address answered and also while it is not known yet
// (the safe side: the console then recommends the SSH tunnel). It never
// blocks; an unknown result starts another background probe at most every
// 30 seconds.
func (p *CloudProbe) OnCloud() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == probeUnknown && p.now().Sub(p.last) >= p.retry {
		p.kickLocked()
	}
	return p.state != probeNotCloud
}

func (p *CloudProbe) kickLocked() {
	if p.running || p.state != probeUnknown {
		return
	}
	p.running, p.last = true, p.now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		answered, sure := p.probe(ctx)
		cancel()
		p.mu.Lock()
		defer p.mu.Unlock()
		p.running = false
		switch {
		case answered:
			p.state = probeCloud
		case sure:
			p.state = probeNotCloud
		}
	}()
}

// metadataAnswers asks url without a proxy and without following redirects.
// answered: an HTTP response came back; sure: the result is definite (an
// answer, a timeout or a refused connection).
func metadataAnswers(ctx context.Context, url string) (answered, sure bool) {
	c := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			Proxy:             nil,
			DialContext:       (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, false
	}
	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		return true, true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false, true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return false, true
	}
	return false, false
}
