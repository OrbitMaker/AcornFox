package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
)

func TestLocalImageForCheck(t *testing.T) {
	var containers []container.Summary
	var images []image.Summary
	pulled := false
	d := observationDocker(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			if !strings.Contains(r.URL.RawQuery, "acornfox.edge") {
				t.Errorf("container list without the edge label filter: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(containers)
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			_ = json.NewEncoder(w).Encode(images)
		case strings.Contains(r.URL.Path, "/images/create"):
			pulled = true
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()

	// No edge container, no image: an error, nothing pulled.
	if _, err := d.LocalImageForCheck(ctx); err == nil {
		t.Fatal("want an error without any local image")
	}
	// Any local image.
	images = []image.Summary{{ID: "sha256:aaa"}}
	if got, err := d.LocalImageForCheck(ctx); err != nil || got != "sha256:aaa" {
		t.Fatalf("got %q %v", got, err)
	}
	// The edge container's image wins.
	containers = []container.Summary{{ID: "c1", ImageID: "sha256:edge", Labels: map[string]string{LabelEdge: "1"}}}
	if got, err := d.LocalImageForCheck(ctx); err != nil || got != "sha256:edge" {
		t.Fatalf("got %q %v", got, err)
	}
	if pulled {
		t.Fatal("LocalImageForCheck must never pull")
	}
}
