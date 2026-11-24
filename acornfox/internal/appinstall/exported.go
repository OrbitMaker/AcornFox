package appinstall

// DownloadFile is the segmented download of this package for another part of
// the program (the Mac installer, internal/macvm, which brings Apple's
// container, its kernel and the Debian image through the download gateway).
// Same engine as the app images: several connections at once, resume after a
// stop, a fresh address from the gateway when one expires, the whole file
// checked against the pinned sha256 and size at the end. A server that does not
// do Range, or a file of unknown size, goes through the single-connection
// download.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FileSource is where to fetch one file from.
type FileSource struct {
	URL       string
	ExpiresAt time.Time
	// Reissue asks for a new address of the same file (nil: an expired or
	// refused address ends the download).
	Reissue func(ctx context.Context) (url string, expires time.Time, err error)
}

// FileProgress is a download's progress.
type FileProgress struct {
	Done, Total int64
	Conns       int  // connections open now (segmented download)
	Verifying   bool // all bytes are in; the checksum is being computed
}

// DownloadFile downloads src into dest (written only when whole and checked),
// keeping unfinished work under cacheDir. wantSHA is required; size 0 means
// unknown (then one connection is used). onProgress, when not nil, is called
// about once a second.
func DownloadFile(ctx context.Context, hc *http.Client, src FileSource, cacheDir, dest, wantSHA string, size int64, onProgress func(FileProgress)) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	st := &dlStatus{}
	st.total.Store(size)
	s := dlSource{URL: src.URL, ExpiresAt: src.ExpiresAt, Reissue: src.Reissue}
	if onProgress != nil {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					onProgress(FileProgress{Done: st.done.Load(), Total: size, Conns: int(st.conns.Load()), Verifying: st.phase.Load() == phaseVerify})
				}
			}
		}()
	}
	prm := segParamsFromEnv()
	if prm.Enabled && size > 2*prm.SegSize && cacheDir != "" {
		cache := newDLCacheEntry(cacheDir, wantSHA, size, prm.SegSize)
		err := segDownload(ctx, hc, s, cache, dest, wantSHA, st, prm)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errNoRange) {
			return scrubURL(err)
		}
		st.done.Store(0)
	}
	if err := downloadChecked(ctx, hc, s, dest, wantSHA, size, st); err != nil {
		_ = os.Remove(dest)
		return scrubURL(err)
	}
	return nil
}
