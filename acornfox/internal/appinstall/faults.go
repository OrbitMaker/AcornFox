package appinstall

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/installtiming"
)

// Fault codes of installing from the app catalog and of connecting private
// catalogs (CAT-CONN-*). This file is their single source: the API, the CLI's --json output, the Skill and docs/capabilities.md
// refer to these strings, so never renumber; new codes are appended.
const (
	CodeUnknown   = "CAT-INST-01" // app, recipe or version not in the catalog
	CodeBadParams = "CAT-INST-02" // unknown, hidden or malformed parameter, or a required one missing
	CodeNameTaken = "CAT-INST-03" // an app with that name already exists
	CodeBusy      = "CAT-INST-04" // an install of the same app is already running (task_id is the running one)
	CodeNoMemory  = "CAT-INST-05" // not enough room under the server-wide memory cap
	CodeArch      = "CAT-INST-06" // this machine's CPU architecture is not in the app's platforms
	CodeNoPort    = "CAT-INST-07" // no free access port left on this server
	CodeTooOld    = "CAT-INST-08" // this AcornFox is older than the app's min_platform
	// CAT-INST-09, -10 and -11 were about a model source built into the
	// platform, which is gone (a model API is a service like any other):
	// nothing raises them any more, and the numbers are never used again.
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
	CodePrivateDenied = "CAT-INST-26" // a private catalog file could not be had from the download gateway (refused GW-CAT-*, gateway off or unreachable, retries used up, or the manifest names no image archive or none for this machine's architecture); no public fallback
	CodeNoDisk        = "CAT-INST-27" // not enough free disk where Docker keeps images for the apps' disk_mb.min
	CodeBadPlatform   = "CAT-INST-28" // a platform variable change (--set platform.…) names an unknown variable, one that cannot be changed, or a bad value
	CodeBadManifest   = "CAT-INST-29" // the app package's manifest uses a field, value or format version this AcornFox does not know (refused before anything is created or downloaded)
	// CodeStuck is not an error: it is in a running task's timing when the
	// running step is past its stuck threshold (internal/installtiming).
	CodeStuck = installtiming.CodeStuck // CAT-INST-30

	// Services an app needs (manifest uses written the new way) and what a
	// providing app hands out to it (needs.go, connect.go).
	CodeNoService     = "CAT-INST-31"            // a required service is not on this machine (usable): register one; nothing is installed
	CodeChooseService = "CAT-INST-32"            // several usable services of the kind: the request must name one (use)
	CodeBadUse        = "CAT-INST-33"            // the service named in use does not exist, is of another kind or is not usable now, or no app of the request has that need
	CodeIssue         = "CAT-INST-34"            // the providing app's issue steps did not pass (no credential for the consumer), or the provider was not running
	CodeRevoke        = installtiming.RevokeCode // CAT-INST-35: a credential was not taken back (a warning: it never stops anything)

	// CodeLegacyManifest: the app package's manifest is written the former
	// way (a setting of the built-in model kind, a provides entry whose
	// steps were in platform code, a use that filled such a setting); this
	// platform does not install it (refused before anything is created).
	CodeLegacyManifest = "CAT-INST-36"

	// Services that hold a consumer's data, and services only some apps
	// may take (connect.go, needs.go).
	CodeCheckStop = "CAT-INST-37" // an old credential failed the check, and its provider wrote on_check_fail: stop: the install stopped, nothing was replaced
	CodeTaken     = "CAT-INST-38" // the service named (or matched) cannot take this app: another app holds it for itself, the need wants it for itself and others use it, or the provider's limit is reached

	// A value the manifest writes could not be worked out: a template that
	// cannot be read, a transform that failed (a map without a matching
	// entry), an if that cannot be read, two entries of one env key both
	// holding, a start command that would carry a secret (steps.go).
	CodeRender = "CAT-INST-39"

	// CodeSegDL: the image archive could not be brought through the download
	// gateway by the segmented download (segdl.go): the download cache could
	// not be written, a segment kept failing, or the address kept expiring.
	// What is in the cache stays, so the next try goes on from it.
	CodeSegDL = "CAT-INST-40"

	// A platform variable an installed app was given at install time is
	// not what the platform would give it now (its address after the
	// exposure changed or a domain was bound): `apps vars` marks the row
	// with it. Not an error; nothing is rewritten in this version.
	CodeVarsStale = "APP-VARS-01"

	// Changing an installed app's settings and applying them again
	// (reapply.go): `acornfox apps set`, `apps reapply`, the address of an
	// app following its exposure or domain, a platform variable or a
	// service the app is plugged into that changed.
	CodeApplyCannot  = "APP-APPLY-01" // this app cannot be applied again now (not from the app center, manifest not saved, stopped with steps to run, an install or another apply is running)
	CodeApplyEdit    = "APP-APPLY-02" // this setting cannot be changed after the install (editable never or reinstall), or a required one was emptied
	CodeApplyFailed  = "APP-APPLY-03" // applying failed and everything was put back as it was (the cause's code is in the message)
	CodeApplyUndone  = "APP-APPLY-04" // applying failed and putting it back did not work either: the app may be in a mixed state
	CodeApplyConfirm = "APP-APPLY-05" // the change restarts the app and the request did not say it is agreed to (confirm_restart); nothing was changed

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
	{CodeBadParams, "参数不对：没有这个参数、这个参数由平台生成不能填、格式不符或缺了必填项", "按 message 改 --set；acornfox catalog info <应用> 看参数。必填项要用户提供内容的（例如要用的模型名），先问用户"},
	{CodeNameTaken, "已经有同名的应用", "用 --name 换个名字；要重装先由用户确认后 acornfox delete 旧的"},
	{CodeBusy, "同一个应用正在安装", "不要重复点安装；acornfox install status <task_id> 看进度"},
	{CodeNoMemory, "整机分配给应用的内存不够", "问用户：调小别的应用（acornfox app set --memory）或调大整机上限（acornfox limits --memory）"},
	{CodeArch, "这台机器的 CPU 架构不在应用支持的列表里", "换一台机器，或等应用出这个架构的版本"},
	{CodeNoPort, "这台服务器的访问端口用完了", "删掉不用的应用后重试"},
	{CodeTooOld, "AcornFox 版本比应用要求的旧", "先升级 AcornFox 再装（错误里的 needs_platform 是要升到的版本）"},
	{"CAT-INST-09", retiredMeaning, retiredAction},
	{"CAT-INST-10", retiredMeaning, retiredAction},
	{"CAT-INST-11", retiredMeaning, retiredAction},
	{CodePull, "镜像从所有来源都没拿到", "看 attempts；国内网络确认已配镜像加速；稍后重试"},
	{CodeImageDigest, "下载网关给的镜像包和目录里签名的校验值或大小对不上，没有导入、已删掉", "公共目录的应用自动换公共源，都不对就等目录更新；私有目录的应用直接停下，不去公共源找，等目录更新或联系给码的人。不要手动绕过"},
	{CodeFiles, "往数据卷里写配置文件失败", "看 message；检查磁盘空间和 Docker 状态（acornfox doctor）"},
	{CodeStart, "容器没能启动或上线", "看 diagnosis；acornfox logs <应用> 看日志"},
	{CodeHealth, "健康检查在时限内没通过", "acornfox logs <应用> 看日志；内存太小时调大后重装"},
	{CodeInit, "装好后的初始化调用失败：发给应用的请求，或在应用自己的容器里执行的命令（没收到应答的请求、超时的命令不会重发，因为它可能已经生效）", "看 message（命令失败的写了退出码和标准错误的最后几行）；acornfox logs <应用>；这次的应用已清理，可以直接重装"},
	{CodeInterrupted, "安装途中服务重启了，这次安装已作废并清理", "重新安装"},
	{CodePackage, "目录或应用包读不了、校验不过", "acornfox catalog sync 后重试；都不行就重装 AcornFox"},
	{CodeInternal, "内部错误：运行器版本旧、数据库或 Docker 出错", "acornfox doctor；升级 AcornFox 后重试"},
	{CodeTaskNotFound, "没有这个安装任务", "检查任务编号"},
	{CodeNotCatalogApp, "这个应用不是从应用中心装的，没有首次登录信息", "—"},
	{CodeUnsupported, "清单用到了这个版本还不支持的写法（多个镜像，或格式已经认、这一版还不执行的写法；现在格式里的写法这一版都已执行）", "升级 AcornFox"},
	{CodeOldVolumes, "这台机器上还留着以前同名应用的数据卷（删应用时保留了数据），默认不让新装悄悄接上旧数据，所以拒绝", "停下问用户：旧数据还要不要。要接着用就加 --reuse-data 重装（页面上是“接上旧数据重装”；删除时保留过参数的，账号密码、你填过的设置项、改过的平台变量、接的服务和配置文件都按以前的，这次写了的以这次为准）；不要旧数据就用 --name 换个名字装，或者由用户执行 message 里卷名对应的 docker volume rm 后再装"},
	{CodeDisconnected, "这个应用或配方来自已断开的私有目录（授权被撤销或目录被停用）；已装好的照常运行，但不能再装、不能升级", "告诉用户联系给连接码的人；拿到新连接码后 acornfox catalog connect 新码，再装"},
	{CodePrivateDenied, "私有目录的文件（应用包或镜像）没从下载网关取到：网关不给（这台机器对这个目录没有授权了、目录停用，或文件不在目录里），或者这台服务器关掉了下载网关、连不上网关、本机时钟不准、下载多次都没下完，或者私有目录的清单没写镜像包、没有这台机器 CPU 架构的镜像包。私有应用的文件只走下载网关，不去公共源找", "看 gateway_code：GW-CAT-* 先 acornfox catalog sync 看目录是否已断开，还连着就请给码的人检查目录；GW-AUTH-01 先校准本机时间（acornfox doctor 看 SETUP-CLOCK-01）；GW-NET-01 或下载中断就检查网络后重装；网关被关掉时问用户是否打开；message 说清单没写镜像包、或没有这台机器架构（例如 linux/arm64）的镜像包时，告诉用户联系给码的人补上"},
	{CodeNoDisk, "Docker 存镜像的分区剩余空间不够这次要装的应用（清单的 disk_mb.min 之和，含下载的镜像包和解开后的镜像）", "问用户：删掉不用的应用或镜像（acornfox delete、docker image prune 由用户确认后执行），或扩容磁盘；acornfox doctor 看各分区剩余空间"},
	{CodeBadPlatform, "改平台变量不对：没有这个变量、这个变量不能改，或值的格式不对", "acornfox catalog platform-vars 看有哪些、哪些能改；按 message 改 --set platform.名字=值"},
	// The action here covers both cases for the fault catalogue; a fault
	// itself carries the hint of its own case (see checkPackage).
	{CodeBadManifest, "应用包里的清单有这个版本的 AcornFox 不认识的写法（字段、取值或格式版本），安装前就拒绝了，什么都没建、没下载镜像", "错误里带 needs_platform 的：先升级 AcornFox 到那个版本再装。不带的：是这份目录有问题，不是操作不对，过一会儿 acornfox catalog sync 后再试，还不行就把 message 反馈给我们；不要想办法绕过"},
	{CodeStuck, "安装任务还在进行，但正在做的那一步用的时间超过了它的卡住门槛（各步骤的正常时长和门槛写在程序里，install status 输出的 timing.step 有 normal_seconds、stuck_after_seconds）。这是提醒，不是失败：任务没有停，到了 AcornFox 自己的时限会判失败并清理（从公共源拉一个镜像所有源加起来 50 分钟、启动 15 分钟、健康检查按清单的 timeout_s、整次 60 分钟，从公共源拉镜像的时间不算在这 60 分钟里）", "先运行 next_command（就是 timing.step.look：下载镜像等步骤是 acornfox doctor --json，启动、健康检查、初始化是 acornfox logs 应用 --tail 200 --json）看原因，只读、不用问用户；把 say 和看到的要点告诉用户，问用户接着等还是不装了；不要自己删除、重装或重启服务"},
	{CodeNoService, svcNoServiceMeaning, svcNoServiceAction},
	{CodeChooseService, svcChooseMeaning, svcChooseAction},
	{CodeBadUse, svcBadUseMeaning, svcBadUseAction},
	{CodeIssue, svcIssueMeaning, svcIssueAction},
	{CodeRevoke, svcRevokeMeaning, svcRevokeAction},
	{CodeLegacyManifest, legacyMeaning, legacyAction},
	{CodeCheckStop, svcCheckStopMeaning, svcCheckStopAction},
	{CodeTaken, svcTakenMeaning, svcTakenAction},
	{CodeRender, renderMeaning, renderAction},
	{CodeSegDL, segDLMeaning, segDLAction},
	{CodeVarsStale, varsStaleMeaning, varsStaleAction},
	{CodeApplyCannot, applyCannotMeaning, applyCannotAction},
	{CodeApplyEdit, applyEditMeaning, applyEditAction},
	{CodeApplyFailed, applyFailedMeaning, applyFailedAction},
	{CodeApplyUndone, applyUndoneMeaning, applyUndoneAction},
	{CodeApplyConfirm, applyConfirmMeaning, applyConfirmAction},
	{CodeUpgCannot, upgCannotMeaning, upgCannotAction},
	{CodeUpgNeed, upgNeedMeaning, upgNeedAction},
	{CodeUpgChanged, upgChangedMeaning, upgChangedAction},
	{CodeUpgConfirm, upgConfirmMeaning, upgConfirmAction},
	{CodeUpgFailed, upgFailedMeaning, upgFailedAction},
	{CodeUpgUndone, upgUndoneMeaning, upgUndoneAction},
	{CodeRollbackCannot, rollbackCannotMeaning, rollbackCannotAction},
	{CodeRollbackConfirm, rollbackConfirmMeaning, rollbackConfirmAction},
	{CodeRollbackFailed, rollbackFailedMeaning, rollbackFailedAction},
	{CodeRollbackUndone, rollbackUndoneMeaning, rollbackUndoneAction},
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
	// Attempts is what each image source did for CAT-INST-12 (source: the
	// registry host, code: skipped, slow, stalled, timeout, cancelled,
	// not_found, unreachable or failed; cut_off only from a runner whose
	// caller sent a deadline, which an install does not).
	Attempts []catalog.Attempt `json:"attempts,omitempty"`
	// Diagnosis carries the deployment's diagnosis for CAT-INST-15/16.
	Diagnosis any `json:"diagnosis,omitempty"`
	// TaskID is the running task for CAT-INST-04.
	TaskID string `json:"task_id,omitempty"`
	// NeedsPlatform is the AcornFox version to upgrade to: set for
	// CAT-INST-08 and for the CAT-INST-29 that an upgrade would cure, empty
	// for every other fault (a CAT-INST-29 without it is a broken catalog).
	// The same name as in the catalog list, so a page never has to guess
	// "upgrade first" from the message text.
	NeedsPlatform string `json:"needs_platform,omitempty"`
	err           error
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
	if s, ok := serviceFaultStatus[f.Code]; ok { // CAT-SVC-* (services.go)
		return s
	}
	switch f.Code {
	case CodeUnknown, CodeTaskNotFound, CodeNotCatalogApp, CodeConnUnknown:
		return http.StatusNotFound
	case CodeBadParams, CodeUnsupported, CodeConnFormat, CodeBadPlatform, CodeChooseService, CodeBadUse:
		return http.StatusBadRequest
	case CodeNameTaken, CodeBusy, CodeNoMemory, CodeNoDisk, CodeArch, CodeNoPort, CodeTooOld, CodeOldVolumes, CodeNoService,
		CodeDisconnected, CodeConnNoGateway, CodeBadManifest, CodeLegacyManifest, CodeTaken, CodeRender,
		CodeApplyCannot, CodeApplyEdit, CodeApplyConfirm,
		CodeUpgCannot, CodeUpgNeed, CodeUpgChanged, CodeUpgConfirm, CodeRollbackCannot, CodeRollbackConfirm, "BKP-03", "BKP-09":
		return http.StatusConflict
	case "BKP-01":
		return http.StatusInsufficientStorage
	case "BKP-11":
		return http.StatusNotImplemented
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

// The two hints of CAT-INST-29. With needs_platform an upgrade cures it;
// without, the catalog is wrong and upgrading would not help.
const hintBadManifestCatalog = "这是这份目录有问题，不是操作不对，升级 AcornFox 也没用：过一会儿 acornfox catalog sync 后再试，还不行就把 message 反馈给我们；不要想办法绕过"

func badManifestUpgradeHint(need string) string {
	return fmt.Sprintf("先升级到 %s：这台 AcornFox 版本太旧，升级后再装这个应用。升级要先问用户；Linux 服务器用 upgrade.sh", need)
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

// What CAT-INST-31 … 35 mean and what to do (the fault catalog says the
// same, word for word).
const (
	svcNoServiceMeaning = "这个应用必须用到的一种服务（例如大模型接口），这台机器上没有能用的，安装前就停下了，什么都没装。平台不会替用户去装别的应用来提供它"
	svcNoServiceAction  = "你手上已经有这种服务的话（地址、Key 等），照 message 里的命令登记一条（acornfox services add 类型 --name 名字 --set 项=值，保密的项写成 项=-，从标准输入给值；命令里的 … 要换成用户给的真实值，先问到再给命令）；登记好以后先运行 acornfox services test 名字 试一下通不通，通了再看一次计划（acornfox install 应用 --plan），能装了再装；这台机器上有、但现在不能用的（提供它的应用停着、还差必填项），--plan 的 how 里写了怎么让它能用"
	svcChooseMeaning    = "这台机器上有好几条同一类型的服务都能用，平台不替用户选，要指定用哪一条"
	svcChooseAction     = "从 message 里列的几条服务里选一条用：安装时加 --use 需要名=服务名 选一条，页面上在“需要的服务”里选"
	svcBadUseMeaning    = "安装时指定的服务（--use）不对：这台机器上没有这个名字的服务、类型不对、现在不能用（例如提供它的应用停着），或者要装的应用里没有这个需要"
	svcBadUseAction     = "acornfox services list 看有哪些服务和它们的状态；acornfox install 应用 --plan 看应用有哪些需要；改对 --use 再装。提供这条服务的应用停着的，先启动它（acornfox start 应用名）"
	svcIssueMeaning     = "提供服务的应用没能给这个应用发凭据（例如在大模型网关里建令牌、在数据库里建库和账号）：提供方没在运行，或者它清单里写的步骤没走通：发请求的应答不对（message 说“这个应用限制了请求次数”的，是提供方回了 HTTP 429，平台没有自动重发），或者在它容器里执行的命令退出码不对、超时（命令可能还在容器里运行，没有重新执行）、输出太多。这次安装已失败并清理，发了一半的凭据已尽量收回，提供方清单写了删数据步骤（purge）的，这次建了一半的库或数据也已照着删掉；收不回、删不掉的，任务的 apps[].carried 里有一条 CAT-INST-35 提醒"
	svcIssueAction      = "看 message 是哪个提供方、哪一步（命令失败的写了退出码和标准错误的最后几行）；acornfox status 提供方 看它是否在运行，acornfox logs 提供方 --tail 200 看日志；说限制了请求次数的，过一会儿再装；提供方的管理员密码在应用里改过的话，平台登不进去：请在提供方应用里手工建一个凭据，登记成服务（acornfox services add …）后用 --use 指定"
	svcRevokeMeaning    = "发给某个应用的凭据没有收回：收回的步骤没走通，或者接旧数据重装时旧的那份没通过检查、换了一份新的，旧的留在了提供方里，或者应用是用旧格式的清单装的、删除时平台不再替它收回；也可能是应用连数据一起删时，提供方清单里删数据的步骤没走通，给它建的库或数据还留在提供方里；或者安装没成功（或两个安装同时抢一条服务、这个输了）回滚时，平台收回了这次刚发的凭据，再照提供方清单的删数据步骤删这次给它新建的库或数据，没删成；或者这次安装的记录里没写明发的是哪一份凭据（旧版本留下的记录），平台不能确认那份库是这次新建的，所以没有删；或者提供方清单里删数据的步骤不是只按这次发凭据的编号找要删的东西（那可能是装之前就有的数据），回滚时平台也只收回凭据、没有删。这只是提醒：删除、回滚或安装照常完成了，但那个凭据（或那份数据）在提供方应用里可能还在、还能用"
	svcRevokeAction     = "到提醒里写的那个应用里，按写的名字或编号看一下那一项，用不上就手工删掉；那个应用停着的，启动后再删"
	legacyMeaning       = "目录里这个应用还是按旧格式的清单（第 2 版及更早）上架的。这一版平台只装新格式（第 3 版）的清单，旧的不装也不转换，安装前就拒绝了，什么都没建（目录的索引里已经标明是旧格式的，连应用包也不下载；没标明的，应用包会先下载到本机缓存，读了清单才知道）。这不是操作不对。用旧清单装好的应用照常运行，能启停、删除。应用中心的列表里这样的应用标着“这是旧格式的清单，这一版装不了，等这个应用按新格式重新上架”（接口里 former 是 true），点进详情也报这个编号"
	legacyAction        = "这个应用要等按新格式重新上架后才能装，现在装不了，不是你的操作不对。已经装着的旧应用照常用；要用需要的服务、自动接上这些新功能，得等新清单上架后删了重装"
	svcCheckStopMeaning = "接旧数据重装时，这个应用以前从一条服务拿到的凭据（例如数据库的账号和密码）没通过提供方清单里写的核对。提供方的清单写了“核对不过就停下”（存数据的服务这样写：换一份新的凭据就等于接到一个空的新库，原来的数据就接不上了），所以平台没有换新的，这次安装停下并清理了，原来的接入和凭据原样留着"
	svcCheckStopAction  = "先查提供方：acornfox status 提供方 看它是否在运行，acornfox logs 提供方 --tail 200 看日志；提供方好了再装一次（还是 --reuse-data）。确认原来的数据还在不在（例如用户在提供方里删过库、改过密码就会这样）。只有用户确定不要原来的数据时，才改接别的服务：加 --use 需要名=服务名 再装；不要自己删库、改密码"
	svcTakenMeaning     = "要接的那条服务现在接不了这个应用：别的应用把它独占了（清单写了只接没有别的使用方在用的服务），这个应用要独占它但已经有别的应用在用，或者提供它的应用限制了同时接几个、已经接满。删了但数据还留着的应用也占着它的位置（它装回来时要接回去）。安装前就停下了，或者在接的那一刻发现被同时在装的别的应用先接上了，这次安装已清理（这次刚发的凭据已收回，提供方清单写了删数据步骤的，给它新建的库或数据也已删掉；没删成的在任务的 apps[].carried 里有 CAT-INST-35 提醒）"
	svcTakenAction      = "acornfox services list 看每条服务被谁独占、还能接几个；acornfox install 应用 --plan 看现在能接哪条。有别的同类服务就用 --use 需要名=服务名 指定；没有就登记一条你已有的（acornfox services add …）。占着位置的是删了但数据还留着的应用、用户也确认它不要了：它已经删了，不能直接 delete --volumes，也不要手工 docker volume rm（接入记录还在，位置照样占着）；问过用户后先 acornfox install --name 原来的名字 --reuse-data 装回来，再 acornfox delete 原来的名字 --volumes 连数据一起删，位置才让出来。不要为了腾位置去删别的应用，要删先问用户"
	segDLMeaning        = "镜像包经下载网关分段下载没成：下载缓存写不了（磁盘满了或没权限）、某一段连着多次下不下来，或者下载地址反复失效。已经下好的部分留在下载缓存里，下次接着下；公共目录的应用接着换镜像站拉，私有目录的应用停下"
	segDLAction         = "看 message 是哪一种：写缓存失败先看磁盘空间和权限（acornfox doctor 的 SETUP-DISK-01）；某一段多次下不下来，多半是网络或网盘暂时不通，稍后重装会从缓存接着下，已下的不用重下；缓存在数据目录 uploads/dl-cache，3 天没动会自己清，磁盘紧张时可以由用户确认后删；要退回单连接下载，环境变量 ACORNFOX_DL_SEGMENTED=0"
	renderMeaning       = "清单里写的一个值算不出来：环境变量、启动命令、配置文件、首次登录信息或初始化步骤里的 {{…}} 写法读不了（检查工具会报 CAT-LINT-46、47），一个变换没做成（例如 map 对照表里没有取到的那个值，又没写 \"*=值\" 兜底），if 条件读不了，同名的两条环境变量的条件同时成立，或者启动命令引用了保密的值。这是清单的问题，不是操作不对；这次安装已失败并清理"
	renderAction        = "看 message 是哪一处的哪个 {{…}}（message 里不带取到的值）。是 map 没对上、条件同时成立这类、要靠换一个值解决的（例如换一种对外方式、--set 改设置项；acornfox catalog platform-vars 看本机平台变量的值），问用户后再装；写法读不了、启动命令带保密值的，是目录的问题，把 message 反馈给我们，不要想办法绕过"
	varsStaleMeaning    = "应用装的时候，平台把这个平台变量的值（多半是应用的访问地址 app.url、app.scheme、app.host）算好写进了应用；装好以后改了对外方式、绑了域名，或者这台机器的设置变了，现在算出来的和装时的不一样了。应用里用的还是装时的值（例如应用里的 webhook 地址还是旧的），应用照常运行：要让它用上现在的值，用 acornfox apps reapply 重新生效。这只是提醒，不是出错"
	varsStaleAction     = "apps vars 里 value 是装时的值，value_now 是现在算出来的值。不影响用就先不动；要让应用用上现在的值：先 acornfox apps reapply 应用 --plan 看会改什么、会不会重启它，经用户同意后 acornfox apps reapply 应用 --yes（会按现在的值重写它的环境变量和配置文件并重启，数据不动，失败自动退回）；一次看有哪些应用落后了：acornfox apps affected"
	applyCannotMeaning  = "这个应用现在不能重新生效：它不是从应用中心装的、装的时候没保存清单或用的是旧格式的清单（那样算不出每一项该是什么），还没有上线，正在装或正在重新生效，或者它停着而这次改动要在应用里做步骤（停着的应用不能在里面做事）。什么都没改"
	applyCannotAction   = "看 message 是哪一种：正在装或正在重新生效的，用 acornfox install status 任务编号 --wait 等它结束再来；停着的先问用户，同意后 acornfox start 应用，再重新生效；清单没存或是旧格式的，这个应用只能删了重装（保留数据 --reuse-data，要先问用户）"
	applyEditMeaning    = "这一项设置装好以后不能改：清单写了 editable: never（改了应用会坏，例如加密密钥）或 reinstall（要重装才能改），或者这是平台生成的值，或者把必填项清空了、值的格式不对。什么都没改"
	applyEditAction     = "按 message 说的做：不能改的，不要想办法绕过（不要手工 env set）；写了“重装才能改”的，问用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data）；值不对的，改对再试。acornfox apps vars 应用 看每一项能不能改"
	applyFailedMeaning  = "重新生效没成功，平台已经把环境变量、启动命令和配置文件、应用的容器都退回了原来的样子，应用照原来的设置在运行，改动没有留下。message 里写了没成功的原因和它的编号（新的容器没起来、健康检查没通过，或清单里“重新生效后做的步骤”没通过）"
	applyFailedAction   = "把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；多半是用户改的值让应用起不来（例如端口、地址写错），改对了再试一次；先用 acornfox apps set … --plan 看会改什么。应用已经退回，不用再处理"
	applyUndoneMeaning  = "重新生效没成功，而且退回原来的样子时也出了错：应用可能停在新旧设置混在一起的状态（环境变量、配置文件或容器有的是新的、有的是旧的）。message 里写了两边的原因"
	applyUndoneAction   = "这是要人处理的：先把 message 告诉用户，不要再重复运行重新生效，不要改密钥或删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；acornfox apps vars 应用 看环境变量是新的还是旧的；用户同意后可以 acornfox restart 应用，或 acornfox apps reapply 应用 --force 再按保存的设置生效一次"
	applyConfirmMeaning = "这次改动会重启这个应用（它的地址或设置变了，应用里用到这些值），请求里没有写明用户同意重启，所以什么都没改。这是确认规则，不是出错"
	applyConfirmAction  = "把 message 里写的会改什么、会重启哪个应用告诉用户，问他同意后在同一条命令上加 --yes（接口里是 confirm_restart: true）再运行一次；用户不想重启，可以加 --no-reapply（接口里 reapply: skip）只改访问方式，应用里的地址以后用 acornfox apps reapply 再对"
	retiredMeaning      = "已不再使用的编号：以前是平台内置的大模型来源（--llm）出的错。大模型接口现在是一种普通的服务，这一类问题报 CAT-INST-31～35。编号保留，不会再分给别的错误"
	retiredAction       = "这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理"
)
