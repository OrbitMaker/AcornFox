package runner

import (
	"context"
	"errors"

	"github.com/moby/moby/client"
)

// LocalImageForCheck returns an image already on this computer that a self
// check can create a throwaway container from ("can Docker create and
// remove containers?"). It prefers the image the edge container runs, else
// takes any local image; it never pulls and does not depend on which proxy
// or app images exist.
func (d *Docker) LocalImageForCheck(ctx context.Context) (string, error) {
	res, err := d.c().ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", LabelEdge+"=1")})
	if err == nil {
		for _, c := range res.Items {
			if c.Labels[LabelEdge] == "1" && c.ImageID != "" {
				return c.ImageID, nil
			}
		}
	}
	imgs, err := d.c().ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return "", err
	}
	for _, im := range imgs.Items {
		if im.ID != "" {
			return im.ID, nil
		}
	}
	return "", errors.New("本机还没有任何镜像")
}
