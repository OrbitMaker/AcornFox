package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// One Docker "list containers" answer with every kind of container: the
// platform's own (an app, an add-on, a helper, the entry proxy, the entry
// proxy it replaced) and the user's (running with ports, stopped without,
// sharing the host network).
const othersListJSON = `[
 {"Id":"aaaaaaaaaaaa1111","Names":["/af-shop-0123456789ab"],"Image":"acornfox/shop:0123456789ab","State":"running","Created":1,
  "Labels":{"acornfox.managed":"1","acornfox.app":"shop","acornfox.role":"app"},"Ports":[{"IP":"127.0.0.1","PrivatePort":8080,"PublicPort":18810,"Type":"tcp"}]},
 {"Id":"bbbbbbbbbbbb2222","Names":["/af-shop-addon-postgres"],"Image":"postgres:16","State":"running","Created":2,
  "Labels":{"acornfox.managed":"1","acornfox.app":"shop","acornfox.role":"addon"},"Ports":[]},
 {"Id":"cccccccccccc3333","Names":["/af-shop-files-1"],"Image":"busybox","State":"exited","Created":3,
  "Labels":{"acornfox.managed":"1","acornfox.app":"shop","acornfox.role":"files"},"Ports":[]},
 {"Id":"dddddddddddd4444","Names":["/acornfox-edge"],"Image":"acornfox/edge","State":"running","Created":4,
  "Labels":{"acornfox.edge":"1"},"Ports":[{"IP":"0.0.0.0","PrivatePort":443,"PublicPort":443,"Type":"tcp"}]},
 {"Id":"eeeeeeeeeeee5555","Names":["/acornfox-edge-old-1"],"Image":"acornfox/edge","State":"exited","Created":5,
  "Labels":{},"Ports":[]},
 {"Id":"ffffffffffff6666","Names":["/redis"],"Image":"redis:7","State":"running","Created":6,
  "Labels":{"com.docker.compose.project":"mine"},"Ports":[
    {"IP":"0.0.0.0","PrivatePort":6379,"PublicPort":6379,"Type":"tcp"},{"IP":"::","PrivatePort":6379,"PublicPort":6379,"Type":"tcp"},{"PrivatePort":9999,"Type":"tcp"}]},
 {"Id":"111111111111aaaa","Names":["/old-db"],"Image":"mysql:5.7","State":"exited","Created":7,"Labels":null,"Ports":[]},
 {"Id":"222222222222bbbb","Names":["/metrics"],"Image":"prom/node-exporter","State":"running","Created":8,"Labels":{},"Ports":[],
  "HostConfig":{"NetworkMode":"host"}}
]`

const othersStatsJSON = `{"read":"2026-11-25T10:00:00Z","cpu_stats":{"cpu_usage":{"total_usage":300},"system_cpu_usage":2000,"online_cpus":2},
 "precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},
 "memory_stats":{"usage":134217728,"limit":536870912,"stats":{"inactive_file":33554432}}}`

type dockerCalls struct {
	mu    sync.Mutex
	calls []string
}

func (c *dockerCalls) add(r *http.Request) {
	c.mu.Lock()
	c.calls = append(c.calls, r.Method+" "+r.URL.Path)
	c.mu.Unlock()
}

func TestIsPlatformContainer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
		names  []string
		want   bool
	}{
		{"app", map[string]string{LabelManaged: "1"}, []string{"/af-a-0123456789ab"}, true},
		{"entry proxy by label", map[string]string{LabelEdge: "1"}, []string{"/x"}, true},
		{"entry proxy by name", nil, []string{"/acornfox-edge"}, true},
		{"old entry proxy by name", nil, []string{"/acornfox-edge-old-7"}, true},
		{"managed=0 is not ours", map[string]string{LabelManaged: "0"}, []string{"/mine"}, false},
		{"user container", map[string]string{"x": "y"}, []string{"/redis"}, false},
		{"user container named like ours", nil, []string{"/af-looks-like"}, false},
		{"no labels no names", nil, nil, false},
	} {
		if got := isPlatformContainer(tc.labels, tc.names); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestListOtherContainersSkipsThePlatformAndReadsOnly(t *testing.T) {
	var calls dockerCalls
	d := observationDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls.add(r)
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(othersListJSON))
		case strings.HasSuffix(r.URL.Path, "/stats"):
			_, _ = w.Write([]byte(othersStatsJSON))
		default:
			w.WriteHeader(404)
		}
	})
	res, err := d.ListOtherContainers(context.Background(), OtherContainersRequest{Stats: true})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, c := range res.Containers {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "metrics,old-db,redis" {
		t.Fatalf("only the user's containers, by name: %v", names)
	}
	byName := map[string]OtherContainer{}
	for _, c := range res.Containers {
		byName[c.Name] = c
	}
	redis := byName["redis"]
	if !redis.Running || redis.State != "running" || redis.Image != "redis:7" || redis.ID != "ffffffffffff" {
		t.Errorf("redis: %+v", redis)
	}
	if len(redis.Ports) != 1 || redis.Ports[0] != (OtherPort{Host: 6379, Container: 6379, Proto: "tcp"}) {
		t.Errorf("a published port is listed once, an unpublished one not at all: %+v", redis.Ports)
	}
	if !redis.StatsAvailable || !redis.CPUAvailable || redis.CPUPercent != 40 || redis.MemoryUsageMB != 96 {
		t.Errorf("redis usage: %+v", redis)
	}
	old := byName["old-db"]
	if old.Running || old.State != "exited" || len(old.Ports) != 0 || old.Ports == nil || old.StatsAvailable {
		t.Errorf("a stopped container without ports has no usage and an empty (not null) port list: %+v", old)
	}
	if m := byName["metrics"]; !m.HostNetwork || !m.Running {
		t.Errorf("host network: %+v", m)
	}
	// Reads only: no request changes anything, and only running user
	// containers get a stats call (never the platform's, never a stopped one).
	stats := 0
	for _, c := range calls.calls {
		if !strings.HasPrefix(c, "GET ") {
			t.Errorf("a request that is not a read: %s", c)
		}
		if strings.HasSuffix(c, "/stats") {
			stats++
			if strings.Contains(c, "aaaaaaaaaaaa") || strings.Contains(c, "dddddddddddd") || strings.Contains(c, "111111111111") {
				t.Errorf("stats of a container that should not be asked: %s", c)
			}
		}
	}
	if stats != 2 {
		t.Errorf("stats calls = %d, want 2 (redis, metrics): %v", stats, calls.calls)
	}
}

func TestListOtherContainersWithoutStatsAndWithBrokenStats(t *testing.T) {
	var calls dockerCalls
	d := observationDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls.add(r)
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			_, _ = w.Write([]byte(othersListJSON))
			return
		}
		w.WriteHeader(500)
	})
	res, err := d.ListOtherContainers(context.Background(), OtherContainersRequest{})
	if err != nil || len(res.Containers) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	for _, c := range calls.calls {
		if strings.HasSuffix(c, "/stats") {
			t.Fatalf("stats were not asked for but were read: %s", c)
		}
	}
	// A container whose stats fail is still listed, without usage.
	res, err = d.ListOtherContainers(context.Background(), OtherContainersRequest{Stats: true})
	if err != nil || len(res.Containers) != 3 {
		t.Fatalf("a failing stats read must not fail the listing: %v %+v", err, res)
	}
	for _, c := range res.Containers {
		if c.StatsAvailable {
			t.Errorf("%s has usage though the read failed", c.Name)
		}
	}
}

func TestListOtherContainersDockerDown(t *testing.T) {
	d := observationDocker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"daemon is not running"}`))
	})
	if _, err := d.ListOtherContainers(context.Background(), OtherContainersRequest{Stats: true}); err == nil {
		t.Fatal("a Docker that fails must fail the listing")
	}
}

func TestListOtherContainersNoneAtAll(t *testing.T) {
	d := observationDocker(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	res, err := d.ListOtherContainers(context.Background(), OtherContainersRequest{Stats: true})
	if err != nil || len(res.Containers) != 0 || res.Truncated {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestListOtherContainersTruncates(t *testing.T) {
	var items []map[string]any
	for i := 0; i < OtherContainersMax+5; i++ {
		items = append(items, map[string]any{"Id": strings.Repeat("a", 12) + string(rune('a'+i%26)), "Names": []string{"/c" + string(rune('a'+i/26)) + string(rune('a'+i%26))}, "State": "exited", "Labels": map[string]string{}})
	}
	body, _ := json.Marshal(items)
	d := observationDocker(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
	res, err := d.ListOtherContainers(context.Background(), OtherContainersRequest{})
	if err != nil || len(res.Containers) != OtherContainersMax || !res.Truncated {
		t.Fatalf("%v len=%d truncated=%v", err, len(res.Containers), res.Truncated)
	}
}

// othersFake serves a fixed listing over the runner socket.
type othersFake struct {
	*fakeAPI
	mu  sync.Mutex
	req OtherContainersRequest
	res OtherContainersResponse
	err error
}

func (o *othersFake) ListOtherContainers(_ context.Context, req OtherContainersRequest) (OtherContainersResponse, error) {
	o.mu.Lock()
	o.req = req
	o.mu.Unlock()
	return o.res, o.err
}

func TestOtherContainersOverTheSocket(t *testing.T) {
	api := &othersFake{fakeAPI: &fakeAPI{}, res: OtherContainersResponse{Containers: []OtherContainer{{ID: "abc", Name: "redis", State: "running", Running: true, Ports: []OtherPort{{Host: 6379, Container: 6379, Proto: "tcp"}}}}}}
	cli := startTestServer(t, NewServer(api, "/var/lib/acornfox/uploads"))
	ctx := context.Background()
	got, err := cli.ListOtherContainers(ctx, OtherContainersRequest{Stats: true})
	if err != nil || len(got.Containers) != 1 || got.Containers[0].Name != "redis" || !api.req.Stats {
		t.Fatalf("%v %+v req=%+v", err, got, api.req)
	}
	// An empty listing is an empty list, not null.
	api.res = OtherContainersResponse{}
	got, err = cli.ListOtherContainers(ctx, OtherContainersRequest{})
	if err != nil || got.Containers == nil || len(got.Containers) != 0 {
		t.Fatalf("empty: %v %+v", err, got)
	}
	// A Docker error comes back as the runner's error.
	api.err = errors.New("docker went away")
	var re *RemoteError
	if _, err = cli.ListOtherContainers(ctx, OtherContainersRequest{}); !errors.As(err, &re) || re.Code != "docker_error" {
		t.Fatalf("docker error: %v", err)
	}
	// A runner API without the capability: 501, which is ErrUnsupported.
	plain := startTestServer(t, NewServer(&fakeAPI{}, "/var/lib/acornfox/uploads"))
	if _, err = plain.ListOtherContainers(ctx, OtherContainersRequest{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
}
