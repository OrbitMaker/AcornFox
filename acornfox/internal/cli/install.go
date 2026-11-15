package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/installtiming"
)

// App center commands: browse the catalog on the server and install from it.
// Fault codes (CAT-INST-*) and what to do about them are defined in
// internal/appinstall/faults.go; the server sends the hint with each error.

const catalogBrowseUsage = `应用中心（在服务器上看目录、装应用）：
  catalog list           目录里的应用和配方、目录序号、上次同步结果。只读。
                         这台 AcornFox 太旧、装不了的应用和配方后面写“要先升级到 x.y.z”
                         （--json 里是 needs_platform，不用升时是空串）：先升级 AcornFox 再装
  catalog info 应用      应用详情和安装时能填的参数（不含平台生成的密码）。只读
  catalog sync           让服务器现在同步一次目录
  catalog platform-vars  平台变量：平台交给应用的时区、下载源、访问地址等，这台机器上的值、哪些能改。只读
从目录安装：acornfox install 应用 / acornfox install --recipe 配方（acornfox install -h 看详情）
装好以后看它是用什么设置装的：acornfox apps vars 应用（只读）`

const catalogConnectUsage = `用法：acornfox catalog connect 连接码

用途：用连接码把这台服务器连到一个私有应用目录。连接码由目录的管理者生成，形如 AFXC-XXXX-XXXX-XXXX-XXXX，
  一个码只给一台机器用，用过就失效（这台机器自己重复用同一个码不算新的一次）。连上后立刻同步一次，
  私有目录里的应用会出现在 catalog list 里，标着来源。
副作用：服务器用本机钥匙签名，把连接码发给下载网关；网关把这台机器登记到这个私有目录名下；
  服务器记下目录编号和名称（不记连接码）。连接码不写进日志。
退出码：0 连上；2 参数不对；3 连不上服务器；4 被拒绝（带 CAT-CONN 编号）。
出错编号：CAT-CONN-01 格式不对（没发出去）；02 服务器关了下载网关；03 码不存在或已被别的机器用过；
  04 已过期；05 目录已停用；06 试得太频繁（等几秒：--json 里是 diagnosis.retry_after，普通输出写在失败原因里）；
  07 连不上网关或网关拒绝（GW- 编号：--json 里是 diagnosis.gateway_code，普通输出写在失败原因里）；
  08 网关已授权但本机没记下（用同一个码再连一次）。
例子：
  acornfox catalog connect AFXC-7K2M-Q9TD-X4HB-C3PW
  acornfox --json catalog connect AFXC-7K2M-Q9TD-X4HB-C3PW`

const catalogSourcesUsage = `用法：acornfox catalog sources

用途：列出这台服务器的应用目录：公共目录一个，加上连接过的私有目录。每个写序号（0 表示还没有可用的索引）、
  应用数、上次同步时间和结果编号、是否已断开。已断开的私有目录（授权被撤销或目录被停用）不再显示它的应用；
  从它装好的应用照常运行，但不能再装、不能升级。
副作用：无，只读。
例子：
  acornfox catalog sources
  acornfox --json catalog sources`

const catalogPlatformVarsUsage = `用法：acornfox catalog platform-vars [--offline]

用途：列出平台变量。应用的清单不写死时区、下载源这类底层的东西，而是引用平台变量，
  装的时候由平台按这台机器填进去，用户不用配。每项写：这台机器上的值、单机版和云版各从哪取、能不能改、
  从哪个平台版本起有（--json 的 since；清单用到它，就至少要这个版本的 AcornFox 才能装）。
  能改的，装应用时用 --set platform.名字=值 改（只对这次装的应用生效）。
  --offline：不连服务端，直接输出这个程序里的整张表（没有这台机器上的值）。
  --json 时每项还写什么时候算（refresh）、哪些版本有值（editions）、能改的值要符合什么（rule、rule_label），
  连服务端和 --offline 都有，人读的输出不显示这几项。
  给别的系统读表用：acornfox --json catalog platform-vars --offline > platform-vars.json
副作用：无，只读。
例子：
  acornfox catalog platform-vars
  acornfox --json catalog platform-vars
  acornfox --json catalog platform-vars --offline
  acornfox install demo-app --set platform.tz=Asia/Tokyo`

const installUsage = `用法：acornfox install 应用 [--name 应用名] [--version 版本] [--set 参数=值]...
                         [--no-wait] [--timeout 30m] [--show-secrets] [--keep-data-on-failure] [--reuse-data]
                         [--plan] [--use 需要名=服务名]... [--expose local|lan|public]
      acornfox install --recipe 配方 [--set 应用.参数=值]... [--use 应用.需要名=服务名]... [--no-wait] …
      acornfox install --name 应用名 --reuse-data [--set 参数=值]... [--plan] …
      acornfox install status 任务编号 [--wait] [--timeout 秒] [--show-secrets]

用途：从应用中心的目录在服务器上一次装好一个应用（或一个配方里的几个应用并连起来）：建应用、写配置、
  下载镜像、启动、健康检查、初始化。装成一个安装任务，可以查进度；任一步失败会自动清理这次建出来的东西。
副作用：会改数据（建应用、数据卷、镜像、容器）。默认按清单写的对外方式，多数是只本机能打开（exposure local）；
  加 --expose 时按它（见下面）。
  应用需要的服务（例如大模型接口）接的是这台机器上别的应用提供的、要按使用方单独发凭据的，会在那个应用里
  给它建一份凭据（例如一把令牌）；提供服务的应用登记进服务登记表（acornfox services list 看）。
参数：
  --name 名字          应用名，默认用目录里的 id
  --version 版本       只能是目录里的版本（acornfox catalog info 看）
  --set 参数=值        可以给多次；左边写参数名（acornfox catalog info 里的 key，例如 model），不写中文说明（label）；
                       写错了报 CAT-INST-02，报错里列出这个应用有哪些参数；配方里的参数可加应用前缀：--set demo-app.admin_username=root；
                       平台变量写 platform. 前缀：--set platform.tz=Asia/Tokyo（acornfox catalog platform-vars 看哪些能改）
                       保密的参数（密码这类，acornfox catalog info 里类型是 secret）不要写在命令行里：写成 --set 参数=-，
                       命令从标准输入读一行当它的值（有几个就按出现的顺序读几行），和 services 命令一样；
                       读到的值在输出里一律遮住。不是保密参数时“-”就是值本身。加 --plan 时不读，当作还没填
  --no-wait            只提交，输出任务编号；之后用 acornfox install status 任务编号 --wait 等它装完
  --timeout 时长       最多等多久（默认 30m）；等不到不会取消安装
  --show-secrets       显示首次登录的密码（默认遮成 ******）
  --keep-data-on-failure  失败时保留数据卷，方便排查（默认清理）
  --plan               只看安装前的计划，什么都不装（只读，没有副作用）。只讲这个应用（或这个配方里的几个应用）
                       自己的事：装哪几个（配方按顺序）；应用需要的每一种服务（例如大模型接口、邮箱）怎么接：
                         matched 已接上（service 是哪一条；配方里接的是前面那个应用装好后才登记的，pending_from 写是谁；
                           那条服务是别的应用提供的、它的清单里写了要用户在那个应用里做一步的，tell 里原样是那些话，
                           装之前就告诉用户；那一项已经填了的不再列；那条服务现在“还不能用”（提供它的应用的就绪检查
                           没过）时 tell 里也有一句“服务 X 现在还不能用：…”，不挡安装，也照原话告诉用户；没有时没有 tell）、
                         optional_missing 可选的，没有，先跳过（without 写了少了它会怎样）、
                         required_missing 必须的，没有（how 里是登记一条你已有的命令：acornfox services add …，
                           必填项都列出，保密项写成 项=-、运行时从标准输入给值；这台机器上有同类、但现在不能用的，
                           how 里先说怎么让它能用：启动提供它的应用、填上还差的项、用旧数据把已删的提供方装回来）、
                         choose 有好几条能用，要用 --use 选一条（candidates 是能选的）；
                       被别的应用独占的服务、已经接满（提供方限制了同时接几个）的服务不算能用；这个需要写了 dedicated
                       （要独占）时，已经有别的应用在用的也不算：都写在 how 里，说明是被谁占着
                       （删了但数据还留着的应用也占着位置）。看计划时会现跑服务的就绪检查（只读，每条最多几秒）；
                       还有哪些必填参数没填（params_missing）、装不了的原因（problems）。
                       平台不会替你装别的应用来提供服务：要几个应用一起装好、接好，装配方（--recipe）。
                       --json 时输出 {can_install, say, steps, apps, problems}：can_install 为 true 才能装；
                       say 可以原样告诉用户。装之前先看计划，是推荐的做法
  --expose 对外方式   装好时就用这个对外方式，不用装完再 acornfox app set --expose：local 只本机、lan 本机和局域网、
                       public 所有网卡加域名。这样应用拿到的访问地址（平台变量 app.url、app.scheme、app.host，
                       例如应用里的 webhook 地址）一开始就是对的；装完再改对外方式，这一版不会改写已经写进应用的地址
                       （acornfox apps vars 应用 会标 APP-VARS-01）。开到局域网、公网是对外开放，要先问用户。
                       只在内部网络的应用（数据库这类）不能用；配方里对这次要装的、不是只在内部网络的应用都生效。
                       写错了报 CAT-INST-02
  --use 需要名=服务名  指定应用的某个需要接哪条服务（acornfox services list 看有哪些），可以给多次；
                       配方里写成 --use 应用名.需要名=服务名。只影响这次安装，本身没有别的副作用
  --reuse-data         接上以前同名应用留下的数据卷（删应用时保留了数据），不加时这种情况报 CAT-INST-24。
                       接上的旧数据装失败也不会删。没写的照旧：账号密码和以前一样；以前用 --set platform.* 改过的平台变量
                       恢复；需要的服务优先接原来那条，原来发给它的凭据检查过还能用就接着用，检查不过就换一份新的
                       （旧的不删，提醒用户去提供它的应用里看）；提供方的清单写了“核对不过就停下”的（数据库这类，
                       换一份等于接到空库），检查不过时安装停下（CAT-INST-37），原来的接入原样留着；
                       这种情况加 --plan 时就会先只读地核对一次（最多几秒），明确没过的 problems 里报 CAT-INST-37；
                       只是没来得及核对完、提供方没应答的，不挡安装，只在 needs[].tell 里说装的时候会再核对；
                       数据里已有的配置文件不重写（平台要填的内容变了才重写，旧文件先另存为 文件名.bak-时间）。
                       这次写了的（--set、--use）以这次为准。沿用了什么，进度和任务的 apps[].carried 里写着；
                       carried 里以 CAT-INST-35 开头的要原样告诉用户（有一份凭据没收回，要用户到提供它的应用里看）。要先问用户。
                       只知道应用名、不知道它是应用中心里的哪个应用时，不写应用、只写 --name 应用名 --reuse-data：
                       平台从删除时保留下来的记录里找到它原来是哪个应用、来自哪个目录（版本照常用目录里现在的，
                       或 --version 指定）；这台机器上没有那份记录（不是从应用中心装的，或者是很旧的版本删的）
                       就报 CAT-INST-02，说明找不到，这时要写上应用中心里的名字
输出：--json 时提交后输出 {"ok":true,"task_id":…,"next_command":…}（--no-wait）或任务结束后的完整任务（字段见下面“查进度”）。
退出码：0 装好（status：进行中或装好）；1 安装失败；2 参数不对；3 连不上服务器；4 服务器拒绝（带 CAT-INST 编号）。
查进度（install status 任务编号，只读）：
  默认查一次就返回。加 --wait 时等到装好或失败才返回（每 2 秒查一次），最多等 --timeout 秒（默认 90，
  最多 600，超过按 600 算；写 90、90s、2m 都行；只写 --timeout 也算 --wait）；到时还没装完就照实返回
  当前进度，安装照常进行，再运行一次接着等。
  --json 输出是任务本身，另加下面几项，照着用，不用自己判断：
    done            true：有结果了（status 是 succeeded 或 failed）；false：还在装，结果还不知道，不要下结论
    stuck、slow     stuck 为 true：正在做的那一步比平常慢很多（CAT-INST-30），还没失败；slow：比平常慢一些
    say             可以原样告诉用户的话（到了哪一步、这一步和整次各用了多久）
    next、next_command  下一步做什么、运行哪条命令（还在装时就是接着等的命令）；装好后 next 只写顺序和看哪一项
    show_secrets_command  装好后、首次登录里有密码这类保密项时才有：查密码的命令（带这次的任务号），交给用户在服务器上自己运行，单独一行；没有这一项就是没有密码要查
    must_tell       装好后必须照原话告诉用户的话：没收回的凭据（apps[].carried 里以 CAT-INST-35 开头的），
                    接上的服务那条还空着、这个应用又用得到的项（apps[].tell，和计划里的 tell 一样），
                    装好时现跑就绪检查发现“还不能用”的服务（“服务 X 现在还不能用：…”）
    first_login[].url_note  首次登录的地址只有服务器本机能打开时的说明，可以原样告诉用户（从用户自己的电脑打开看 open_hint）
    apps[].open_hint  装好后、只在服务器本机能打开的应用怎么打开，和 acornfox --json apps 里这个应用的 open_hint 一样：
                    有 ask（recommend 是 lan、还没开放）时把 ask 原样问用户并等回答，同意后运行 ask_yes_command，
                    改好以后才能用 lan_url_after_expose 打开（之前打不开，不要当成现在能用的地址）；
                    不同意或没有 ask：把 command（用户电脑上有 acornfox 命令行时）和 tunnel_command（没有时，
                    “用户@服务器地址”由用户换成自己的，再打开 tunnel_url）各放一个代码块给用户；
                    两条都在用户自己电脑上运行，不在服务器上：command_where、tunnel_where 是写给用户的这句话；
                    lan_url 只在已经开放到局域网时才有；没有 open_hint 就用 apps[].url
    apps[].internal true：这个应用只在内部网络（数据库、缓存这类，给别的应用用），没有地址、没有网页，不要叫用户打开它
    elapsed_seconds 整次用了多久；timing 里是细节：timing.step 是正在做的那一步（elapsed_seconds、
                    normal_seconds 正常最多多久、stuck_after_seconds 过了算卡住、typical、look 看原因的只读命令），
                    timing.steps 是每一步用了多久
    带 --wait 时另有 waited_seconds（这次等了多久）、timed_out（等到时限还没装完）
  退出码：0 装好或还在装（看 done）；1 安装失败；3 连不上服务器；4 没有这个任务（CAT-INST-21）。
常见编号：CAT-INST-03 同名应用已存在；04 正在装；05 内存不够；
  12 镜像拉不到；15 没能上线；16 健康检查不过；17 初始化失败；24 以前同名应用的数据卷还在（先问用户，要旧数据就加 --reuse-data）；
  27 磁盘不够；28 平台变量改法不对；
  08 这台 AcornFox 比应用或配方要求的旧（先升级再装；--json 里 diagnosis.needs_platform 是要升到的版本）；
  29 应用包里的清单有这个版本不认识的写法：diagnosis 带 needs_platform 就先升级，不带就是这份目录有问题，稍后再试或反馈。
  30 某一步比平常慢很多、还没失败（install status 输出 stuck: true；先运行 next_command 只读看原因）。
  31 必须用到的服务这台机器上没有：问用户手上有没有现成的（地址、Key），有就按 message 里的命令登记一条，
     登记好以后先运行 acornfox services test 名字，通了再看一次计划（--plan）再装；
  32 同一类服务有好几条，问用户用哪条，安装时加 --use 需要名=服务名 选一条；33 --use 指定的服务不对（没有、类型不对或现在不能用）；
  34 提供服务的应用没能发凭据（安装已失败并清理；说“限制了请求次数”的过一会儿再装）；35 有一份凭据没收回（只是提醒，要用户到提供方应用里看、手工删）；
  36 应用包里的清单是旧格式的，这一版装不了，要等应用按新格式重新上架（不是操作的问题，不要绕过）；
  37 接旧数据重装时原来的凭据核对没过，提供方写了核对不过就停下（先查提供方、确认数据还在，不要自己删库改密码；
     用户确定不要原来的数据才 --use 改接别的服务）；
  38 要接的服务被别的应用独占、已经接满，或者这个应用要独占它但已经有别的应用在用（acornfox services list 看被谁占着；
     有别的同类服务就 --use 指定，没有就登记一条你已有的；不要为了腾位置删别的应用）。
  完整列表见 docs/capabilities.md。
例子：
  acornfox install demo-app
  acornfox install --recipe demo-bundle            # 配方：几个应用按顺序一起装，并接好
  acornfox --json install demo-app --no-wait
  acornfox install status 3f9a0c1b2d4e
  acornfox --json install status 3f9a0c1b2d4e --wait   # 等到装好或失败，最多 90 秒；没装完就再运行一次
  acornfox install demo-app --reuse-data           # 删掉时保留了数据，接上旧数据重装（换应用版本也这样做）
  acornfox install --name my-gw --reuse-data       # 只知道应用名：平台从保留的记录里找到它原来是哪个应用
  acornfox install 应用 --plan                     # 先看计划：需要的服务接哪条、还缺什么（只读）
  acornfox --json install 应用 --plan
  acornfox install 应用 --use llm=我的上游          # 有几条同类服务时，指定用哪一条
  acornfox install demo-app --expose lan          # 装好就能在局域网别的电脑上打开（先问用户）
装好以后：
  只本机能打开的应用，在你自己的电脑上运行 acornfox open 应用名 打开（经 SSH 隧道，不用放行安全组）。
  应用接上的服务是别的应用发的凭据时，凭据有没有额度、会不会过期由那个应用决定；要控制花费，到那个应用里给它设上限。
另：不带应用名的 acornfox install 是在这台电脑上安装 AcornFox 本身，这一版没有开放（Mac 上只有命令行客户端）；
  Linux 服务器用 install.sh 安装、upgrade.sh 升级，见使用文档。`

// setFlag collects repeated --set k=v.
type setFlag []string

func (s *setFlag) String() string     { return strings.Join(*s, ",") }
func (s *setFlag) Set(v string) error { *s = append(*s, v); return nil }

func (a *app) appCenterAPI(ctx context.Context) (client.API, client.AppCenterAPI, int) {
	t, _, err := a.resolveTarget()
	if err != nil {
		return nil, nil, a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return nil, nil, a.out.fail(derr)
	}
	ac, ok := api.(client.AppCenterAPI)
	if !ok {
		api.Close()
		return nil, nil, a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持应用中心", Hint: "升级 AcornFox"}})
	}
	return api, ac, -1
}

// emitRaw prints the server's JSON answer with "ok": true added.
func (a *app) emitRaw(raw json.RawMessage, extra map[string]any) {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	for k, v := range extra {
		m[k] = v
	}
	a.out.emitJSON(m)
}

func (a *app) cmdCatalogList(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog list", catalogBrowseUsage)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := ac.Catalog(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, nil)
		return exitOK
	}
	a.out.human("目录序号 %d（%s）；同步：%s", v.Serial, map[string]string{"cache": "已同步的目录", "snapshot": "程序内置的目录"}[v.Origin], v.Sync.Message)
	if len(v.Recipes) > 0 {
		a.out.human("")
		a.out.human("配方：")
		for _, r := range v.Recipes {
			apps := []string{}
			for _, x := range r.Apps {
				apps = append(apps, x.App)
			}
			a.out.human("  %-22s %s（%s）%s%s%s  %s", r.ID, r.Title, strings.Join(apps, " + "), fromText(r.Source), upgradeFirst(r.NeedsPlatform), formerText(r.Note), r.Command)
		}
	}
	a.out.human("")
	a.out.human("应用：")
	installable := 0
	for _, x := range v.Apps {
		if !x.Former {
			installable++
		}
	}
	if installable == 0 {
		// The same words as an install of an app that is not there (the
		// server's CAT-INST-01 then): syncing cannot help.
		a.out.human("  %s", catalog.NothingToInstall)
	}
	for _, x := range v.Apps {
		state := ""
		if len(x.Installed) > 0 {
			state = "  已安装：" + strings.Join(x.Installed, "、")
		} else if !x.Supported {
			state = "  这台机器的 CPU 不支持"
		}
		state += upgradeFirst(x.NeedsPlatform) + formerText(x.Note)
		a.out.human("  %-22s %s %s，内存 %s 起%s%s", x.ID, x.Name, x.Version, mbText(x.MemoryMB.Min), fromText(x.Source), state)
		a.out.human("  %-22s %s", "", x.Summary)
	}
	return exitOK
}

// upgradeFirst marks a catalog entry this AcornFox is too old to install.
func upgradeFirst(needs string) string {
	if needs == "" {
		return ""
	}
	return "  要先升级到 " + needs
}

// formerText marks a catalog entry listed in a former manifest format (the
// server's note, which says it cannot be installed until published again).
func formerText(note string) string {
	if note == "" {
		return ""
	}
	return "  " + note
}

// fromText marks an entry of a private catalog ("（来自：名称）").
func fromText(s client.SourceRef) string {
	if s.ID == "" || s.ID == "public" {
		return ""
	}
	return "（来自：" + s.Name + "）"
}

func (a *app) cmdCatalogInfo(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog info", catalogBrowseUsage)
	from := fs.String("catalog", "", "public 或私有目录编号")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 {
		return a.out.usageError("用法：acornfox catalog info 应用")
	}
	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	d, err := a.catalogApp(ctx, api, ac, pos[0], *from)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(d.Raw, nil)
		return exitOK
	}
	a.out.human("%s %s：%s%s", d.Name, d.Version, d.Summary, fromText(d.Source))
	a.out.human("镜像 %s（约 %d MB），端口 %d，默认访问 %s，内存 %s 起（建议 %s）", d.Image, d.SizeMB, d.Port, exposureText(d.Exposure), mbText(d.MemoryMB.Min), mbText(d.MemoryMB.Recommended))
	a.out.human("许可证 %s，主页 %s", d.License, d.Homepage)
	if d.SourceCode != "" {
		a.out.human("源码 %s", d.SourceCode)
	}
	if len(d.Installed) > 0 {
		a.out.human("已安装：%s", strings.Join(d.Installed, "、"))
	}
	if d.NeedsPlatform != "" {
		a.out.human("要先升级到 %s：这台 AcornFox 版本太旧，装不了这个应用。先升级 AcornFox（Linux 服务器用 upgrade.sh，见使用文档）再装", d.NeedsPlatform)
	}
	if len(d.Params) > 0 {
		a.out.human("")
		a.out.human("安装时能填的参数：")
		for _, p := range d.Params {
			note := ""
			switch {
			case p.Generated:
				note = "留空就自动生成"
			case p.Default != nil:
				note = fmt.Sprintf("默认 %v", p.Default)
			}
			req := ""
			if p.Required {
				req = "（必填）"
			}
			a.out.human("  %s %s%s  %s", p.Key, p.Label, req, note)
			if p.Hint != "" {
				a.out.human("      %s", p.Hint)
			}
		}
	}
	if len(d.Platform) > 0 {
		a.out.human("")
		a.out.human("平台自动填的（不用你配）：")
		for _, x := range d.Platform {
			val := x.Value
			if val == "" {
				val = "（" + x.Note + "）"
			}
			can := ""
			if x.UserCanSet {
				can = "  可以改：--set platform." + x.Name + "=…"
			}
			a.out.human("  %s %s：%s%s", x.Name, x.Label, val, can)
		}
	}
	if len(d.Delivery.UserSteps) > 0 {
		a.out.human("")
		a.out.human("装好后要你做的：")
		for i, s := range d.Delivery.UserSteps {
			a.out.human("  %d. %s", i+1, s)
		}
	}
	a.out.human("")
	a.out.human("安装：%s", d.Command)
	return exitOK
}

// catalogApp fetches an app's detail, from one catalog when from is set
// (that needs a server that knows private catalogs).
func (a *app) catalogApp(ctx context.Context, api client.API, ac client.AppCenterAPI, id, from string) (client.CatalogApp, error) {
	if from == "" {
		return ac.CatalogApp(ctx, id)
	}
	pc, ok := api.(client.PrivateCatalogAPI)
	if !ok {
		return client.CatalogApp{}, errOldServer()
	}
	return pc.CatalogAppFrom(ctx, id, from)
}

func errOldServer() error {
	return &client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持私有目录", Hint: "升级 AcornFox"}}
}

func exposureText(e string) string {
	switch e {
	case "local":
		return "只本机"
	case "lan":
		return "局域网"
	case "public":
		return "公网"
	case "internal":
		return "只在内部网络（给别的应用用，没有网页）"
	}
	return e
}

func (a *app) cmdCatalogSync(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog sync", catalogBrowseUsage)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	st, err := ac.CatalogSync(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		b, _ := json.Marshal(st)
		a.emitRaw(b, map[string]any{"ok": st.OK})
	} else {
		a.out.human("%s", st.Message)
		if st.Code != "" {
			a.out.human("编号 %s", st.Code)
		}
	}
	if !st.OK {
		return exitOpFailed
	}
	return exitOK
}

// cmdInstall implements `acornfox install <app|--recipe id> …` and
// `acornfox install status <task>`.
func (a *app) cmdInstall(ctx context.Context, args []string) int {
	if len(args) > 0 && args[0] == "status" {
		return a.cmdInstallStatus(ctx, args[1:])
	}
	// -h anywhere (also after flags of the local install, which this
	// FlagSet does not know) prints the usage.
	for _, x := range args {
		if x == "-h" || x == "--help" || x == "-help" {
			fmt.Fprintln(a.out.stdout, installUsage)
			return exitOK
		}
	}
	fs := a.catalogFlags("install", installUsage)
	var sets setFlag
	fs.Var(&sets, "set", "参数=值，可以给多次")
	var (
		recipe     = fs.String("recipe", "", "配方 id")
		from       = fs.String("catalog", "", "public 或私有目录编号")
		name       = fs.String("name", "", "应用名")
		version    = fs.String("version", "", "版本")
		noWait     = fs.Bool("no-wait", false, "只提交")
		timeout    = fs.Duration("timeout", 30*time.Minute, "最多等多久")
		show       = fs.Bool("show-secrets", false, "显示首次登录的密码")
		keepOnFail = fs.Bool("keep-data-on-failure", false, "失败时保留数据卷")
		reuseData  = fs.Bool("reuse-data", false, "接上以前同名应用留下的数据卷")
		planOnly   = fs.Bool("plan", false, "只看安装前的计划，什么都不装")
		expose     = fs.String("expose", "", "装好时的对外方式：local、lan 或 public")
	)
	var uses setFlag
	fs.Var(&uses, "use", "需要名=服务名，可以给多次")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	req := client.InstallRequest{Recipe: *recipe, Catalog: strings.TrimSpace(*from), Name: *name, Version: *version, KeepVolumesOnFailure: *keepOnFail, ReuseData: *reuseData,
		Expose: strings.TrimSpace(*expose)}
	switch req.Expose {
	case "", "local", "lan", "public":
	default:
		return a.out.usageError("--expose 只能是 local（只本机）、lan（本机和局域网）或 public（所有网卡 + 域名）：%s", req.Expose)
	}
	for _, u := range uses {
		k, v, ok := strings.Cut(u, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			return a.out.usageError("--use 要写成 需要名=服务名（配方里写 应用名.需要名=服务名）：%s", u)
		}
		if req.Use == nil {
			req.Use = map[string]string{}
		}
		req.Use[k] = v
	}
	switch {
	case *recipe != "" && len(pos) == 0:
	case *recipe == "" && len(pos) == 1:
		req.App = pos[0]
	case *recipe == "" && len(pos) == 0 && *reuseData && strings.TrimSpace(*name) != "":
		// Bring an app back on its data by its name alone: the server
		// finds which catalog app it was.
	default:
		return a.out.usageError("用法：acornfox install 应用、acornfox install --recipe 配方，或 acornfox install --name 应用名 --reuse-data（-h 看详情）")
	}
	params, platformSets, perr := parseSets(sets)
	if perr != nil {
		return a.out.usageError("%s", perr.Error())
	}
	req.Platform = platformSets

	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()

	req.Params = params
	if code := a.secretParamsFromStdin(ctx, api, ac, &req, sets, *planOnly); code >= 0 {
		return code
	}

	if *planOnly {
		return a.showPlan(ctx, api, req)
	}
	task, err := ac.Install(ctx, req)
	if err != nil {
		hintTaskWord(req.App, err)
		var ce *client.Error
		if errors.As(err, &ce) && ce.TaskID != "" && !a.out.json {
			a.out.humanErr("正在进行的任务：acornfox install status %s", ce.TaskID)
		}
		return a.out.fail(err)
	}
	if *noWait {
		if a.out.json {
			a.out.emitJSON(map[string]any{"task_id": task.ID, "status": task.Status, "done": false,
				"next": installtiming.NextRunning, "next_command": installtiming.WaitCommand(task.ID)})
		} else {
			a.out.human("已提交安装任务 %s。等它装完：acornfox install status %s --wait", task.ID, task.ID)
		}
		return exitOK
	}
	if !a.out.json {
		a.out.human("安装任务 %s 已开始（%s）", task.ID, task.Target)
	}
	return a.waitInstall(ctx, ac, task.ID, *timeout, *show)
}

// taskWords are what someone after the list of install tasks may type
// after install; install takes them for an app's name (and the server
// answers CAT-INST-01), there is no such list: a task is looked at by its
// number.
var taskWords = map[string]bool{"list": true, "ls": true, "tasks": true, "task": true, "ps": true, "show": true}

// taskWordHint is put before the hint of that CAT-INST-01.
const taskWordHint = "查安装任务用 acornfox install status 任务号（任务号是提交安装时给的 task_id）"

// hintTaskWord adds taskWordHint to err when app is one of taskWords and the
// server did not find such an app (CAT-INST-01).
func hintTaskWord(app string, err error) {
	var ce *client.Error
	if !taskWords[app] || !errors.As(err, &ce) || ce.Diag.Code != "CAT-INST-01" {
		return
	}
	if ce.Diag.Hint == "" {
		ce.Diag.Hint = taskWordHint
	} else {
		ce.Diag.Hint = taskWordHint + "；" + ce.Diag.Hint
	}
}

// planTaskWord is hintTaskWord for a plan: the server answers a plan of an
// app not in the catalog with 200 and CAT-INST-01 among its problems, not as
// an error. When app is one of taskWords, taskWordHint goes before the hint
// of that problem, in p.Problems and in p.Raw (what --json prints).
func planTaskWord(app string, p *client.InstallPlan) {
	if !taskWords[app] {
		return
	}
	join := func(h string) string {
		if h == "" {
			return taskWordHint
		}
		return taskWordHint + "；" + h
	}
	found := false
	for i := range p.Problems {
		if p.Problems[i].Code == "CAT-INST-01" {
			p.Problems[i].Hint = join(p.Problems[i].Hint)
			found = true
		}
	}
	if !found {
		return
	}
	var m map[string]any
	if json.Unmarshal(p.Raw, &m) != nil || m == nil {
		return
	}
	probs, _ := m["problems"].([]any)
	for _, x := range probs {
		if pr, ok := x.(map[string]any); ok && pr["code"] == "CAT-INST-01" {
			h, _ := pr["hint"].(string)
			pr["hint"] = join(h)
		}
	}
	if raw, err := json.Marshal(m); err == nil {
		p.Raw = raw
	}
}

// needStatusText says what became of one need, in the user's words.
var needStatusText = map[string]string{
	"matched":          "已接上",
	"optional_missing": "可选的，这台机器上没有，先跳过",
	"required_missing": "必须的，这台机器上没有",
	"choose":           "有好几条能用，要选一条",
}

// showPlan prints the plan of req (POST /v1/catalog/plan): nothing is
// installed. Exit 0 whatever the plan says: can_install tells.
func (a *app) showPlan(ctx context.Context, api client.API, req client.InstallRequest) int {
	pa, ok := api.(client.PlanAPI)
	if !ok {
		return a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持安装前的计划", Hint: "升级 AcornFox"}})
	}
	p, err := pa.InstallPlan(ctx, req)
	if err != nil {
		hintTaskWord(req.App, err)
		return a.out.fail(err)
	}
	planTaskWord(req.App, &p)
	if a.out.json {
		a.emitRaw(p.Raw, map[string]any{"ok": true})
		return exitOK
	}
	a.out.human("%s", p.Say)
	if len(p.Steps) > 0 {
		a.out.human("安装顺序：")
		for i, s := range p.Steps {
			line := fmt.Sprintf("  %d. %s", i+1, s.Name)
			if s.Action == "reuse" {
				line += "（已装，直接用）"
			}
			if s.Because != "" {
				line += "：" + s.Because
			}
			a.out.human("%s", line)
		}
	}
	for _, ap := range p.Apps {
		if len(ap.Needs) == 0 && len(ap.ParamsMissing) == 0 {
			continue
		}
		a.out.human("%s 需要：", ap.Name)
		for _, n := range ap.Needs {
			text := needStatusText[n.Status]
			if text == "" {
				text = n.Status
			}
			line := fmt.Sprintf("  %s（%s）：%s", n.KindLabel, n.Use, text)
			switch {
			case n.Service != "" && n.PendingFrom != "":
				line += "，用服务 " + n.Service + "（" + n.PendingFrom + " 装好后登记）"
			case n.Service != "":
				line += "，用服务 " + n.Service
			case len(n.Candidates) > 0:
				line += "：" + strings.Join(n.Candidates, "、")
			}
			if n.Status == "optional_missing" && n.Without != "" {
				line += "（" + n.Without + "）"
			}
			a.out.human("%s", line)
			for _, s := range n.Tell {
				a.out.human("    提供它的应用要你做的：%s", s)
			}
			stdin := false
			for _, h := range n.How {
				a.out.human("    %s", h)
				stdin = stdin || strings.Contains(h, "=- ") || strings.HasSuffix(h, "=-")
			}
			if stdin {
				a.out.human("    （写成 项=- 的是保密项：命令从标准输入读它的值，一项一行，按出现的顺序；不要把值写在命令行里）")
			}
		}
		for _, m := range ap.ParamsMissing {
			a.out.human("  还没填的必填参数：%s（%s），用 --set %s=值", m.Key, m.Label, m.Key)
		}
	}
	for _, pr := range p.Problems {
		a.out.human("装不了：%s %s", pr.Code, pr.Message)
		if pr.Hint != "" {
			a.out.human("  %s", pr.Hint)
		}
	}
	if p.CanInstall {
		a.out.human("这只是计划，什么都没装。")
	} else {
		a.out.human("这只是计划，什么都没装；按上面的原因处理后再装。")
	}
	return exitOK
}

// secretParamsFromStdin reads the value of every --set 参数=- whose
// parameter is secret from standard input, one line each in the order given
// (as `services add` does): a password is not typed on the command line, and
// "-" is never installed as one. An empty line is refused (it is not taken
// as "not given", which would have the platform generate one). A secret
// parameter given its value on the command line gets a reminder that it
// stays in the shell's history, and is kept out of the output. Whether a parameter is secret comes from the
// catalog's detail of its app (for a recipe, of the app the prefix names, or
// of every app of the recipe for a bare key). With plan nothing is read: the
// parameter counts as not filled in yet. A value that cannot be looked up is
// sent as it is (the server refuses "-" for a secret parameter). code >= 0
// when it failed (printed).
func (a *app) secretParamsFromStdin(ctx context.Context, api client.API, ac client.AppCenterAPI, req *client.InstallRequest, sets []string, plan bool) int {
	var dash []string              // keys given as "-", in the order given
	literal := map[string]string{} // keys given a value on the command line
	for _, s := range sets {
		k, v, _ := strings.Cut(s, "=")
		k = strings.TrimSpace(k)
		if strings.HasPrefix(k, "platform.") {
			continue
		}
		if v == "-" {
			if !slices.Contains(dash, k) {
				dash = append(dash, k)
			}
		} else if v != "" {
			literal[k] = v
		}
	}
	if len(dash) == 0 && len(literal) == 0 {
		return -1
	}
	apps := []string{req.App}
	if req.Recipe != "" {
		apps = nil
		if list, err := ac.Catalog(ctx); err == nil {
			for _, r := range list.Recipes {
				if r.ID != req.Recipe {
					continue
				}
				for _, x := range r.Apps {
					if !slices.Contains(apps, x.App) {
						apps = append(apps, x.App)
					}
				}
			}
		}
	}
	types := map[string]map[string]string{} // app id → parameter key → type
	paramType := func(id, key string) string {
		t, ok := types[id]
		if !ok {
			t = map[string]string{}
			if d, err := a.catalogApp(ctx, api, ac, id, req.Catalog); err == nil {
				for _, p := range d.Params {
					t[p.Key] = p.Type
				}
			}
			types[id] = t
		}
		return t[key]
	}
	isSecret := func(k string) bool {
		if id, key, prefixed := strings.Cut(k, "."); prefixed && req.Recipe != "" {
			return paramType(id, key) == "secret"
		}
		for _, id := range apps {
			if paramType(id, k) == "secret" {
				return true
			}
		}
		return false
	}
	// A secret written on the command line works, but stays in the shell's
	// history: say so (as the services commands do) and keep it out of the
	// output.
	keys := make([]string, 0, len(literal))
	for k := range literal {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if isSecret(k) {
			a.out.addSecret(literal[k])
			a.out.humanErr("提示：%s 是保密参数，写在命令行里会留在命令历史里；下次写成 --set %s=-，从标准输入给值", k, k)
		}
	}
	var in *stdinLines
	for _, k := range dash {
		if !isSecret(k) {
			continue
		}
		if plan {
			delete(req.Params, k)
			continue
		}
		if in == nil {
			in = a.stdinLines()
		}
		got, err := in.read(k)
		var empty *emptyStdinError
		if errors.As(err, &empty) {
			return a.out.usageError("%s：要填就在标准输入里给 %s 的值；要平台替你生成（能生成的参数才行），就去掉 --set %s=-", err.Error(), k, k)
		}
		if err != nil {
			return a.out.usageError("%s（保密参数写成 --set %s=- 时，值从标准输入给）", err.Error(), k)
		}
		a.out.addSecret(got)
		req.Params[k] = got
	}
	return -1
}

// parseSets turns ["demo-app.admin_username=root", "a=b",
// "platform.tz=Asia/Tokyo"] into the parameters and the platform variable
// changes. A parameter key has no dot (the format allows [a-z][a-z0-9_]*),
// so "x.y=v" is parameter y of app x of a recipe and stays the key "x.y";
// "platform.<name>=v" goes to the second map (the name may contain dots).
func parseSets(sets []string) (map[string]any, map[string]string, error) {
	params := map[string]any{}
	var platformSets map[string]string
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, nil, fmt.Errorf("--set 要写成 参数=值：%s", s)
		}
		if name, isPlatform := strings.CutPrefix(k, "platform."); isPlatform {
			if name == "" {
				return nil, nil, fmt.Errorf("--set platform. 后面要写变量名：%s", s)
			}
			if platformSets == nil {
				platformSets = map[string]string{}
			}
			platformSets[name] = v
			continue
		}
		parts := strings.Split(k, ".")
		if len(parts) > 2 || slices.Contains(parts, "") {
			return nil, nil, fmt.Errorf("--set 的参数名要写成 参数 或 应用名.参数（配方里）：%s", k)
		}
		params[k] = v
	}
	return params, platformSets, nil
}

// waitInstall polls the task until it ends, printing step changes.
func (a *app) waitInstall(ctx context.Context, ac client.AppCenterAPI, id string, timeout time.Duration, show bool) int {
	deadline := time.Now().Add(timeout)
	seen := map[string]string{}
	said := map[string]time.Time{} // when a running step's changing message was last printed
	saidMsg := map[string]string{}
	for {
		t, err := ac.InstallTask(ctx, id)
		if err != nil {
			return a.out.fail(err)
		}
		if !a.out.json {
			for _, s := range t.Steps {
				key := s.App + "/" + s.Name
				if s.Status == "pending" {
					continue
				}
				if seen[key] == s.Status {
					// A running step that reports progress (the image download)
					// changes its message every second: print it at most every
					// progressEvery so the output stays readable.
					if s.Status == "running" && s.Message != "" && s.Message != saidMsg[key] && time.Since(said[key]) >= progressEvery {
						said[key], saidMsg[key] = time.Now(), s.Message
						a.out.human("  [%s] %s %s：%s", s.App, s.Label, stepText(s.Status), s.Message)
					}
					continue
				}
				seen[key], said[key], saidMsg[key] = s.Status, time.Now(), s.Message
				msg := ""
				if s.Message != "" {
					msg = "：" + s.Message
				}
				a.out.human("  [%s] %s %s%s", s.App, s.Label, stepText(s.Status), msg)
			}
		}
		if t.Status != "running" {
			return a.reportTask(ctx, ac, t, show, nil)
		}
		if time.Now().After(deadline) {
			if a.out.json {
				return a.reportTask(ctx, ac, t, show, map[string]any{"waiting": "timeout"})
			}
			a.out.human("等了 %s 还没装完，安装仍在进行。接着等：acornfox install status %s --wait", timeout, id)
			return exitOK
		}
		select {
		case <-ctx.Done():
			if !a.out.json {
				a.out.humanErr("不再等待，安装仍在服务器上进行：acornfox install status %s", id)
			}
			return exitOpFailed
		case <-time.After(2 * time.Second):
		}
	}
}

// progressEvery is how often `install` (waiting) prints a running step's
// progress message.
var progressEvery = 10 * time.Second

func stepText(s string) string {
	switch s {
	case "running":
		return "进行中"
	case "done":
		return "完成"
	case "failed":
		return "失败"
	case "skipped":
		return "跳过"
	case "pending":
		return "还没开始"
	}
	return s
}

// reportTask prints a finished (or running) task with its first-login info
// and the verdict on its step times (the server's, or worked out here when
// an older server sent none); more goes into the --json output as well.
func (a *app) reportTask(ctx context.Context, ac client.AppCenterAPI, t client.InstallTask, show bool, more map[string]any) int {
	vd := verdictOf(t)
	logins := t.FirstLogin
	if show {
		logins = nil
		for _, fl := range t.FirstLogin {
			if full, err := ac.FirstLogin(ctx, fl.App, true); err == nil {
				logins = append(logins, full)
			} else {
				logins = append(logins, fl)
			}
		}
	}
	if a.out.json {
		extra := map[string]any{"ok": t.Status != "failed", "task_id": t.ID,
			"done": vd.Done, "slow": vd.Slow, "stuck": vd.Stuck, "elapsed_seconds": vd.ElapsedSeconds,
			"say": vd.Say, "next": vd.Next, "must_tell": vd.MustTell}
		if vd.MustTell == nil {
			extra["must_tell"] = []string{}
		}
		if vd.NextCommand != "" {
			extra["next_command"] = vd.NextCommand
		}
		if vd.ShowSecretsCommand != "" {
			extra["show_secrets_command"] = vd.ShowSecretsCommand
		}
		if t.Timing == nil {
			extra["timing"] = vd
		}
		for k, v := range more {
			extra[k] = v
		}
		if show {
			extra["first_login"] = logins
		}
		if t.Fault != nil {
			diag := map[string]any{"stage": "install", "code": t.Fault.Code, "message": t.Fault.Message, "hint": t.Fault.Hint}
			if t.Fault.NeedsPlatform != "" {
				diag["needs_platform"] = t.Fault.NeedsPlatform
			}
			extra["diagnosis"] = diag
		}
		a.emitRaw(t.Raw, extra)
	} else {
		switch t.Status {
		case "running":
			a.out.human("任务 %s 进行中（%s）", t.ID, t.Step)
			a.out.human("%s", vd.Say)
			if vd.Stuck {
				a.out.human("这一步比平常慢很多（%s），还没失败：先运行 %s 看原因（只读）；完整办法：acornfox faults %s", installtiming.CodeStuck, vd.NextCommand, installtiming.CodeStuck)
			}
			a.out.human("接着等：acornfox install status %s --wait", t.ID)
		case "succeeded":
			a.out.human("已装好。")
			for _, x := range t.Apps {
				note, where := "", x.URL
				if x.Reused {
					note = "（已装，直接用）"
				}
				if x.Internal {
					where = "只在内部网络，给别的应用用（没有网页可以打开）"
				}
				a.out.human("  %s %s %s%s", x.App, x.Version, where, note)
			}
			for _, x := range t.Apps {
				if !x.Reused && (x.OpenHint != nil || strings.HasPrefix(x.URL, "http://127.0.0.1:")) {
					a.out.human("怎么打开：%s %s。", x.App, localOnlyNote(x.App, x.OpenHint))
				}
			}
			for _, x := range t.Apps {
				if len(x.Carried) > 0 {
					if len(x.ReusedVolumes) > 0 {
						a.out.human("%s 接上了旧数据，沿用的设置：", x.App)
					} else {
						a.out.human("%s 另外：", x.App)
					}
					for _, c := range x.Carried {
						a.out.human("  - %s", c)
					}
				}
			}
			for _, fl := range logins {
				a.out.human("首次登录 %s：", fl.App)
				if fl.URL != "" {
					if fl.URLNote != "" {
						a.out.human("  地址：%s（%s）", fl.URL, fl.URLNote)
					} else {
						a.out.human("  地址：%s", fl.URL)
					}
				}
				for _, f := range fl.Fields {
					a.out.human("  %s：%s", f.Label, f.Value)
				}
			}
			if !show && loginSecrets(logins) {
				a.out.human("密码已遮住。要看密码，在服务器上运行：")
				a.out.human("  %s", installtiming.ShowSecretsCommand(t.ID))
			}
			if len(t.UserSteps) > 0 {
				a.out.human("还要你做的：")
				for i, s := range t.UserSteps {
					a.out.human("  %d. %s", i+1, s)
				}
			}
			if tell := tellLines(t); len(tell) > 0 {
				a.out.human("接上的服务那边还要你做的：")
				for _, s := range tell {
					a.out.human("  - %s", s)
				}
			}
		case "failed":
			if t.Fault != nil {
				a.out.humanErr("安装失败：%s %s", t.Fault.Code, t.Fault.Message)
				if t.Fault.NeedsPlatform != "" {
					a.out.humanErr("要先升级到 %s：这台 AcornFox 版本太旧，升级 AcornFox 后再试", t.Fault.NeedsPlatform)
				}
				if t.Fault.Hint != "" {
					a.out.humanErr("建议：%s", t.Fault.Hint)
				}
			}
			var kept []string
			if t.KeepVolumes {
				for _, x := range t.Apps {
					if x.Created {
						kept = append(kept, x.Volumes...)
					}
				}
			}
			if len(kept) > 0 {
				a.out.humanErr("这次建出来的应用已清理，数据卷 %s 按要求保留供排查；查完后由用户确认，再用 docker volume rm 删掉。把以上内容交给 AI 处理。", strings.Join(kept, "、"))
			} else {
				a.out.humanErr("这次建出来的应用和数据卷已清理。把以上内容交给 AI 处理。")
			}
		}
	}
	if t.Status == "failed" {
		return exitOpFailed
	}
	return exitOK
}

// tellLines are what the providers of the services the task's apps were
// plugged into wrote for the user (apps[].tell), each once.
func tellLines(t client.InstallTask) []string {
	var out []string
	for _, x := range t.Apps {
		for _, s := range x.Tell {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// loginSecrets reports whether some first-login field is a secret: only
// then is there a password to look up with --show-secrets.
func loginSecrets(logins []client.FirstLogin) bool {
	for _, fl := range logins {
		for _, f := range fl.Fields {
			if f.Secret {
				return true
			}
		}
	}
	return false
}

// verdictOf returns the server's verdict on t's step times, or works it out
// by this computer's clock when the server sent none (older than v0.5.1).
func verdictOf(t client.InstallTask) installtiming.Verdict {
	if t.Timing != nil {
		return *t.Timing
	}
	it := installtiming.Task{ID: t.ID, Status: t.Status, CreatedAt: t.CreatedAt, FinishedAt: t.FinishedAt, KeepVolumes: t.KeepVolumes}
	if t.Fault != nil {
		it.FaultCode, it.FaultMsg = t.Fault.Code, t.Fault.Message
	}
	for _, s := range t.Steps {
		it.Steps = append(it.Steps, installtiming.Step{App: s.App, Name: s.Name, Label: s.Label, Status: s.Status, Message: s.Message,
			StartedAt: s.StartedAt, FinishedAt: s.FinishedAt, Progress: s.Progress})
	}
	for _, x := range t.Apps {
		it.Apps = append(it.Apps, installtiming.App{Name: x.App, Carried: x.Carried, Tell: x.Tell})
	}
	it.HasSecrets = loginSecrets(t.FirstLogin)
	return installtiming.Assess(it, waitNow(), "client")
}

// How `install status --wait` waits: it asks for the task every installPoll,
// by default for statusWaitDefault, at most statusWaitMax. 90 seconds keeps
// one command inside the two minutes AI workbenches commonly allow a single
// command (with room for an SSH hop); a longer install is waited on by
// running the command again.
const (
	statusWaitDefault = 90 * time.Second
	statusWaitMax     = 600 * time.Second
)

// installPoll, waitNow and waitSleep are what the waiting goes by (tests
// replace them with a virtual clock).
var (
	installPoll = 2 * time.Second
	waitNow     = time.Now
	waitSleep   = func(ctx context.Context, d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
)

// waitFlag is --timeout of install status: whole seconds ("90") or a Go
// duration ("90s", "2m"); set says it was given.
type waitFlag struct {
	d   time.Duration
	set bool
}

func (w *waitFlag) String() string { return w.d.String() }
func (w *waitFlag) Set(v string) error {
	v = strings.TrimSpace(v)
	d, err := time.ParseDuration(v)
	if err != nil {
		n, nerr := strconv.Atoi(v)
		if nerr != nil {
			return fmt.Errorf("要写秒数，例如 90（或 90s、2m）：%s", v)
		}
		d = time.Duration(n) * time.Second
	}
	if d <= 0 {
		return fmt.Errorf("要大于 0：%s", v)
	}
	w.d, w.set = d, true
	return nil
}

// cmdInstallStatus implements `install status <task> [--wait] [--timeout s]`:
// one look at the task, or (--wait) asking every installPoll until it is
// over or the time is up. Either way the output carries the verdict
// (done, stuck, say, next_command …) so the reader need not judge.
func (a *app) cmdInstallStatus(ctx context.Context, args []string) int {
	fs := a.catalogFlags("install status", installUsage)
	show := fs.Bool("show-secrets", false, "显示首次登录的密码")
	wait := fs.Bool("wait", false, "等到装好或失败才返回")
	var limit waitFlag
	fs.Var(&limit, "timeout", "--wait 最多等多久，秒（默认 90，最多 600）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 {
		return a.out.usageError("用法：acornfox install status 任务编号 [--wait] [--timeout 秒]")
	}
	waiting := *wait || limit.set
	budget := statusWaitDefault
	if limit.set {
		budget = min(limit.d, statusWaitMax)
	}
	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	start := waitNow()
	deadline := start.Add(budget)
	interrupted := false
	var t client.InstallTask
	for {
		t, err = ac.InstallTask(ctx, pos[0])
		if err != nil {
			return a.out.fail(err)
		}
		if !waiting || t.Status != "running" || !waitNow().Before(deadline) {
			break
		}
		if err := waitSleep(ctx, min(installPoll, deadline.Sub(waitNow()))); err != nil {
			interrupted = true
			break
		}
	}
	if !a.out.json {
		for _, s := range t.Steps {
			msg := ""
			if s.Message != "" {
				msg = "：" + s.Message
			}
			a.out.human("  [%s] %s %s%s", s.App, s.Label, stepText(s.Status), msg)
		}
	}
	var more map[string]any
	if waiting {
		more = map[string]any{"waited_seconds": int(waitNow().Sub(start) / time.Second), "timed_out": t.Status == "running",
			"wait_timeout_seconds": int(budget / time.Second)}
		if interrupted {
			more["interrupted"] = true
		}
	}
	rc := a.reportTask(ctx, ac, t, *show, more)
	if interrupted {
		if !a.out.json {
			a.out.humanErr("不再等待，安装仍在服务器上进行：acornfox install status %s --wait", t.ID)
		}
		return exitOpFailed
	}
	return rc
}
