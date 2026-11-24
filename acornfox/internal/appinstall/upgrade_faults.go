package appinstall

// Fault codes of upgrading an app from the app center to a newer version of
// its catalog entry and of going back to how it was before the upgrade
// (upgrade.go): `acornfox apps outdated|upgrade|rollback`, the console's
// update page. The full entries are in internal/faults (group INST); the
// words here are the same as there, word for word (a test checks it).
const (
	CodeUpgCannot       = "APP-UPG-01" // this app cannot be upgraded now (nothing newer, not from the app center, stopped, still being installed or applied, manifest not saved)
	CodeUpgNeed         = "APP-UPG-02" // the new version needs something from the user first (a setting without a default, more memory); nothing was changed
	CodeUpgChanged      = "APP-UPG-03" // the new version differs in a way this AcornFox does not upgrade in place (a new data directory, another service it provides, a new required service); nothing was changed
	CodeUpgConfirm      = "APP-UPG-04" // the upgrade restarts the app and the request did not say the user agreed (confirm_restart); nothing was changed
	CodeUpgFailed       = "APP-UPG-05" // the upgrade failed and the app was put back as it was before (the cause's code is in the message)
	CodeUpgUndone       = "APP-UPG-06" // the upgrade failed and putting the app back did not fully work either
	CodeRollbackCannot  = "APP-UPG-07" // there is nothing to go back to (no snapshot from before an upgrade, or it is of the version running now, or its files or program are gone)
	CodeRollbackConfirm = "APP-UPG-08" // going back loses what was written after the upgrade and the request did not say the user agreed (confirm); nothing was changed
	CodeRollbackFailed  = "APP-UPG-09" // going back failed and the app stays as it was before the attempt
	CodeRollbackUndone  = "APP-UPG-10" // going back failed and so did coming back: the app may be in a mixed state
)

// What APP-UPG-01 … 10 mean and what to do (the fault catalog says the same,
// word for word).
const (
	upgCannotMeaning       = "这个应用现在不能升级：目录里没有比现在装的更新的版本，它不是从应用中心装的、装的时候没保存清单或用的是旧格式的清单，还没有上线，停着，正在安装、重新生效或升级，或者它来自已断开的私有目录。什么都没改"
	upgCannotAction        = "看 message 是哪一种：没有新版本就是已经最新，不用处理；正在装、重新生效或升级的，acornfox install status 任务编号 --wait 等它结束再来；停着的先问用户，同意后 acornfox start 应用再升级；清单没存或是旧格式的，这个应用只能删了重装（保留数据 --reuse-data，要先问用户）"
	upgNeedMeaning         = "新版本要用户先补一样东西才能升级：多了一项必填、又没有默认值的设置，或者要的内存比现在给这个应用的多。升级前就停下了，什么都没改，也没留快照"
	upgNeedAction          = "按 message 里写的补：缺的设置问到用户给的值，升级时加 --set 设置名=值（密码类写 设置名=-，从标准输入给）；内存不够就问用户，同意后 acornfox app set --app 应用 --memory 数字，再升级。补好以后先 acornfox apps upgrade 应用 --plan 看一遍"
	upgChangedMeaning      = "新版本和现在装的在这几处不一样，这一版 AcornFox 不能原地升级：多了数据目录、提供的服务变了，或者多需要一个必须的服务（平台不替用户去接别的应用的服务）。升级前就停下了，什么都没改，也没留快照"
	upgChangedAction       = "把 message 里写的告诉用户：现在这个应用照常用，只是不能升到这个版本。要用新版本，用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data；多了必须的服务的，先登记一条已有的：acornfox services add …）；不同意就先不升，不要想办法绕过"
	upgConfirmMeaning      = "升级会重启这个应用（先留一份快照，再换新版本，中间有一段时间用不了），请求里没有写明用户同意，所以什么都没做。这是确认规则，不是出错"
	upgConfirmAction       = "把 acornfox apps upgrade 应用 --plan 说的会做什么、要停多久告诉用户，问他同意后在同一条命令上加 --yes（接口里是 confirm_restart: true）再运行一次"
	upgFailedMeaning       = "升级没成功，平台已经自动退回到升级前：数据、设置、程序版本都恢复成升级前留的快照，应用照原来的版本在运行。message 里写了没成功的原因和它的编号（新版本没拉下来、没留成快照、容器没起来、健康检查没通过，或清单里“重新生效后做的步骤”没通过）"
	upgFailedAction        = "把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；原因是磁盘不够、网络不通这类的，处理好了再升级一次；应用已经退回，不用再处理，也不要重复升级"
	upgUndoneMeaning       = "升级没成功，而且退回到升级前时也出了错：应用可能停在新旧混在一起的状态（程序是旧的、数据可能已经被新版本改过，或设置有的新有的旧）。message 里写了两边的原因，升级前的快照还在"
	upgUndoneAction        = "这是要人处理的：先把 message 告诉用户，不要再重复升级，不要删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；用户同意后用升级前的快照恢复：acornfox backup list 应用 找到类型是升级前的那份，再 acornfox backup restore 应用 备份编号 --yes（先 --plan 看会怎样）"
	rollbackCannotMeaning  = "没有可以退回的东西：这个应用还没升级过（没有升级前的快照），或者快照已经被后来的升级覆盖、被删掉，或者快照就是现在这个版本的，或者快照的文件不齐、那个版本的程序已经不在这台机器上了。什么都没改"
	rollbackCannotAction   = "看 message 是哪一种：没升级过或已经退回过，就没有可退的，不用处理；快照坏了或程序没了，告诉用户这条路走不通，别的备份用 acornfox backup list 应用 看有没有能用的"
	rollbackConfirmMeaning = "退回到升级前会用升级前留的快照覆盖现在的数据：升级之后写进应用的新数据会没有（退回前会先把现在的数据另存一份，需要时能找回），应用会重启一次。请求里没有写明用户同意，所以什么都没做。这是确认规则，不是出错"
	rollbackConfirmAction  = "把 acornfox apps rollback 应用 --plan 说的告诉用户，特别是“升级后的新数据会没有”，问他同意后加 --yes（接口里是 confirm: true）再运行一次"
	rollbackFailedMeaning  = "退回没成功，应用已经保持退回前的样子（还是升级后的版本，数据没变）。message 里写了原因和它的编号（磁盘不够、快照有问题、容器没起来……）"
	rollbackFailedAction   = "把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；处理好了可以再退回一次"
	rollbackUndoneMeaning  = "退回没成功，而且回到退回前的样子时也出了错：应用可能停在新旧混在一起的状态。message 里写了两边的原因，退回前另存的那份数据还在"
	rollbackUndoneAction   = "这是要人处理的：先把 message 告诉用户，不要再重复退回，不要删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；acornfox backup list 应用 里有退回前另存的那份（类型是恢复前），问用户同意后用它恢复"
)
