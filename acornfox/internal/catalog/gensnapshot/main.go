// Command gensnapshot rebuilds the catalog snapshot that is embedded in
// acornfox (internal/catalog/snapshot). It lints and packs -src with -serial,
// unsigned, and replaces version.txt, index.json, index.json.sig and apps/
// under -out. Nothing else under -out is touched. On a lint error it stops
// and -out stays as it was.
//
// The defaults are what the snapshot is made from today, with paths relative
// to internal/catalog (where go generate runs it):
//
//	go run ./gensnapshot                                      (= -src snapshotsrc -serial 4 -out snapshot)
//	go run ./gensnapshot -src <catalog checkout> -serial <N>  (before a release, N above 4)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/acornfox/acornfox/internal/catalog"
)

func main() {
	src := flag.String("src", "snapshotsrc", "目录仓库的根目录（含 apps/、recipes/、service-types/）")
	serial := flag.Int64("serial", 4, "快照的发布序号")
	out := flag.String("out", "snapshot", "快照目录")
	flag.Parse()
	if *src == "" {
		fmt.Fprintln(os.Stderr, "用法：gensnapshot [-src 目录] [-serial N] [-out 快照目录]")
		os.Exit(2)
	}
	b, err := catalog.Build(*src, *serial)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if e, ok := err.(*catalog.Error); ok {
			for _, p := range e.Problems {
				fmt.Fprintf(os.Stderr, "  %s %s %s：%s\n", p.Code, p.File, p.Path, p.Message)
			}
		}
		os.Exit(1)
	}
	for _, name := range []string{catalog.VersionFile, catalog.IndexFile, catalog.SigFile, "apps"} {
		if err := os.RemoveAll(filepath.Join(*out, name)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	files, err := b.Write(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, f := range files {
		fmt.Printf("%s  %s  %d\n", f.SHA256, f.Path, f.Size)
	}
}
