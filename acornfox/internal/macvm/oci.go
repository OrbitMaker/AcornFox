package macvm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RootfsLayer returns the root file system in rootfs (a tar, tar.xz or tar.gz
// of an LXC image) as a plain tar stream without extended attributes: Apple's
// `container image load` refuses archives whose entries carry them
// ("invalid extended attribute", measured on macOS 26). macOS's own tar
// (bsdtar) reads the compressed archive and writes the entries again.
// Close the returned reader to end the process.
func RootfsLayer(ctx context.Context, rootfs string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "tar", "-cf", "-", "--no-xattrs", "--no-mac-metadata", "--format", "gnutar", "@"+rootfs)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procReader{ReadCloser: out, cmd: cmd}, nil
}

type procReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (p *procReader) Close() error {
	_ = p.ReadCloser.Close()
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
	return nil
}

// BuildOCI writes an OCI image archive to out: one layer (the tar stream,
// gzip-compressed), an image configuration that starts /sbin/init (a system
// with systemd, which container machine needs) and the name ref in the index.
// `container image load -i out` loads it under that name.
func BuildOCI(layer io.Reader, ref, out string) error {
	dir, err := os.MkdirTemp(filepath.Dir(out), ".oci-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return err
	}
	// The layer: hash the plain tar (diff id) and the compressed bytes (digest) in one pass.
	lf, err := os.CreateTemp(blobs, "layer-")
	if err != nil {
		return err
	}
	zh, dh := sha256.New(), sha256.New()
	cw := &countWriter{w: io.MultiWriter(lf, zh)}
	gz, _ := gzip.NewWriterLevel(cw, 6)
	if _, err := io.Copy(io.MultiWriter(gz, dh), layer); err != nil {
		lf.Close()
		return fmt.Errorf("打包根文件系统：%w", err)
	}
	if err := gz.Close(); err != nil {
		lf.Close()
		return err
	}
	if err := lf.Close(); err != nil {
		return err
	}
	layerHex := hex.EncodeToString(zh.Sum(nil))
	diffHex := hex.EncodeToString(dh.Sum(nil))
	if err := os.Rename(lf.Name(), filepath.Join(blobs, layerHex)); err != nil {
		return err
	}
	put := func(v any) (string, int64, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", 0, err
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		return h, int64(len(b)), os.WriteFile(filepath.Join(blobs, h), b, 0o644)
	}
	cfg := map[string]any{
		"architecture": "arm64", "os": "linux",
		"config": map[string]any{"Cmd": []string{"/sbin/init"}, "Env": []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}},
		"rootfs": map[string]any{"type": "layers", "diff_ids": []string{"sha256:" + diffHex}},
	}
	ch, cs, err := put(cfg)
	if err != nil {
		return err
	}
	man := map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": "sha256:" + ch, "size": cs},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": "sha256:" + layerHex, "size": cw.n}},
	}
	mh, ms, err := put(man)
	if err != nil {
		return err
	}
	index := map[string]any{"schemaVersion": 2, "manifests": []any{map[string]any{
		"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:" + mh, "size": ms,
		"platform":    map[string]any{"architecture": "arm64", "os": "linux"},
		"annotations": map[string]any{"org.opencontainers.image.ref.name": ref},
	}}}
	ib, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), ib, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		return err
	}
	return tarDir(dir, out)
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// tarDir writes the files of dir into a tar (names start with "./").
func tarDir(dir, out string) error {
	tmp := out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(f)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		name := "./" + filepath.ToSlash(rel)
		if rel == "." {
			name = "./"
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name, h.Uid, h.Gid, h.Uname, h.Gname = name, 0, 0, "", ""
		h.ModTime = time.Unix(0, 0)
		if info.IsDir() && !strings.HasSuffix(h.Name, "/") {
			h.Name += "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if cerr := tw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}
