package catalog

import (
	"sort"
	"strings"
)

// Image archives per CPU architecture.
//
// An image that is large, or not on any public registry, is handed out by
// the download gateway as an archive (what "docker save" writes). One
// archive holds one architecture's image, so a manifest names one archive
// per architecture:
//
//	release:
//	  images:
//	    - name: app
//	      …
//	      artifacts:
//	        amd64: { file: <gateway file id>, sha256: <hex>, size: <bytes> }
//	        arm64: { file: …, sha256: …, size: … }
//
// The older form (artifact, artifact_sha256, artifact_size right on the
// image) names a single archive. Every archive published that way was built
// on amd64, so it stands for amd64 only.
//
// A machine whose architecture has no archive does not use the gateway for
// that image: it pulls the image from public sources by its pinned digest
// (when the digest names a multi-architecture index, the registry hands out
// the machine's architecture). A private catalog's app is only fetched
// through the gateway, so there it cannot be installed.
//
// The installer asks ArtifactFor with the machine's architecture; lint
// checks the per-architecture form (CAT-LINT-20, CAT-LINT-21).

// Archs are the architecture names the per-architecture form accepts: Go's
// GOARCH spelling, the part after "linux/" in resources.platforms.
var Archs = []string{"amd64", "arm64"}

// LegacyArtifactArch is the architecture the older single-archive form
// stands for.
const LegacyArtifactArch = "amd64"

// ArchArtifact is one architecture's image archive on the download gateway.
type ArchArtifact struct {
	File   string `yaml:"file" json:"file"`     // the gateway's file id
	SHA256 string `yaml:"sha256" json:"sha256"` // of the archive; checked before it is loaded
	Size   int64  `yaml:"size" json:"size"`     // bytes
}

// NamesArtifact reports whether the image names a gateway archive at all,
// in either form and for any architecture.
func (img Image) NamesArtifact() bool {
	return len(img.Artifacts) > 0 || img.Artifact != ""
}

// LegacyArtifact reports whether the image names its archive in the older
// single-archive form (and not in the per-architecture one).
func (img Image) LegacyArtifact() bool {
	return len(img.Artifacts) == 0 && img.Artifact != ""
}

// ArtifactFor returns the archive the gateway serves for arch ("amd64",
// "arm64"), pinned by its checksum. ok is false when there is none for arch:
// the per-architecture form has no entry for it, or the older form is used
// and arch is not amd64, or the older form has no artifact_sha256 (the
// gateway is never used without the signed checksum). When a manifest writes
// both forms (lint refuses that) the per-architecture one wins.
func (img Image) ArtifactFor(arch string) (ArchArtifact, bool) {
	if len(img.Artifacts) > 0 {
		a, ok := img.Artifacts[arch]
		if !ok || a.File == "" || a.SHA256 == "" {
			return ArchArtifact{}, false
		}
		a.SHA256 = strings.ToLower(a.SHA256)
		return a, true
	}
	if img.Artifact == "" || img.ArtifactSHA256 == "" || arch != LegacyArtifactArch {
		return ArchArtifact{}, false
	}
	return ArchArtifact{File: img.Artifact, SHA256: strings.ToLower(img.ArtifactSHA256), Size: img.ArtifactSize}, true
}

// ArtifactArchs lists, sorted, the architectures ArtifactFor has an
// archive for.
func (img Image) ArtifactArchs() []string {
	var out []string
	if len(img.Artifacts) > 0 {
		for arch := range img.Artifacts {
			if _, ok := img.ArtifactFor(arch); ok {
				out = append(out, arch)
			}
		}
		sort.Strings(out)
		return out
	}
	if _, ok := img.ArtifactFor(LegacyArtifactArch); ok {
		out = append(out, LegacyArtifactArch)
	}
	return out
}

// NoArtifactReason says, for a machine of arch, why the image has no archive
// to fetch from the gateway (ArtifactFor is not ok): "" when it has one or
// names none at all. The text is shown in the install's progress.
func (img Image) NoArtifactReason(arch string) string {
	if _, ok := img.ArtifactFor(arch); ok || !img.NamesArtifact() {
		return ""
	}
	if img.LegacyArtifact() {
		if img.ArtifactSHA256 == "" {
			return "清单没有镜像包的校验值"
		}
		return "清单里的镜像包是旧写法，只算 linux/" + LegacyArtifactArch + " 的，这台是 linux/" + arch
	}
	have := img.ArtifactArchs()
	if len(have) == 0 {
		return "清单里没有 linux/" + arch + " 的镜像包"
	}
	return "清单里只有 linux/" + strings.Join(have, "、linux/") + " 的镜像包，没有 linux/" + arch + " 的"
}
