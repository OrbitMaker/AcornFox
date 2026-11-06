package setup

// Files the built-in runtime downloads: pinned versions, sizes and SHA-256
// (taken from the upstream release files on 2026-11-01). To move to a new
// version, change the version and the checksum together; Fetch refuses a file
// whose checksum differs. Our own source, once it exists, serves the same
// files under <base>/runtime/<File> (see Sources).
//
// GatewayID is the file's ID on the AcornFox download gateway (the first
// column of the gateway's file list). It changes with the version, so a new
// version is asked for under a new ID. A file the gateway does not have yet
// answers GW-ART-01 and the next source is used; reserving the ID costs
// nothing.
//
// Mirrors inside mainland China (order from 2026-11 measurements on home
// broadband, where Huawei and Tencent were fastest and Aliyun's public
// mirror was capped at about 0.3 MB/s):
//   - files.m.daocloud.io/github.com/... mirrors GitHub release files;
//   - Alpine: Huawei, Tencent, Aliyun, Nanjing University.

const (
	colimaVersion     = "v0.10.3"
	colimaCoreVersion = "v0.10.4" // VM images (abiosoft/colima-core)
	limaVersion       = "v2.2.1"
	wslVersion        = "3.0.3"
	alpineBranch      = "v3.21"
	alpineVersion     = "3.21.9"
)

const ghMirror = "https://files.m.daocloud.io/github.com/"

func gh(path string) (mirror, upstream string) {
	return ghMirror + path, "https://github.com/" + path
}

func ghArtifact(id, title, file, path, sum string, size int64) Artifact {
	m, u := gh(path)
	return Artifact{ID: id, Title: title, File: file, SHA256: sum, Size: size, Mirrors: []string{m}, Upstream: []string{u}}
}

// macOS ----------------------------------------------------------------------

var colimaSums = map[string]struct {
	sum  string
	size int64
}{
	"arm64": {"980ad8bf61a4ca370243f4cb41401a61276dcd2c2502bee7b9b86f9250169f34", 15656320},
	"amd64": {"3082737fe8a98afda11cba7d9a20b6e56fe80c6153464beda04bec630758770b", 16954240},
}

func colimaBinary(goarch string) Artifact {
	a := map[string]string{"arm64": "arm64", "amd64": "x86_64"}[goarch]
	f := "colima-Darwin-" + a
	s := colimaSums[goarch]
	out := ghArtifact("colima", "运行工具 Colima", f, "abiosoft/colima/releases/download/"+colimaVersion+"/"+f, s.sum, s.size)
	out.GatewayID = "colima-darwin-" + goarch + "-" + colimaVersion
	return out
}

var limaSums = map[string]struct {
	sum  string
	size int64
}{
	"arm64": {"9e9eacce88f37e185c346bad73aa6136f738d8cdf8c3bb23cd42b071824bc66e", 38328082},
	"amd64": {"6b776e4bc41f358e6ee01fefcc78e2eb26f11635a0fcff632234af77a566c6c4", 24958659},
}

func limaTarball(goarch string) Artifact {
	a := map[string]string{"arm64": "arm64", "amd64": "x86_64"}[goarch]
	v := limaVersion[1:]
	f := "lima-" + v + "-Darwin-" + a + ".tar.gz"
	s := limaSums[goarch]
	out := ghArtifact("lima", "运行工具 Lima", f, "lima-vm/lima/releases/download/"+limaVersion+"/"+f, s.sum, s.size)
	out.GatewayID = "lima-darwin-" + goarch + "-" + limaVersion
	return out
}

var colimaImageSums = map[string]struct {
	sum  string
	size int64
}{
	"arm64": {"1fc0354f4f99734ce3886628cc7af8b0437c1a1d391b126bd09cba0df35ee53f", 332354401},
	"amd64": {"4cd967d2c58aa6971621343d372255b14ffbfe28a185cda0e3c34ac8a1eb15e6", 358298593},
}

func colimaImage(goarch string) Artifact {
	f := "ubuntu-24.04-minimal-cloudimg-" + goarch + "-docker.raw.gz"
	s := colimaImageSums[goarch]
	out := ghArtifact("vm-image", "Linux 虚拟机镜像", f, "abiosoft/colima-core/releases/download/"+colimaCoreVersion+"/"+f, s.sum, s.size)
	out.GatewayID = "colima-image-ubuntu-24.04-" + goarch + "-" + colimaCoreVersion
	return out
}

// Windows --------------------------------------------------------------------

func wslPackage() Artifact {
	f := "wsl." + wslVersion + ".0.x64.msi"
	out := ghArtifact("wsl-package", "WSL 安装包", f, "microsoft/WSL/releases/download/"+wslVersion+"/"+f, wslSum, wslSize)
	out.GatewayID = "wsl-x64-" + wslVersion // must match the gateway's file list
	return out
}

var (
	wslSum        = "ae977286974255bd26bf925f00f40f5692c6b2084155cbc0563c09aea091f211"
	wslSize int64 = 367575040
)

func alpineRootfs() Artifact {
	f := "alpine-minirootfs-" + alpineVersion + "-x86_64.tar.gz"
	p := "/" + alpineBranch + "/releases/x86_64/" + f
	return Artifact{ID: "rootfs", Title: "精简 Linux 安装包", File: f, SHA256: alpineSum, Size: alpineSize,
		Mirrors: []string{"https://mirrors.huaweicloud.com/alpine" + p, "https://mirrors.cloud.tencent.com/alpine" + p,
			"https://mirrors.aliyun.com/alpine" + p, "https://mirrors.nju.edu.cn/alpine" + p},
		Upstream: []string{"https://dl-cdn.alpinelinux.org/alpine" + p}}
}

var (
	alpineSum        = "3d5912d47e004ac77746d63ab848abd9e37164465cf1c866eb574bc526135012"
	alpineSize int64 = 3490481
)
