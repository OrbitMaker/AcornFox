package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

// `acornfox services …`: what the server can offer its apps (GET/POST/PATCH/
// DELETE /v1/services…). The server checks everything and masks secrets;
// this file parses the command line, reads secret values from standard
// input when asked to (项=-), keeps every secret it handles out of its own
// output, and prints the answers.

const servicesUsage = `用法：acornfox services 子命令

服务是这台机器上能给应用用的东西：装好的应用提供的（例如装了提供大模型接口的应用，就有了一条大模型接口），
或者你自己已有、登记进来的（例如自建邮箱、别家的大模型 Key）。装应用时平台按“需要的服务”自动接上。

  services kinds                              有哪些服务类型、每种有哪些项（只读）
  services list                               这台机器上的服务：类型、来源、状态、谁在用（只读）
  services show 名字                          一条服务的每一项（保密项只显示后 4 位）（只读）
  services add 类型 --name 名字 --set 项=值…  登记一条自己已有的服务（会改数据）
  services set 名字 项=值…                    改几项（会改数据）
  services test 名字                          照这种服务类型的办法试一下通不通（只读，会去连那个地址）
  services remove 名字 [--yes]                删一条自己登记的服务；不加 --yes 只试跑（会删数据）

每个子命令加 -h 看详情。保密项（密码、Key）不要写在命令行里：写成 --set 项=-，再从标准输入给值。`

const servicesKindsUsage = `用法：acornfox services kinds

用途：列出应用目录里定义的服务类型：每种有哪些项（哪些必填、哪些保密、类型、默认值、可选值）、
      能不能试连。登记服务前先看这里。
副作用：无，只读。
输出（--json）：{"ok":true,"kinds":[{kind, label, summary, fields:[{key, label, type, required, secret, default, options, hint}],
  has_test}]}
退出码：0 正常；2 参数不对；3 连不上服务器；4 服务器拒绝（服务器版本太旧是 server_too_old）。
例子：
  acornfox services kinds
  acornfox --json services kinds`

const servicesListUsage = `用法：acornfox services list

用途：列出这台机器上的服务：名字、类型、来源（自己登记的，或由哪个应用提供）、状态、哪些应用在用，
      被哪个应用独占、还能接几个。
      状态：可用；还差必填项；提供它的应用停着；提供它的应用已删；还不能用（提供它的应用的就绪检查没通过，
      后面跟着它清单里写的原因，照着告诉用户；还能接上，只是用到它的功能要等它好了）。
      被独占的服务别的应用接不了；“还能接 0 个”的也接不了（看计划时会写明被谁占着；用 --use 硬指过去报 CAT-INST-38）。
副作用：只读。会现跑应用提供的服务的就绪检查（只读的步骤，每条最多几秒）。保密项只显示后 4 位，没有显示明文的参数。
输出（--json）：{"ok":true,"services":[服务…]}，每条服务的样子见 acornfox services show -h。
退出码：0 正常；2 参数不对；3 连不上服务器；4 服务器拒绝。
例子：
  acornfox services list
  acornfox --json services list`

const servicesShowUsage = `用法：acornfox services show 名字

用途：看一条服务的每一项、状态、哪些应用在用它、被谁独占、还能接几个。应用提供的服务里，标“要你做一步”的项是要你在那个应用里
      操作一下再填进来的（用 acornfox services set 名字 项=- 填）；“每个使用它的应用单独发”的项不在服务上。
      状态是“还不能用”时，后面是提供它的应用的清单写的原因（ready_say），原样告诉用户；ready_detail 是检查哪一步没过。
副作用：只读。会现跑这条服务的就绪检查（只读的步骤，最多几秒）。保密项只显示后 4 位。
输出（--json）：{"ok":true,"name","kind","kind_label","source":"user|app","app",
  "status":"ok|incomplete|provider_stopped|provider_gone|not_ready","status_label","missing":[项],
  "values":[{key, label, value, secret, set, ask}],"issued_per_consumer":[项],"used_by":[{app, use, gone, dedicated}],
  "dedicated_to":"独占它的应用（没有是空串）","max_consumers":最多接几个（0 是不限）,"slots_left":还能接几个（不限是 null）,
  "ready_say":"还不能用的原因（只在 not_ready 时有）","ready_detail":"检查哪一步没过","created_at","updated_at"}
退出码：0 正常；2 参数不对；3 连不上服务器；4 服务器拒绝（没有这条服务是 not_found）。
例子：
  acornfox services show 我的邮箱`

const servicesAddUsage = `用法：acornfox services add 类型 --name 名字 --set 项=值 [--set 项=值 …]

用途：登记一条你自己已有的服务，登记后装应用时就能接上它，别的应用也能用。
      类型和项看 acornfox services kinds；没写的项用默认值，必填项都要给。
      名字：文字和数字，第二个字起也可以用 - _ .，不能有空格。
      地址不能写 127.0.0.1、localhost、::1：应用在容器里，连不到这台机器的本机地址（CAT-SVC-07）。
      类型后面只跟 --name、--set，不能再跟别的词：多了的（例如复制命令时把后面的话一起带进来）报用法错误、退出码 2，
      报错里写“多了参数：…”（带等号的词只显示等号左边，像 Key 的词遮住）。命令要单独一行运行。
      项=值 前面要有 --set：漏写了也报“多了参数”，并提醒改成 --set 项=值。
保密项：密码、Key 这类不要写在命令行里（会留在命令历史里）。写成 --set 项=-，命令会从标准输入读一行当它的值；
        有几个 项=- 就按出现的顺序读几行。硬写在命令行里也收，但会提醒你，输出里一律遮住。
副作用：会改数据（往这台服务器的服务登记表里加一条）。
输出（--json）：{"ok":true, …登记好的那条服务（样子同 services show）}
退出码：0 登记好了；2 参数不对；3 连不上服务器；4 服务器拒绝：CAT-SVC-01 没有这种类型，CAT-SVC-02 少必填项或值不对，
        CAT-SVC-03 重名，CAT-SVC-07 地址是本机回环地址（acornfox faults 编号 看怎么处理）。
例子：
  acornfox services add smtp --name 我的邮箱 --set host=mail.example.com --set username=me --set password=-
  printf '%s\n' "$KEY" | acornfox services add llm_gateway --name 我的上游 --set base_url=https://api.example.com/v1 --set api_key=-`

const servicesSetUsage = `用法：acornfox services set 名字 项=值 [项=值 …]

用途：改一条服务的几项。只写要改的项；值写空（项=）表示清掉：必填项不能清，有默认值的项清掉后回到默认值。
      应用提供的服务：只能填那个应用要你做一步再填进来的项（services show 里标“要你做一步”的），别的项由应用算出来（CAT-SVC-05）。
      改完后，已经在用它的应用要重新应用设置才生效，输出的说明里会列出是哪些应用。
保密项：写成 项=-，从标准输入读一行（有几个读几行，按出现的顺序）。不要把密码、Key 写在命令行里。
名字后面只跟 项=值：多了不带等号的词（例如复制命令时把后面的话一起带进来）报用法错误、退出码 2，报错里写“多了参数：…”。
副作用：会改数据。
输出（--json）：{"ok":true, …改好的那条服务, "notes":[说明…]}
退出码：0 改好了；2 参数不对；3 连不上服务器；4 服务器拒绝（CAT-SVC-02、CAT-SVC-05、CAT-SVC-07，没有这条服务是 not_found）。
例子：
  acornfox services set 我的邮箱 port=465 tls=tls
  acornfox services set 应用名 model=qwen-plus`

const servicesTestUsage = `用法：acornfox services test 名字

用途：照这种服务类型定义里写的办法试一下通不通（连一下那个地址的端口，或者发一个请求看回答），说清连的是哪、通没通、没通的原因
      （拒绝连接、超时、找不到地址、回答的状态码不对）。应用提供的服务，从这台机器经那个应用映射出来的端口去试；
      试不了的（那个应用停着、这种类型没有试连的办法、还差的项没填）照实说，不算通。
副作用：只读；会从服务器去连那个地址。
输出（--json）：{"ok":true|false,"detail":"…","code":"CAT-SVC-06"（试了没通时）,"target":"连的地址","skipped":true（试不了时）}
  注意这里的 ok 是“通没通”。
退出码：0 通了；1 没通或试不了；2 参数不对；3 连不上服务器；4 服务器拒绝（没有这条服务是 not_found）。
例子：
  acornfox services test 我的邮箱`

const servicesRemoveUsage = `用法：acornfox services remove 名字 [--yes]

用途：删一条你自己登记的服务。不加 --yes 只试跑：说会删哪条、现在能不能删，什么都不删。
      还有应用在用它时删不掉（CAT-SVC-04，会列出是哪些应用）；应用提供的服务不能这样删，删掉那个应用它才会消失（CAT-SVC-05）。
副作用：加 --yes 时会删数据（删掉这条服务和它存的值，不能恢复）。删之前先问用户。
参数：
  --yes   真的删
输出（--json）：试跑 {"ok":true,"dry_run":true,"would_remove":true|false,"blocked":""|"in_use"|"from_app","service":{…},"used_by":[{app,use}]}；
  加 --yes {"ok":true,"removed":"名字"}
退出码：0 试跑完或删好了；2 参数不对；3 连不上服务器；4 服务器拒绝（CAT-SVC-04、CAT-SVC-05，没有这条服务是 not_found）。
例子：
  acornfox services remove 我的邮箱
  acornfox services remove 我的邮箱 --yes`

func (a *app) cmdServices(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.out.stdout, servicesUsage)
		return exitOK
	}
	switch args[0] {
	case "kinds":
		return a.cmdServicesKinds(ctx, args[1:])
	case "list", "ls":
		return a.cmdServicesList(ctx, args[1:])
	case "show":
		return a.cmdServicesShow(ctx, args[1:])
	case "add":
		return a.cmdServicesAdd(ctx, args[1:])
	case "set":
		return a.cmdServicesSet(ctx, args[1:])
	case "test":
		return a.cmdServicesTest(ctx, args[1:])
	case "remove", "rm":
		return a.cmdServicesRemove(ctx, args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(a.out.stdout, servicesUsage)
		return exitOK
	}
	return a.out.usageError("未知的 services 子命令：%s（可用：kinds、list、show、add、set、test、remove）", args[0])
}

// servicesAPI dials the target and checks it can do services; code >= 0
// when it could not (the error is printed).
func (a *app) servicesAPI(ctx context.Context) (client.API, client.ServicesAPI, int) {
	t, _, err := a.resolveTarget()
	if err != nil {
		return nil, nil, a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return nil, nil, a.out.fail(derr)
	}
	sa, ok := api.(client.ServicesAPI)
	if !ok {
		api.Close()
		return nil, nil, a.out.fail(client.ServerTooOld("服务"))
	}
	return api, sa, -1
}

// secretLike is a word that may be a key or a password (long, only letters,
// digits and -_.): extraArgs does not print it.
var secretLike = regexp.MustCompile(`^[A-Za-z0-9_.\-]{12,}$`)

// extraArgs says which words a command got that it does not take: the
// first few, each cut short, so that text copied along with the command
// (the rest of a sentence) shows. It never prints what may be a secret: a
// word with "=" shows only the part before it (项=******), a word that looks
// like a key shows as ******. Words with "=" also get the hint that 项=值
// goes after --set.
func extraArgs(extra []string) string {
	const most, longest = 3, 24
	cut := func(x string) string {
		if r := []rune(x); len(r) > longest {
			return string(r[:longest]) + "…"
		}
		return x
	}
	shown := make([]string, 0, most)
	withEq := false
	for i, x := range extra {
		if strings.Contains(x, "=") {
			withEq = true
		}
		if i >= most {
			continue
		}
		switch {
		case strings.Contains(x, "="):
			// 项=值 written without --set in front: the value may be a key.
			k, _, _ := strings.Cut(x, "=")
			x = cut(k) + "=******"
		case secretLike.MatchString(x):
			// A key typed without --set k= in front would be printed here.
			x = "******（像是保密值，已遮住）"
		default:
			x = cut(x)
		}
		shown = append(shown, x)
	}
	text := strings.Join(shown, " ")
	if len(extra) > most {
		text += fmt.Sprintf(" …（共 %d 个）", len(extra))
	}
	text = "多了参数：" + text + "；命令要单独一行，别把后面的话一起带进来"
	if withEq {
		text += "；项=值 要写成 --set 项=值（保密项写 --set 项=-，再从标准输入给值）"
	}
	return text
}

// oneName parses a subcommand whose only positional argument is a service
// name.
func (a *app) oneName(name, usage string, args []string) (string, int) {
	fs := a.catalogFlags("services "+name, usage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return "", flagExit(err)
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return "", a.out.usageError("用法：acornfox services %s 名字（acornfox services list 看有哪些）", name)
	}
	return strings.TrimSpace(pos[0]), -1
}

func (a *app) cmdServicesKinds(ctx context.Context, args []string) int {
	fs := a.catalogFlags("services kinds", servicesKindsUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("services kinds 不带参数")
	}
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := sa.ServiceKinds(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, nil)
		return exitOK
	}
	if len(v.Kinds) == 0 {
		a.out.human("应用目录里还没有定义任何服务类型（目录太旧时先 acornfox catalog sync）。")
		return exitOK
	}
	for i, k := range v.Kinds {
		if i > 0 {
			a.out.human("")
		}
		a.out.human("%s（%s）：%s", k.Label, k.Kind, k.Summary)
		for _, f := range k.Fields {
			var tags []string
			tags = append(tags, typeText(f.Type, f.Options))
			if f.Required {
				tags = append(tags, "必填")
			}
			if f.Secret {
				tags = append(tags, "保密")
			}
			if f.Default != "" {
				tags = append(tags, "默认 "+f.Default)
			}
			line := fmt.Sprintf("  %s %s：%s", f.Key, f.Label, strings.Join(tags, "，"))
			if f.Hint != "" {
				line += "。" + f.Hint
			}
			a.out.human("%s", line)
		}
		if k.HasTest {
			a.out.human("  能试连：acornfox services test 名字")
		} else {
			a.out.human("  这种类型没有试连的办法")
		}
		a.out.human("  登记一条：acornfox services add %s --name 名字 --set 项=值", k.Kind)
	}
	return exitOK
}

func typeText(t string, options []string) string {
	switch t {
	case "int":
		return "整数"
	case "bool":
		return "true 或 false"
	case "url":
		return "网址"
	case "enum":
		return "从 " + strings.Join(options, "、") + " 里选"
	case "string", "":
		return "文字"
	}
	return t
}

func sourceText(s client.Service) string {
	if s.Source == "app" {
		return "由应用 " + s.App + " 提供"
	}
	return "自己登记的"
}

// statusText is the status with what is missing named.
func statusText(s client.Service) string {
	switch s.Status {
	case "incomplete":
		var labels []string
		for _, k := range s.Missing {
			l := k
			for _, v := range s.Values {
				if v.Key == k && v.Label != "" {
					l = v.Label
				}
			}
			labels = append(labels, l)
		}
		if len(labels) > 0 {
			return "还差：" + strings.Join(labels, "、")
		}
	case "provider_stopped":
		return "提供它的应用 " + s.App + " 停着"
	case "provider_gone":
		return "提供它的应用 " + s.App + " 已删"
	case "not_ready":
		if s.ReadySay != "" {
			return "还不能用：" + s.ReadySay
		}
		return "还不能用"
	}
	if s.StatusLabel != "" {
		return s.StatusLabel
	}
	return s.Status
}

func usedByText(s client.Service) string {
	var parts []string
	for _, u := range s.UsedBy {
		parts = append(parts, u.App+"（"+u.Use+"）")
	}
	return strings.Join(parts, "、")
}

// slotsText says who holds the service for itself and how many more apps
// can be plugged in; "" when anyone can (no holder, no limit).
func slotsText(s client.Service) string {
	switch {
	case s.DedicatedTo != "":
		return "被应用 " + s.DedicatedTo + " 独占，别的应用接不了"
	case s.SlotsLeft != nil && *s.SlotsLeft == 0:
		return fmt.Sprintf("已经接满（最多 %d 个），别的应用接不了", s.MaxConsumers)
	case s.SlotsLeft != nil:
		return fmt.Sprintf("还能接 %d 个（最多 %d 个）", *s.SlotsLeft, s.MaxConsumers)
	}
	return ""
}

func (a *app) cmdServicesList(ctx context.Context, args []string) int {
	fs := a.catalogFlags("services list", servicesListUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("services list 不带参数；看一条用 acornfox services show 名字")
	}
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := sa.Services(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, nil)
		return exitOK
	}
	if len(v.Services) == 0 && len(v.Unreadable) == 0 {
		a.out.human("这台机器上还没有服务。装好的应用提供的服务会自己出现在这里；")
		a.out.human("自己已有的可以登记：acornfox services add 类型 --name 名字 --set 项=值（acornfox services kinds 看有哪些类型）")
		return exitOK
	}
	for _, s := range v.Services {
		line := fmt.Sprintf("%s  %s  %s  %s", s.Name, s.KindLabel, sourceText(s), statusText(s))
		if u := usedByText(s); u != "" {
			line += "  在用它的：" + u
		}
		if t := slotsText(s); t != "" {
			line += "  " + t
		}
		a.out.human("%s", line)
	}
	if len(v.Unreadable) > 0 {
		a.out.human("")
		a.out.human("这几条存的值解不开（数据目录里的钥匙文件换过，或者记录坏了）：%s", strings.Join(v.Unreadable, "、"))
	}
	return exitOK
}

// printService prints one service for humans.
func (a *app) printService(s client.Service) {
	a.out.human("%s：%s，%s", s.Name, s.KindLabel, sourceText(s))
	a.out.human("状态：%s", statusText(s))
	if s.Status == "not_ready" && s.ReadyDetail != "" {
		a.out.human("  就绪检查没过的那一步：%s", s.ReadyDetail)
	}
	for _, v := range s.Values {
		head := v.Key
		if v.Label != "" && v.Label != v.Key {
			head = v.Label + "（" + v.Key + "）"
		}
		switch {
		case v.Set:
			a.out.human("  %s = %s", head, v.Value)
		case v.Ask != "":
			a.out.human("  %s 还没填；要你做一步：%s（填：acornfox services set %s %s=-）", head, v.Ask, s.Name, v.Key)
		default:
			a.out.human("  %s 没填", head)
		}
	}
	if len(s.IssuedPerConsumer) > 0 {
		a.out.human("  每个使用它的应用单独发：%s", strings.Join(s.IssuedPerConsumer, "、"))
	}
	if u := usedByText(s); u != "" {
		a.out.human("在用它的：%s", u)
	} else {
		a.out.human("还没有应用在用它")
	}
	if t := slotsText(s); t != "" {
		a.out.human("能不能再接：%s", t)
	} else {
		a.out.human("能不能再接：能，不限个数")
	}
}

func (a *app) cmdServicesShow(ctx context.Context, args []string) int {
	name, code := a.oneName("show", servicesShowUsage, args)
	if code >= 0 {
		return code
	}
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	s, err := sa.Service(ctx, name)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(s.Raw, nil)
		return exitOK
	}
	a.printService(s)
	return exitOK
}

// setFlags collects repeated --set 项=值 in order.
type setFlags []string

func (s *setFlags) String() string     { return strings.Join(*s, ",") }
func (s *setFlags) Set(v string) error { *s = append(*s, v); return nil }

// stdinLines reads values from standard input, one line each.
type stdinLines struct {
	a   *app
	r   *bufio.Reader
	tty bool
}

func (a *app) stdinLines() *stdinLines {
	l := &stdinLines{a: a}
	if a.stdin != nil {
		l.r = bufio.NewReader(a.stdin)
	}
	if f, ok := a.stdin.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			l.tty = true
		}
	}
	return l
}

func (l *stdinLines) read(key string) (string, error) {
	if l.r == nil {
		return "", fmt.Errorf("%s=- 要从标准输入读值，但没有标准输入", key)
	}
	if l.tty {
		fmt.Fprintf(l.a.out.stderr, "输入 %s 的值，回车结束（输入时屏幕上会显示）：", key)
	}
	line, err := l.r.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("%s=- 要从标准输入读一行，但标准输入里没有了（有几个 项=- 就要给几行）", key)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		// An empty line is never taken as "not given" (a secret would then
		// be generated, or an item cleared, without anyone meaning it).
		return "", &emptyStdinError{key: key}
	}
	return line, nil
}

// emptyStdinError: the line read for 项=- was empty. The command stops
// and says so; what to write instead depends on the command.
type emptyStdinError struct{ key string }

func (e *emptyStdinError) Error() string {
	return fmt.Sprintf("%s=- 从标准输入读到的是空值（一个空行），没有用它", e.key)
}

// parseItems turns 项=值 into values in the order given; 项=- is read from
// standard input. secret reports whether an item is secret; every secret
// value and every value read from standard input is kept out of the output,
// and a secret written on the command line gets a reminder.
// onEmpty is what to write instead when the line read for 项=- is empty
// (the item's name is filled in for %[1]s).
func (a *app) parseItems(items []string, secret func(key string) bool, onEmpty string) (map[string]string, int) {
	out := map[string]string{}
	var in *stdinLines
	for _, it := range items {
		k, v, ok := strings.Cut(it, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, a.out.usageError("要写成 项=值，%q 不是（保密项写 项=-，从标准输入读）", it)
		}
		if _, dup := out[k]; dup {
			return nil, a.out.usageError("%s 写了两次", k)
		}
		if v == "-" {
			if in == nil {
				in = a.stdinLines()
			}
			got, err := in.read(k)
			var empty *emptyStdinError
			if errors.As(err, &empty) {
				return nil, a.out.usageError("%s：%s", err.Error(), fmt.Sprintf(onEmpty, k))
			}
			if err != nil {
				return nil, a.out.usageError("%s", err.Error())
			}
			a.out.addSecret(got)
			out[k] = got
			continue
		}
		if secret(k) && v != "" {
			a.out.addSecret(v)
			a.out.humanErr("提示：%s 是保密项，写在命令行里会留在命令历史里；下次写成 %s=-，从标准输入给值", k, k)
		}
		out[k] = v
	}
	return out, -1
}

func (a *app) cmdServicesAdd(ctx context.Context, args []string) int {
	fs := a.catalogFlags("services add", servicesAddUsage)
	name := fs.String("name", "", "服务的名字")
	var sets setFlags
	fs.Var(&sets, "set", "项=值（可以写多次；保密项写 项=-）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	switch {
	case len(pos) == 0 || strings.TrimSpace(pos[0]) == "":
		return a.out.usageError("少了类型：用法：acornfox services add 类型 --name 名字 --set 项=值（acornfox services kinds 看有哪些类型）")
	case len(pos) > 1:
		return a.out.usageError("%s。用法：acornfox services add 类型 --name 名字 --set 项=值（类型后面只跟 --name、--set）", extraArgs(pos[1:]))
	}
	kind := strings.TrimSpace(pos[0])
	if strings.TrimSpace(*name) == "" {
		return a.out.usageError("要给这条服务起个名字：--name 名字")
	}
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	kinds, err := sa.ServiceKinds(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	secrets := map[string]bool{}
	for _, k := range kinds.Kinds {
		if k.Kind != kind {
			continue
		}
		for _, f := range k.Fields {
			secrets[f.Key] = f.Secret
		}
	}
	values, code := a.parseItems(sets, func(k string) bool { return secrets[k] }, "要登记它就在标准输入里给 %[1]s 的值；这一项不填就去掉 --set %[1]s=-")
	if code >= 0 {
		return code
	}
	s, err := sa.AddService(ctx, client.AddServiceRequest{Name: strings.TrimSpace(*name), Kind: kind, Values: values})
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(s.Raw, nil)
		return exitOK
	}
	a.out.human("已登记。")
	a.printService(s)
	a.out.human("")
	a.out.human("试一下通不通：acornfox services test %s", s.Name)
	return exitOK
}

func (a *app) cmdServicesSet(ctx context.Context, args []string) int {
	fs := a.catalogFlags("services set", servicesSetUsage)
	var sets setFlags
	fs.Var(&sets, "set", "项=值（也可以直接写在名字后面）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) < 1 || strings.TrimSpace(pos[0]) == "" {
		return a.out.usageError("用法：acornfox services set 名字 项=值 [项=值 …]")
	}
	name := strings.TrimSpace(pos[0])
	// Words after the name that are no 项=值: most likely text copied
	// along with the command (the rest of a sentence).
	var extra []string
	for _, p := range pos[1:] {
		if !strings.Contains(p, "=") {
			extra = append(extra, p)
		}
	}
	if len(extra) > 0 {
		return a.out.usageError("%s。用法：acornfox services set 名字 项=值 [项=值 …]（名字后面只跟 项=值）", extraArgs(extra))
	}
	items := append(append([]string{}, pos[1:]...), sets...)
	if len(items) == 0 {
		return a.out.usageError("要改哪一项？写成 acornfox services set %s 项=值（值写空表示清掉；保密项写 项=-）", name)
	}
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	cur, err := sa.Service(ctx, name)
	if err != nil {
		return a.out.fail(err)
	}
	secrets := map[string]bool{}
	for _, v := range cur.Values {
		secrets[v.Key] = v.Secret
	}
	values, code := a.parseItems(items, func(k string) bool {
		s, known := secrets[k]
		return s || !known // an item the service does not list: keep it out of the output too
	}, "要改它就在标准输入里给 %[1]s 的新值；要清掉这一项写成 %[1]s=（等号后面什么都不写）")
	if code >= 0 {
		return code
	}
	s, err := sa.PatchService(ctx, name, values)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(s.Raw, nil)
		return exitOK
	}
	a.out.human("已改好。")
	a.printService(s)
	for _, n := range s.Notes {
		a.out.human("注：%s", n)
	}
	return exitOK
}

func (a *app) cmdServicesTest(ctx context.Context, args []string) int {
	name, code := a.oneName("test", servicesTestUsage, args)
	if code >= 0 {
		return code
	}
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	r, err := sa.TestService(ctx, name)
	if err != nil {
		return a.out.fail(err)
	}
	exit := exitOK
	if !r.OK {
		exit = exitOpFailed
	}
	if a.out.json {
		a.out.encodeRedacted(a.out.stdout, map[string]any{"ok": r.OK, "detail": r.Detail, "code": r.Code, "target": r.Target, "skipped": r.Skipped})
		return exit
	}
	switch {
	case r.OK:
		a.out.human("通了：%s", r.Detail)
	case r.Skipped:
		a.out.human("试不了：%s", r.Detail)
	default:
		a.out.human("没通：%s", r.Detail)
		if line := codeLine(r.Code); line != "" {
			a.out.human("%s", line)
		}
	}
	return exit
}

func (a *app) cmdServicesRemove(ctx context.Context, args []string) int {
	fs := a.catalogFlags("services remove", servicesRemoveUsage)
	yes := fs.Bool("yes", false, "真的删")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return a.out.usageError("用法：acornfox services remove 名字 [--yes]")
	}
	name := strings.TrimSpace(pos[0])
	api, sa, code := a.servicesAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	if *yes {
		notes, err := sa.RemoveService(ctx, name)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			if notes == nil {
				notes = []string{}
			}
			a.out.emitJSON(map[string]any{"removed": name, "notes": notes})
			return exitOK
		}
		a.out.human("已删除服务 %s。", name)
		for _, n := range notes {
			a.out.human("%s", n)
		}
		return exitOK
	}
	s, err := sa.Service(ctx, name)
	if err != nil {
		return a.out.fail(err)
	}
	// Apps deleted with their data kept do not keep it: their bindings go
	// with it.
	var installedUsers, goneUsers []string
	for _, u := range s.UsedBy {
		if u.Gone {
			goneUsers = append(goneUsers, u.App)
		} else {
			installedUsers = append(installedUsers, u.App+"（"+u.Use+"）")
		}
	}
	blocked := ""
	switch {
	case s.Source == "app" && s.Status != "provider_gone":
		blocked = "from_app"
	case len(installedUsers) > 0:
		blocked = "in_use"
	}
	if a.out.json {
		var svc any = map[string]any{}
		if len(s.Raw) > 0 {
			svc = s.Raw
		}
		used := []map[string]string{}
		for _, u := range s.UsedBy {
			used = append(used, map[string]string{"app": u.App, "use": u.Use})
		}
		a.out.emitJSON(map[string]any{"dry_run": true, "would_remove": blocked == "", "blocked": blocked, "service": svc, "used_by": used})
		return exitOK
	}
	a.out.human("试跑，什么都没删。")
	switch blocked {
	case "from_app":
		a.out.human("%s 是应用 %s 提供的服务，不能这样删：删掉那个应用它才会消失（acornfox delete %s，删之前先想清楚）。", s.Name, s.App, s.App)
	case "in_use":
		a.out.human("%s 现在删不掉：还有应用在用它：%s。要删得先删掉这些应用。", s.Name, strings.Join(installedUsers, "、"))
	default:
		a.out.human("会删：%s（%s，%s）。删了不能恢复。", s.Name, s.KindLabel, sourceText(s))
		if len(goneUsers) > 0 {
			a.out.human("以前接它的 %s 已经删了（数据还留着），它们接这条服务的记录会一起删掉，以后装回来时要重新接服务。", strings.Join(goneUsers, "、"))
		}
		a.out.human("确认要删就加 --yes：acornfox services remove %s --yes", s.Name)
	}
	return exitOK
}
