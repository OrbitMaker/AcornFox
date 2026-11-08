package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// App center commands: browse the catalog on the server and install from it.
// Fault codes (CAT-INST-*) and what to do about them are defined in
// internal/appinstall/faults.go; the server sends the hint with each error.

const catalogBrowseUsage = `应用中心（在服务器上看目录、装应用）：
  catalog list           目录里的应用和配方、目录序号、上次同步结果。只读
  catalog info 应用      应用详情和安装时能填的参数（不含平台生成的密码）。只读
  catalog sync           让服务器现在同步一次目录
  catalog platform-vars  平台变量：平台交给应用的模型地址、时区、下载源、访问地址等，这台机器上的值、哪些能改。只读
从目录安装：acornfox install 应用 / acornfox install --recipe 配方（acornfox install -h 看详情）`

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

const catalogPlatformVarsUsage = `用法：acornfox catalog platform-vars

用途：列出平台变量。应用的清单不写死时区、下载源、默认模型这类底层的东西，而是引用平台变量，
  装的时候由平台按这台机器填进去，用户不用配。每项写：这台机器上的值、单机版和云版各从哪取、能不能改。
  能改的，装应用时用 --set platform.名字=值 改（只对这次装的应用生效）。
副作用：无，只读。
例子：
  acornfox catalog platform-vars
  acornfox --json catalog platform-vars
  acornfox install new-api --set platform.tz=Asia/Tokyo`

const installUsage = `用法：acornfox install 应用 [--name 应用名] [--version 版本] [--set 参数=值]...
                         [--llm own|local|platform] [--no-wait] [--timeout 30m] [--show-secrets] [--keep-data-on-failure] [--reuse-data]
      acornfox install --recipe 配方 [--set 应用.参数=值]... [--llm …] [--no-wait] …
      acornfox install status 任务编号 [--show-secrets]

用途：从应用中心的目录在服务器上一次装好一个应用（或一个配方里的几个应用并连起来）：建应用、写配置、
  下载镜像、启动、健康检查、初始化。装成一个安装任务，可以查进度；任一步失败会自动清理这次建出来的东西。
副作用：会改数据（建应用、数据卷、镜像、容器）。默认只本机能打开（exposure local）。
  选 --llm local 时会在本机 New API 里建一把令牌给这个应用用。
参数：
  --name 名字          应用名，默认用目录里的 id
  --version 版本       只能是目录里的版本（acornfox catalog info 看）
  --set 参数=值        可以给多次；大模型参数写 --set llm.base_url=… --set llm.api_key=… --set llm.model=…；
                       配方里的参数可加应用前缀：--set new-api.admin_username=root；
                       平台变量写 platform. 前缀：--set platform.tz=Asia/Tokyo（acornfox catalog platform-vars 看哪些能改）
  --llm 来源           大模型来源：local（本机 New API，装了就默认用它）、own（自己的 Key，要同时给
                       llm.base_url、llm.api_key、llm.model）、platform（平台 Key，即将开放，现在报 CAT-INST-09）
  --no-wait            只提交，输出任务编号；之后用 acornfox install status 任务编号 查
  --timeout 时长       最多等多久（默认 30m）；等不到不会取消安装
  --show-secrets       显示首次登录的密码（默认遮成 ******）
  --keep-data-on-failure  失败时保留数据卷，方便排查（默认清理）
  --reuse-data         接上以前同名应用留下的数据卷（删应用时保留了数据），不加时这种情况报 CAT-INST-24。
                       接上的旧数据装失败也不会删；账号密码和以前一样（删除时保留的参数会恢复）。要先问用户
输出：--json 时提交后输出 {"ok":true,"task_id":…}（--no-wait）或任务结束后的完整任务。
退出码：0 装好（status：进行中或装好）；1 安装失败；2 参数不对；3 连不上服务器；4 服务器拒绝（带 CAT-INST 编号）。
常见编号：CAT-INST-03 同名应用已存在；04 正在装；05 内存不够；09 平台 Key 即将开放；10 本机没有 New API；
  12 镜像拉不到；15 没能上线；16 健康检查不过；17 初始化失败；24 以前同名应用的数据卷还在（先问用户，要旧数据就加 --reuse-data）；
  27 磁盘不够；28 平台变量改法不对。
  完整列表见 docs/capabilities.md。
例子：
  acornfox install new-api
  acornfox install hermes-agent --llm local
  acornfox install hermes-agent --llm own --set llm.base_url=https://api.example.com/v1 --set llm.api_key=sk-… --set llm.model=deepseek-chat
  acornfox install --recipe hermes-with-new-api
  acornfox --json install new-api --no-wait
  acornfox install status 3f9a0c1b2d4e
装好以后：
  只本机能打开的应用，在你自己的电脑上运行 acornfox open 应用名 打开（经 SSH 隧道，不用放行安全组）。
  用了本机 New API 的应用（--llm local）拿到的是一把不限额、不过期的令牌；要控制花费，在 New API 后台的令牌里给它设上限。
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
			a.out.human("  %-22s %s（%s）%s  %s", r.ID, r.Title, strings.Join(apps, " + "), fromText(r.Source), r.Command)
		}
	}
	a.out.human("")
	a.out.human("应用：")
	for _, x := range v.Apps {
		state := ""
		if len(x.Installed) > 0 {
			state = "  已安装：" + strings.Join(x.Installed, "、")
		} else if !x.Supported {
			state = "  这台机器的 CPU 不支持"
		}
		a.out.human("  %-22s %s %s，内存 %s 起%s%s", x.ID, x.Name, x.Version, mbText(x.MemoryMB.Min), fromText(x.Source), state)
		a.out.human("  %-22s %s", "", x.Summary)
	}
	return exitOK
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
	if len(d.Params) > 0 {
		a.out.human("")
		a.out.human("安装时能填的参数：")
		for _, p := range d.Params {
			note := ""
			switch {
			case p.LLM != nil:
				local := "不可用"
				if p.LLM.Local.Available {
					local = "可用（" + strings.Join(p.LLM.Local.Apps, "、") + "）"
				} else if p.LLM.Local.Message != "" {
					local = p.LLM.Local.Message
				}
				note = fmt.Sprintf("--llm local %s；--llm own 自己的 Key；platform %s", local, p.LLM.Platform.Message)
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
		llm        = fs.String("llm", "", "大模型来源：local、own、platform")
		noWait     = fs.Bool("no-wait", false, "只提交")
		timeout    = fs.Duration("timeout", 30*time.Minute, "最多等多久")
		show       = fs.Bool("show-secrets", false, "显示首次登录的密码")
		keepOnFail = fs.Bool("keep-data-on-failure", false, "失败时保留数据卷")
		reuseData  = fs.Bool("reuse-data", false, "接上以前同名应用留下的数据卷")
	)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	req := client.InstallRequest{Recipe: *recipe, Catalog: strings.TrimSpace(*from), Name: *name, Version: *version, KeepVolumesOnFailure: *keepOnFail, ReuseData: *reuseData}
	switch {
	case *recipe != "" && len(pos) == 0:
	case *recipe == "" && len(pos) == 1:
		req.App = pos[0]
	default:
		return a.out.usageError("用法：acornfox install 应用 或 acornfox install --recipe 配方（-h 看详情）")
	}
	src := ""
	switch *llm {
	case "":
	case "local", "local_gateway":
		src = "local_gateway"
	case "own", "platform":
		src = *llm
	default:
		return a.out.usageError("--llm 只能是 local、own、platform")
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

	if src != "" || hasLLMSet(params) {
		if err := a.applyLLM(ctx, api, ac, req, params, src); err != nil {
			return a.out.fail(err)
		}
	}
	for _, v := range params {
		if m, ok := v.(map[string]any); ok {
			if k, ok := m["api_key"].(string); ok {
				a.out.addSecret(k)
			}
		}
	}
	req.Params = params

	task, err := ac.Install(ctx, req)
	if err != nil {
		var ce *client.Error
		if errors.As(err, &ce) && ce.TaskID != "" && !a.out.json {
			a.out.humanErr("正在进行的任务：acornfox install status %s", ce.TaskID)
		}
		return a.out.fail(err)
	}
	if *noWait {
		if a.out.json {
			a.out.emitJSON(map[string]any{"task_id": task.ID, "status": task.Status})
		} else {
			a.out.human("已提交安装任务 %s。查进度：acornfox install status %s", task.ID, task.ID)
		}
		return exitOK
	}
	if !a.out.json {
		a.out.human("安装任务 %s 已开始（%s）", task.ID, task.Target)
	}
	return a.waitInstall(ctx, ac, task.ID, *timeout, *show)
}

// parseSets turns ["llm.model=x", "new-api.admin_username=root", "a=b"] into
// params: "x.y=v" becomes params["x"]["y"] unless x looks like an app id
// (has a "-"), in which case it stays a prefixed key "app.key" (and
// "app.key.field" nests under it). "platform.<name>=v" goes to the
// second map (platform variables; the name may contain dots).
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
		prefix := ""
		if len(parts) > 1 && strings.Contains(parts[0], "-") {
			prefix, parts = parts[0]+".", parts[1:]
		}
		key := prefix + parts[0]
		switch len(parts) {
		case 1:
			params[key] = v
		case 2:
			m, _ := params[key].(map[string]any)
			if m == nil {
				m = map[string]any{}
			}
			m[parts[1]] = v
			params[key] = m
		default:
			return nil, nil, fmt.Errorf("--set 的参数名太深：%s", k)
		}
	}
	return params, platformSets, nil
}

func hasLLMSet(params map[string]any) bool {
	for _, v := range params {
		if _, ok := v.(map[string]any); ok {
			return true
		}
	}
	return false
}

// applyLLM sets the source of every llm_provider parameter (looked up in
// the catalog) to src, keeping the fields given with --set.
func (a *app) applyLLM(ctx context.Context, api client.API, ac client.AppCenterAPI, req client.InstallRequest, params map[string]any, src string) error {
	ids := []string{req.App}
	prefixed := false
	from := req.Catalog
	if req.Recipe != "" {
		cat, err := ac.Catalog(ctx)
		if err != nil {
			return err
		}
		ids = nil
		for _, r := range cat.Recipes {
			if r.ID == req.Recipe && (req.Catalog == "" || r.Source.ID == "" || r.Source.ID == req.Catalog) {
				for _, x := range r.Apps {
					ids = append(ids, x.App)
				}
				if from == "" {
					from = r.Source.ID
				}
				break
			}
		}
		prefixed = true
	}
	for _, id := range ids {
		d, err := a.catalogApp(ctx, api, ac, id, from)
		if err != nil {
			return err
		}
		for _, p := range d.Params {
			if p.Type != "llm_provider" {
				continue
			}
			for _, key := range []string{p.Key, id + "." + p.Key} {
				m, ok := params[key].(map[string]any)
				if !ok {
					if key != p.Key || src == "" {
						continue
					}
					m = map[string]any{}
				}
				if src != "" {
					m["source"] = src
				}
				if _, has := m["source"]; !has {
					m["source"] = "own"
				}
				if prefixed && key == p.Key {
					delete(params, key)
					params[id+"."+p.Key] = m
				} else {
					params[key] = m
				}
			}
		}
	}
	return nil
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
			return a.reportTask(ctx, ac, t, show)
		}
		if time.Now().After(deadline) {
			if a.out.json {
				a.out.emitJSON(map[string]any{"task_id": id, "status": t.Status, "waiting": "timeout"})
			} else {
				a.out.human("等了 %s 还没装完，安装仍在进行。查进度：acornfox install status %s", timeout, id)
			}
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
	}
	return s
}

// reportTask prints a finished (or running) task with its first-login info.
func (a *app) reportTask(ctx context.Context, ac client.AppCenterAPI, t client.InstallTask, show bool) int {
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
		extra := map[string]any{"ok": t.Status != "failed", "task_id": t.ID}
		if show {
			extra["first_login"] = logins
		}
		if t.Fault != nil {
			extra["diagnosis"] = map[string]any{"stage": "install", "code": t.Fault.Code, "message": t.Fault.Message, "hint": t.Fault.Hint}
		}
		a.emitRaw(t.Raw, extra)
	} else {
		switch t.Status {
		case "running":
			a.out.human("任务 %s 进行中（%s）", t.ID, t.Step)
		case "succeeded":
			a.out.human("已装好。")
			for _, x := range t.Apps {
				note := ""
				if x.Reused {
					note = "（已装，直接用）"
				}
				a.out.human("  %s %s %s%s", x.App, x.Version, x.URL, note)
			}
			for _, x := range t.Apps {
				if !x.Reused && strings.HasPrefix(x.URL, "http://127.0.0.1:") {
					a.out.human("%s 只在服务器本机能打开：在你自己的电脑上运行 acornfox open %s（经 SSH 隧道，不用放行安全组）。", x.App, x.App)
				}
			}
			for _, x := range t.Apps {
				if x.GatewayToken != nil {
					a.out.human("%s", gatewayTokenNote(x.App, x.GatewayToken.Provider))
				}
			}
			for _, fl := range logins {
				a.out.human("首次登录 %s：", fl.App)
				for _, f := range fl.Fields {
					a.out.human("  %s：%s", f.Label, f.Value)
				}
			}
			if !show && len(logins) > 0 {
				a.out.human("密码已遮住；要显示加 --show-secrets（acornfox install status %s --show-secrets）", t.ID)
			}
			if len(t.UserSteps) > 0 {
				a.out.human("还要你做的：")
				for i, s := range t.UserSteps {
					a.out.human("  %d. %s", i+1, s)
				}
			}
		case "failed":
			if t.Fault != nil {
				a.out.humanErr("安装失败：%s %s", t.Fault.Code, t.Fault.Message)
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

// gatewayTokenNote says that app uses a token with no quota from the local
// model gateway provider (New API), and where to cap it.
func gatewayTokenNote(app, provider string) string {
	return app + " 用的是 " + provider + " 里一把不限额的令牌；要控制花费，可在 " + provider + " 后台的令牌里给它设上限。"
}

func (a *app) cmdInstallStatus(ctx context.Context, args []string) int {
	fs := a.catalogFlags("install status", installUsage)
	show := fs.Bool("show-secrets", false, "显示首次登录的密码")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 {
		return a.out.usageError("用法：acornfox install status 任务编号")
	}
	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	t, err := ac.InstallTask(ctx, pos[0])
	if err != nil {
		return a.out.fail(err)
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
	return a.reportTask(ctx, ac, t, *show)
}
