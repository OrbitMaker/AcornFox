package appinstall

import (
	"errors"
	"fmt"
	"net/http"
)

// Fault codes of installing from the app catalog and of connecting private
// catalogs (CAT-CONN-*). This file is their single source: the API, the CLI's --json output, the Skill and docs/capabilities.md
// refer to these strings, so never renumber; new codes are appended.
const (
	CodeUnknown       = "CAT-INST-01" // app, recipe or version not in the catalog
	CodeBadParams     = "CAT-INST-02" // unknown, hidden or malformed parameter, or a required one missing
	CodeNameTaken     = "CAT-INST-03" // an app with that name already exists
	CodeBusy          = "CAT-INST-04" // an install of the same app is already running (task_id is the running one)
	CodeNoMemory      = "CAT-INST-05" // not enough room under the server-wide memory cap
	CodeArch          = "CAT-INST-06" // this machine's CPU architecture is not in the app's platforms
	CodeNoPort        = "CAT-INST-07" // no free access port left on this server
	CodeTooOld        = "CAT-INST-08" // this AcornFox is older than the app's min_platform
	CodePlatformLLM   = "CAT-INST-09" // the platform model key is not available yet (coming soon)
	CodeNoGateway     = "CAT-INST-10" // local model gateway chosen but none is installed and running here
	CodeGatewayToken  = "CAT-INST-11" // the local gateway would not issue a token (login or token API failed)
	CodePull          = "CAT-INST-12" // the image could not be obtained from any source
	CodeImageDigest   = "CAT-INST-13" // a gateway image archive did not match the catalog's signed checksum or size (not loaded)
	CodeFiles         = "CAT-INST-14" // writing config files into the data volume failed
	CodeStart         = "CAT-INST-15" // the deployment failed (container did not start or go live)
	CodeHealth        = "CAT-INST-16" // health check did not pass in time (or answered unexpected JSON)
	CodeInit          = "CAT-INST-17" // an init step (HTTP call after healthy) failed
	CodeInterrupted   = "CAT-INST-18" // the server restarted while the install was running
	CodePackage       = "CAT-INST-19" // the catalog or the app package could not be read or verified
	CodeInternal      = "CAT-INST-20" // runner without app-center support, store or Docker error
	CodeTaskNotFound  = "CAT-INST-21" // no install task with that id
	CodeNotCatalogApp = "CAT-INST-22" // the app was not installed from the catalog (no first-login info)
	CodeUnsupported   = "CAT-INST-23" // the manifest uses something this version cannot install yet (several images, requires)
	CodeOldVolumes    = "CAT-INST-24" // Docker still has data volumes of an earlier app of the same name (deleted with its data kept)
	CodeDisconnected  = "CAT-INST-25" // the app or recipe comes from a private catalog that was disconnected (no new installs, no upgrades)
	CodePrivateDenied = "CAT-INST-26" // a private catalog file could not be had from the download gateway (refused GW-CAT-*, gateway off or unreachable, retries used up, or the manifest names no image archive); no public fallback
	CodeNoDisk        = "CAT-INST-27" // not enough free disk where Docker keeps images for the apps' disk_mb.min
	CodeBadPlatform   = "CAT-INST-28" // a platform variable change (--set platform.…) names an unknown variable, one that cannot be changed, or a bad value

	// Connecting a private catalog with a code (POST /v1/catalog/connect).
	CodeConnFormat    = "CAT-CONN-01" // the code is not AFXC- plus 16 letters/digits (checked here, not sent)
	CodeConnNoGateway = "CAT-CONN-02" // the download gateway is switched off on this server
	CodeConnUnknown   = "CAT-CONN-03" // GW-CAT-01: no such code, or another machine used it
	CodeConnExpired   = "CAT-CONN-04" // GW-CAT-02: the code expired
	CodeConnDisabled  = "CAT-CONN-05" // GW-CAT-03: the private catalog is disabled
	CodeConnTooMany   = "CAT-CONN-06" // GW-CAT-04: too many tries from this machine this hour (retry_after)
	CodeConnGateway   = "CAT-CONN-07" // gateway unreachable or refused for another reason (gateway_code), or the machine key is unreadable
	CodeConnSave      = "CAT-CONN-08" // granted by the gateway but not recorded here; connecting again with the same code is safe
)

// FaultDef explains one code.
type FaultDef struct {
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
	Action  string `json:"action"`
}

// Faults lists every code with what it means and what to do. Steps that need
// the user (spending money, opening to the network, typing a key) say so.
var Faults = []FaultDef{
	{CodeUnknown, "目录里没有这个应用、配方或版本", "acornfox catalog list 看有哪些；目录太旧就 acornfox catalog sync"},
	{CodeBadParams, "参数不对：没有这个参数、这个参数由平台生成不能填、格式不符或缺了必填项", "按 message 改 --set；acornfox catalog info <应用> 看参数"},
	{CodeNameTaken, "已经有同名的应用", "用 --name 换个名字；要重装先由用户确认后 acornfox delete 旧的"},
	{CodeBusy, "同一个应用正在安装", "不要重复点安装；acornfox install status <task_id> 看进度"},
	{CodeNoMemory, "整机分配给应用的内存不够", "问用户：调小别的应用（acornfox app set --memory）或调大整机上限（acornfox limits --memory）"},
	{CodeArch, "这台机器的 CPU 架构不在应用支持的列表里", "换一台机器，或等应用出这个架构的版本"},
	{CodeNoPort, "这台服务器的访问端口用完了", "删掉不用的应用后重试"},
	{CodeTooOld, "AcornFox 版本比应用要求的旧", "先升级 AcornFox 再装"},
	{CodePlatformLLM, "平台 Key 还没开放", "改用 --llm local（本机 New API）或 --llm own（自己的 Key，要用户提供）"},
	{CodeNoGateway, "选了本机网关，但本机没有在运行的 New API", "先 acornfox install new-api，或装配方 hermes-with-new-api"},
	{CodeGatewayToken, "本机 New API 发不出令牌（登录或令牌接口失败）", "看 New API 是否在运行（acornfox status new-api）；管理员密码被改过时改用 --llm own"},
	{CodePull, "镜像从所有来源都没拿到", "看 attempts；国内网络确认已配镜像加速；稍后重试"},
	{CodeImageDigest, "下载网关给的镜像包和目录里签名的校验值或大小对不上，没有导入、已删掉", "公共目录的应用自动换公共源，都不对就等目录更新；私有目录的应用直接停下，不去公共源找，等目录更新或联系给码的人。不要手动绕过"},
	{CodeFiles, "往数据卷里写配置文件失败", "看 message；检查磁盘空间和 Docker 状态（acornfox doctor）"},
	{CodeStart, "容器没能启动或上线", "看 diagnosis；acornfox logs <应用> 看日志"},
	{CodeHealth, "健康检查在时限内没通过", "acornfox logs <应用> 看日志；内存太小时调大后重装"},
	{CodeInit, "装好后的初始化调用失败（没收到应答的不会重发，因为它可能已经生效）", "看 message；acornfox logs <应用>；这次的应用已清理，可以直接重装"},
	{CodeInterrupted, "安装途中服务重启了，这次安装已作废并清理", "重新安装"},
	{CodePackage, "目录或应用包读不了、校验不过", "acornfox catalog sync 后重试；都不行就重装 AcornFox"},
	{CodeInternal, "内部错误：运行器版本旧、数据库或 Docker 出错", "acornfox doctor；升级 AcornFox 后重试"},
	{CodeTaskNotFound, "没有这个安装任务", "检查任务编号"},
	{CodeNotCatalogApp, "这个应用不是从应用中心装的，没有首次登录信息", "—"},
	{CodeUnsupported, "清单用到了这个版本还不支持的写法（多个镜像、自带依赖）", "升级 AcornFox"},
	{CodeOldVolumes, "这台机器上还留着以前同名应用的数据卷（删应用时保留了数据），默认不让新装悄悄接上旧数据，所以拒绝", "停下问用户：旧数据还要不要。要接着用就加 --reuse-data 重装（页面上是“接上旧数据重装”；删除时保留过参数的，账号密码和以前一样）；不要旧数据就用 --name 换个名字装，或者由用户执行 message 里卷名对应的 docker volume rm 后再装"},
	{CodeDisconnected, "这个应用或配方来自已断开的私有目录（授权被撤销或目录被停用）；已装好的照常运行，但不能再装、不能升级", "告诉用户联系给连接码的人；拿到新连接码后 acornfox catalog connect 新码，再装"},
	{CodePrivateDenied, "私有目录的文件（应用包或镜像）没从下载网关取到：网关不给（这台机器对这个目录没有授权了、目录停用，或文件不在目录里），或者这台服务器关掉了下载网关、连不上网关、本机时钟不准、下载多次都没下完，或者私有目录的清单没写镜像包。私有应用的文件只走下载网关，不去公共源找", "看 gateway_code：GW-CAT-* 先 acornfox catalog sync 看目录是否已断开，还连着就请给码的人检查目录；GW-AUTH-01 先校准本机时间（acornfox doctor 看 SETUP-CLOCK-01）；GW-NET-01 或下载中断就检查网络后重装；网关被关掉时问用户是否打开；message 说清单没写镜像包时，告诉用户联系给码的人补上"},
	{CodeNoDisk, "Docker 存镜像的分区剩余空间不够这次要装的应用（清单的 disk_mb.min 之和，含下载的镜像包和解开后的镜像）", "问用户：删掉不用的应用或镜像（acornfox delete、docker image prune 由用户确认后执行），或扩容磁盘；acornfox doctor 看各分区剩余空间"},
	{CodeBadPlatform, "改平台变量不对：没有这个变量、这个变量不能改，或值的格式不对", "acornfox catalog platform-vars 看有哪些、哪些能改；按 message 改 --set platform.名字=值"},
	{CodeConnFormat, "连接码格式不对（应是 AFXC- 加 16 个字母数字），没有发给网关", "让用户照原样再抄一遍；0、1、8 会当成 O、I、B"},
	{CodeConnNoGateway, "这台服务器关掉了下载网关，连不了私有目录", "问用户是否打开下载网关（去掉 ACORNFOX_GATEWAY=off 或 network.json 里的 \"gateway\": \"off\"）"},
	{CodeConnUnknown, "连接码不存在、抄错了，或已经被别的机器用过（一码一机）", "让用户核对连接码；还不行就找给码的人要新码。不要反复试"},
	{CodeConnExpired, "连接码已过期", "找给码的人要新码"},
	{CodeConnDisabled, "这个私有目录已停用", "告诉用户联系给码的人"},
	{CodeConnTooMany, "这台机器这一小时试连接码的次数用完了", "等 retry_after 秒后再试，不要循环重试"},
	{CodeConnGateway, "连不上下载网关、网关拒绝（看 gateway_code），或本机钥匙读不出", "GW-AUTH-01 先校准本机时间；GW-NET-01 看网络和代理；ID-KEY 看 acornfox identity show；稍后再试"},
	{CodeConnSave, "网关已经授权，但本机没记下来（数据目录写不进去）", "检查数据目录权限和磁盘空间后，用同一个连接码再连一次（同一台机器重复用不算新的一次）"},
}

// Fault is an install failure with its code.
type Fault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	// GatewayCode is the download gateway's GW-* code behind CAT-CONN-*,
	// CAT-INST-26.
	GatewayCode string `json:"gateway_code,omitempty"`
	// RetryAfter is the wait in seconds for CAT-CONN-06.
	RetryAfter int `json:"retry_after,omitempty"`
	// Diagnosis carries the deployment's diagnosis for CAT-INST-15/16.
	Diagnosis any `json:"diagnosis,omitempty"`
	// TaskID is the running task for CAT-INST-04.
	TaskID string `json:"task_id,omitempty"`
	err    error
	// answered: an init step got an answer that said no (HTTP 4xx/5xx or
	// unexpected JSON), as opposed to a request that never got through.
	answered bool
}

func (f *Fault) Error() string {
	s := f.Code + " " + f.Message
	if f.err != nil {
		s += "（" + f.err.Error() + "）"
	}
	return s
}

func (f *Fault) Unwrap() error { return f.err }

// HTTPStatus is the status the API answers a synchronous fault with.
func (f *Fault) HTTPStatus() int {
	switch f.Code {
	case CodeUnknown, CodeTaskNotFound, CodeNotCatalogApp, CodeConnUnknown:
		return http.StatusNotFound
	case CodeBadParams, CodePlatformLLM, CodeUnsupported, CodeConnFormat, CodeBadPlatform:
		return http.StatusBadRequest
	case CodeNameTaken, CodeBusy, CodeNoMemory, CodeNoDisk, CodeArch, CodeNoPort, CodeTooOld, CodeNoGateway, CodeOldVolumes,
		CodeDisconnected, CodeConnNoGateway:
		return http.StatusConflict
	case CodePrivateDenied, CodeConnDisabled:
		return http.StatusForbidden
	case CodeConnExpired:
		return http.StatusGone
	case CodeConnTooMany:
		return http.StatusTooManyRequests
	case CodeConnGateway:
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

func fault(code string, err error, format string, a ...any) *Fault {
	f := &Fault{Code: code, Message: fmt.Sprintf(format, a...), err: err}
	for _, d := range Faults {
		if d.Code == code {
			f.Hint = d.Action
		}
	}
	return f
}

// AsFault returns err as a *Fault (CAT-INST-20 for anything else).
func AsFault(err error) *Fault {
	var f *Fault
	if errors.As(err, &f) {
		return f
	}
	return fault(CodeInternal, err, "内部错误：%v", err)
}
