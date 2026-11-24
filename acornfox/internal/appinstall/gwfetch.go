package appinstall

// Bringing an image archive through the download gateway: ask for the
// address (W2), download it (several connections at once for a big file,
// segdl.go; one connection for a small one or a server that ignores Range,
// download.go), check it against the signed catalog, and have the runner
// load it. The choice between the gateway and the image sources (mirrors)
// is made in pickpath.go.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/runner"
)

// fromGateway downloads the image archive art (this machine's architecture's,
// from ArtifactFor) through the download gateway (W2, purpose app), checks it
// against the size and sha256 the signed catalog pins for it before anything
// is loaded, and has the runner load it and tag it acornfox/<app>:<tagID>. The checksum the
// gateway reports is only compared: when it differs from the catalog's, the
// file is not even downloaded. Loaded names other than the app's tag are
// dropped by the runner, so nothing is left behind when a later step fails
// (cleanup removes the app's tag, hence the image).
func (t *taskRun) fromGateway(ctx context.Context, p *planned, cat runner.CatalogAPI, art catalog.ArchArtifact, tagID string) (string, error) {
	st := &dlStatus{}
	stop := t.reportDownload(ctx, p.name, 0, st)
	dest, err := t.gatewayDownload(ctx, p, art, st)
	stop()
	if err != nil {
		return "", err
	}
	defer os.Remove(dest)
	return t.loadArchive(ctx, p, cat, tagID, dest, st.total.Load())
}

// gatewayDownload asks the gateway for art's address and downloads the
// archive into the downloads folder, checked against the catalog's sha256
// and size. It returns the file (the caller removes it after loading it);
// st gets the total size once it is known and the progress.
func (t *taskRun) gatewayDownload(ctx context.Context, p *planned, art catalog.ArchArtifact, st *dlStatus) (string, error) {
	e := t.e
	want := strings.ToLower(art.SHA256)
	issue := func(ctx context.Context) (gatewayclient.Issued, error) {
		if p.app.Origin != "" {
			// A private catalog's image: W2 names the catalog and the app.
			iss, err := e.cfg.Catalog.cfg.Gateway.IssueApp(ctx, art.File, gatewayclient.AppRef{Catalog: p.app.Origin, App: p.app.Manifest.ID})
			if err != nil {
				return iss, privateIssueFault(err, "镜像包 "+art.File)
			}
			return iss, nil
		}
		iss, err := e.cfg.Gateway.Issue(ctx, art.File, "app")
		return iss, scrubURL(err)
	}
	// offset is the gateway's clock minus this machine's, as the client
	// learned it; expiry times are turned into this machine's clock with it.
	offset := func() time.Duration {
		var g any = e.cfg.Gateway
		if p.app.Origin != "" {
			g = e.cfg.Catalog.cfg.Gateway
		}
		if c, ok := g.(clockOffsetter); ok {
			return c.ClockOffset()
		}
		return 0
	}
	size := art.Size
	// check compares what the gateway reports with the signed catalog.
	check := func(iss gatewayclient.Issued) error {
		if iss.SHA256 != "" && strings.ToLower(iss.SHA256) != want {
			return fault(CodeImageDigest, nil, "下载网关给的 %s 校验值和目录里的不一致，没有下载", art.File)
		}
		if size > 0 && iss.Size > 0 && iss.Size != size {
			return fault(CodeImageDigest, nil, "下载网关给的 %s 大小和目录里的不一致，没有下载", art.File)
		}
		return nil
	}
	iss, err := issue(ctx)
	if err != nil {
		return "", err
	}
	if err := check(iss); err != nil {
		return "", err
	}
	if size <= 0 {
		size = iss.Size
	}
	st.total.Store(size)
	if e.cfg.UploadDir == "" {
		return "", errors.New("没有下载目录")
	}
	dest := filepath.Join(e.cfg.UploadDir, "catalog-"+t.id+"-"+newID()+".tar")
	src := dlSource{URL: iss.URL, ExpiresAt: localExpiry(iss.ExpiresAt, offset(), time.Now()), Reissue: func(ctx context.Context) (string, time.Time, error) {
		// The signed address expired or the connection broke: ask the
		// gateway again and resume from the bytes already here.
		again, err := issue(ctx)
		if err != nil {
			return "", time.Time{}, err
		}
		if err := check(again); err != nil {
			return "", time.Time{}, err
		}
		return again.URL, localExpiry(again.ExpiresAt, offset(), time.Now()), nil
	}}
	if err := t.fetchArchive(ctx, src, dest, want, size, st); err != nil {
		_ = os.Remove(dest)
		return "", scrubURL(err)
	}
	return dest, nil
}

// fetchArchive downloads src to dest and checks it. A big file whose size is
// known goes through the segmented download; when its server ignores Range
// (or the download is switched off, ACORNFOX_DL_SEGMENTED=0), and for a small
// file, one connection does it.
func (t *taskRun) fetchArchive(ctx context.Context, src dlSource, dest, want string, size int64, st *dlStatus) error {
	e := t.e
	prm := segParamsFromEnv()
	if prm.Enabled && size > 2*prm.SegSize && e.cfg.UploadDir != "" {
		cache := newDLCacheEntry(dlCacheDir(e.cfg.UploadDir), want, size, prm.SegSize)
		err := segDownload(ctx, e.cfg.HTTP, src, cache, dest, want, st, prm)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errNoRange) {
			var f *Fault
			if ctx.Err() != nil || errors.As(err, &f) {
				return err
			}
			return fault(CodeSegDL, err, "镜像包分段下载没成：%v", scrubURL(err))
		}
		e.cfg.Log.Info("download server ignores Range, one connection", "size", size)
		st.done.Store(0)
		st.conns.Store(0)
		st.phase.Store(phaseDownload)
	}
	return downloadChecked(ctx, e.cfg.HTTP, src, dest, want, size, st)
}

// loadArchive has the runner load the downloaded archive dest and tag it
// acornfox/<app>:<tagID>; size is only for the message.
func (t *taskRun) loadArchive(ctx context.Context, p *planned, cat runner.CatalogAPI, tagID, dest string, size int64) (string, error) {
	t.progress(ctx, p.name, "image", fmt.Sprintf("已从下载网关下载 %s，正在导入镜像（大镜像要几十秒）", mbText(size)),
		&StepProgress{Phase: "load", DoneBytes: size, TotalBytes: size})
	res, err := cat.LoadImage(ctx, runner.LoadImageRequest{App: p.name, Path: dest, DeploymentID: tagID})
	if err != nil {
		return "", fmt.Errorf("导入镜像失败：%w", err)
	}
	if res.Image == nil || res.Image.ID == "" {
		return "", errors.New("导入镜像后没有拿到镜像（运行器版本太旧？）")
	}
	return res.Image.ID, nil
}
