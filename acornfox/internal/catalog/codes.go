package catalog

import (
	"errors"
	"strings"
)

// Fault codes for building, signing, verifying and syncing the published
// catalog. This file is their single source: the CLI's --json output, the
// Skill and the docs refer to these strings, so do not renumber. Lint codes
// (CAT-LINT-*) live next to the checks in lint.go.
const (
	// Building (acornfox catalog build).
	CodeBuildLint    = "CAT-BUILD-01" // the catalog has lint errors; nothing was written
	CodeBuildInput   = "CAT-BUILD-02" // bad input: serial < 1, odd version string, symlink, unreadable or unpackable file in an app dir
	CodeBuildWrite   = "CAT-BUILD-03" // output directory could not be written
	CodeBuildSerial  = "CAT-BUILD-04" // serial is lower than the previous publish, or equal to it with a different index.json
	CodeBuildPackage = "CAT-BUILD-05" // a package path of the previous publish would get other bytes (same app version, new content)

	// Keys (keygen, build --key, verify --pubkey).
	CodeKeyRead   = "CAT-KEY-01" // private key file missing, unreadable or not an ed25519 PKCS#8 PEM
	CodeKeyPerm   = "CAT-KEY-02" // private key file readable by group or others (wider than 0600), or running on Windows where this cannot be checked
	CodeKeyExists = "CAT-KEY-03" // keygen target already exists; it is never overwritten
	CodeKeyPublic = "CAT-KEY-04" // public key string is not a 32-byte ed25519 key in base64url

	// Verifying a published directory (acornfox catalog verify).
	CodeVerifyFiles   = "CAT-VER-01" // version.txt, index.json or index.json.sig missing or unreadable, or index.json malformed
	CodeVerifySig     = "CAT-VER-02" // signature does not match any trusted public key
	CodeVerifySerial  = "CAT-VER-03" // version.txt and the serial inside index.json differ
	CodeVerifyPackage = "CAT-VER-04" // an app package is missing or its sha256/size differs from index.json

	// Client sync (Sync). Each attempt against one source fails with 02..06;
	// when every source failed Sync returns 08 with the attempts attached.
	// 01, 07 and 09 stop Sync at once (another source would not help).
	CodeSyncNoKeys    = "CAT-SYNC-01" // the program has no trusted public key, so nothing can be accepted
	CodeSyncNetwork   = "CAT-SYNC-02" // source unreachable, timed out or answered with a non-200 status
	CodeSyncResponse  = "CAT-SYNC-03" // source answered something unreadable: version.txt not a number, index.json malformed or too big
	CodeSyncSignature = "CAT-SYNC-04" // index.json signature does not verify with any trusted key
	CodeSyncMismatch  = "CAT-SYNC-05" // version.txt and the signed serial in index.json differ (source is mid-publish)
	CodeSyncRollback  = "CAT-SYNC-06" // signed index is not newer than the local copy (old mirror or replay)
	CodeSyncCache     = "CAT-SYNC-07" // local cache directory could not be written
	CodeSyncAllFailed = "CAT-SYNC-08" // every source failed; see the attempts
	CodeSyncBusy      = "CAT-SYNC-09" // another sync holds the cache lock and did not finish in time

	// Private catalogs (W10). 03..07 above are reused per private catalog for
	// the same kinds of problem with its index.
	CodeSyncGateway      = "CAT-SYNC-10" // asking the download gateway for the private catalogs failed; the cached ones are kept
	CodeSyncDisconnected = "CAT-SYNC-11" // a private catalog was disconnected (access revoked or catalog disabled); its cache was removed
	CodeSyncWrongCatalog = "CAT-SYNC-12" // the signed index names another catalog than the one it was fetched for (one catalog's index passed off as another's)

	// Local read (Load).
	CodeLoad = "CAT-LOAD-01" // neither the cache nor the built-in snapshot is usable

	// App packages (FetchPackage).
	CodePkgAllFailed = "CAT-PKG-01" // no source delivered a package that matches index.json
	CodePkgMismatch  = "CAT-PKG-02" // downloaded package sha256 or size differs from index.json
	CodePkgWrite     = "CAT-PKG-03" // the downloaded package could not be saved locally
	CodePkgPath      = "CAT-PKG-04" // the package path in index.json is not apps/<id>/<version>.tar.gz
)

// FaultDef explains one code for people and AI workbenches.
type FaultDef struct {
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
	Action  string `json:"action"`
}

// Faults lists every code above with what it means and what to do.
var Faults = []FaultDef{
	{CodeBuildLint, "目录有校验错误，没有打包", "先跑 acornfox catalog lint 目录，改完再打包"},
	{CodeBuildInput, "打包输入不对：序号小于 1、版本号含奇怪字符、应用目录里有符号链接、读不了或打不进包的文件", "按错误信息改输入"},
	{CodeBuildWrite, "输出目录写不进去", "检查输出目录权限和磁盘空间"},
	{CodeBuildSerial, "发布序号比上次小，或和上次相同但 index.json 内容不同；客户端会把这样的发布当回退拒收", "用比上次大的 --serial；上次的序号看输出目录（或 --prev 目录）里的 version.txt"},
	{CodeBuildPackage, "上次发布里已有的应用包路径要换成别的内容（同一 release.version 改了文件）", "改了应用目录就升 release.version，已发布的包不能换内容"},
	{CodeKeyRead, "私钥文件不存在、读不了，或不是 ed25519 的 PKCS#8 PEM", "确认 --key 指向 acornfox catalog keygen 生成的文件"},
	{CodeKeyPerm, "私钥文件权限比 0600 宽，别人也能读；或在 Windows 上运行（查不了权限，不在 Windows 上生成或使用私钥）", "chmod 600 私钥文件；Windows 用户在 WSL、Linux 或 Mac 上签名；怀疑泄露就换钥匙"},
	{CodeKeyExists, "keygen 的目标文件已存在，不会覆盖", "换一个文件名；确定不要旧钥匙时由人手动删除"},
	{CodeKeyPublic, "公钥不是 base64url 编码的 32 字节 ed25519 公钥", "用 keygen 打印的那一行"},
	{CodeVerifyFiles, "发布目录缺 version.txt、index.json 或 index.json.sig，或 index.json 格式不对", "重新 acornfox catalog build"},
	{CodeVerifySig, "签名和给出的公钥都对不上", "确认 --pubkey 是签名私钥对应的公钥；文件被改过就重新打包"},
	{CodeVerifySerial, "version.txt 和 index.json 里的序号不一致", "重新打包，发布时最后上传 version.txt"},
	{CodeVerifyPackage, "应用包缺失，或 sha256、大小和 index.json 不一致", "重新打包并完整上传 apps/ 目录"},
	{CodeSyncNoKeys, "程序里没有受信公钥，任何目录都不会被接受", "用带发布公钥的正式版程序；在此之前只用内置快照"},
	{CodeSyncNetwork, "连不上来源、超时或返回的不是 200", "换下一个来源（自动）；都不行就看网络和代理"},
	{CodeSyncResponse, "来源的应答看不懂", "换下一个来源（自动）；多半是地址指错了或中间有代理改了内容"},
	{CodeSyncSignature, "index.json 的签名验不过", "换下一个来源（自动），不落盘，继续用手上的目录"},
	{CodeSyncMismatch, "version.txt 和签名的 index.json 序号不一致，来源可能正在发布", "换下一个来源（自动），稍后再同步"},
	{CodeSyncRollback, "签名的目录不比本地新（旧副本或重放）", "换下一个来源（自动），继续用手上的目录"},
	{CodeSyncCache, "本地缓存目录写不进去", "检查数据目录的权限和磁盘空间"},
	{CodeSyncAllFailed, "所有来源都失败了", "继续用手上的目录；看 attempts 里每个来源的编号"},
	{CodeSyncBusy, "另一个同步正在写同一个缓存目录，等不到它结束", "稍后再同步；一直这样就看是不是有卡住的 acornfox 进程"},
	{CodeSyncGateway, "向下载网关取私有目录失败（连不上、签名不对或网关拒绝），手上的私有目录不变", "看 message 里的 GW- 编号；GW-AUTH-01 多半是本机时间不准；稍后 acornfox catalog sync 重试"},
	{CodeSyncDisconnected, "私有目录已断开：授权被撤销或目录被停用。它的应用不再显示，已经装好的照常运行，但不能再装、不能升级", "告诉用户联系给连接码的人；要恢复就拿新连接码 acornfox catalog connect"},
	{CodeSyncWrongCatalog, "签名的索引写明属于别的目录（例如私有目录的索引被放到公共来源，或网关把别的目录的索引当成这个目录下发），不用", "公共目录：换下一个来源（自动），继续用手上的目录，把这个来源报给维护者；私有目录：手上的版本不变，告诉给连接码的人"},
	{CodeLoad, "本地缓存和内置快照都用不了", "重新安装程序，或同步一次目录"},
	{CodePkgAllFailed, "所有来源都没下到和目录一致的应用包", "看 attempts 里每个来源的编号；稍后重试"},
	{CodePkgMismatch, "下载的应用包 sha256 或大小和目录不一致，已丢弃", "换下一个来源（自动）；都不对就等目录更新"},
	{CodePkgWrite, "应用包存不到本地", "检查目标目录权限和磁盘空间"},
	{CodePkgPath, "目录里的应用包路径不是 apps/<id>/<version>.tar.gz", "目录本身有问题，等下一次发布"},
}

// Attempt is one source tried by Sync or FetchPackage.
type Attempt struct {
	Source  string `json:"source"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is a catalog failure with its code.
type Error struct {
	Code     string
	Message  string
	Err      error
	Problems []Problem // CAT-BUILD-01: the lint findings
	Attempts []Attempt // CAT-SYNC-08, CAT-PKG-01: one entry per source
}

func (e *Error) Error() string {
	s := e.Code + " " + e.Message
	if e.Err != nil {
		s += "（" + e.Err.Error() + "）"
	}
	if len(e.Attempts) > 0 {
		var parts []string
		for _, a := range e.Attempts {
			parts = append(parts, a.Source+"："+a.Code+" "+a.Message)
		}
		s += "；" + strings.Join(parts, "；")
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// CodeOf returns err's catalog code, or "".
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func newErr(code string, err error, msg string) *Error {
	return &Error{Code: code, Message: msg, Err: err}
}
