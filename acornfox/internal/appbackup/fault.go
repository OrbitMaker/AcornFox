package appbackup

import (
	"net/http"
)

// Fault codes of the backups (the full entries are in internal/faults, group
// BKP; `acornfox faults BKP-xx`). A code, once released, is never renamed.
const (
	CodeDisk           = "BKP-01" // not enough free space
	CodeExport         = "BKP-02" // reading or writing a volume failed
	CodeBusy           = "BKP-03" // the app is busy (another backup or restore, a deployment)
	CodeNotFound       = "BKP-04" // no such backup
	CodeCorrupt        = "BKP-05" // the backup's files do not match its list
	CodeConfirm        = "BKP-06" // restoring or deleting needs the user's yes
	CodeRolledBack     = "BKP-07" // the restore failed and the data is back as before
	CodeRollbackFailed = "BKP-08" // the restore failed and so did going back
	CodeVersionGone    = "BKP-09" // the program of the backup's version is not on this machine
	CodeInterrupted    = "BKP-10" // an operation was cut off (restart); leftovers
	CodeUnsupported    = "BKP-11" // this runner cannot read or write volume data
	CodeNoApp          = "BKP-12" // the app is not installed (a restore needs it)
	CodeNoData         = "BKP-13" // the app has no data to back up
	CodeDirNotWritable = "BKP-14" // the backup directory cannot be written
	CodeServiceRecheck = "BKP-15" // after a restore: accounts or connections that no longer match
	CodeBadRequest     = "BKP-16" // the request itself is wrong (id, kind)
)

// Error is a fault with a code, an HTTP status for the API, and a hint.
type Error struct {
	Code    string
	Status  int
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

var statusOf = map[string]int{
	CodeDisk:           http.StatusInsufficientStorage,
	CodeExport:         http.StatusInternalServerError,
	CodeBusy:           http.StatusConflict,
	CodeNotFound:       http.StatusNotFound,
	CodeCorrupt:        http.StatusUnprocessableEntity,
	CodeConfirm:        http.StatusConflict,
	CodeRolledBack:     http.StatusInternalServerError,
	CodeRollbackFailed: http.StatusInternalServerError,
	CodeVersionGone:    http.StatusConflict,
	CodeInterrupted:    http.StatusInternalServerError,
	CodeUnsupported:    http.StatusNotImplemented,
	CodeNoApp:          http.StatusNotFound,
	CodeNoData:         http.StatusConflict,
	CodeDirNotWritable: http.StatusInternalServerError,
	CodeServiceRecheck: http.StatusOK,
	CodeBadRequest:     http.StatusBadRequest,
}

// Hints are the one-line next step of each code (the same words as the
// catalog entry's Next).
var hints = map[string]string{
	CodeDisk:           "先清理磁盘（删掉不用的旧备份：acornfox backup rm 应用名 备份编号 --yes），或把备份目录所在的磁盘扩一扩，再重试；没有动过应用",
	CodeExport:         "看 acornfox logs 和备份目录的 operations.log；应用已经照原样重新启动，重试一次，还不行就把日志给负责的人",
	CodeBusy:           "等这件事做完再试（acornfox backup task 任务编号 看进度）",
	CodeNotFound:       "acornfox backup list 应用名 看现有的备份和编号",
	CodeCorrupt:        "这份备份不能用来恢复，现在的数据没动；换一份备份，或重新做一份",
	CodeConfirm:        "恢复会用备份里的数据覆盖现在的数据：先告诉用户，用户同意后加 --yes 再运行（先 --plan 看会怎样）",
	CodeRolledBack:     "数据已经退回到恢复之前的样子；看任务里写的失败原因，处理后再试",
	CodeRollbackFailed: "不要再动这个应用；恢复之前的备份还在（编号见任务结果的 pre_restore_id），马上告诉负责的人",
	CodeVersionGone:    "备份时的程序版本已不在本机；要在现在的版本上用这份数据，加 --allow-current-version（先问用户）",
	CodeInterrupted:    "运行 acornfox doctor 看有没有半截的备份；重新做一份备份，残留的会自己清掉",
	CodeUnsupported:    "升级 AcornFox 到最新版（acornfox 的执行器要和主程序同版本）",
	CodeNoApp:          "恢复要先有这个应用：先重新安装一个同名应用（acornfox install），再恢复",
	CodeNoData:         "这个应用没有存数据的目录（数据卷），没有东西可备份；先让应用跑起来、产生数据，或确认它的数据目录登记过（acornfox volume add）",
	CodeDirNotWritable: "检查备份目录的权限和所在磁盘（acornfox doctor 会指出是哪个目录）",
	CodeServiceRecheck: "照提示的应用逐个重新接上服务（acornfox services 看，装应用时会重新发账号）",
	CodeBadRequest:     "按提示改请求后重试",
}

// Hint is the next step of code.
func Hint(code string) string { return hints[code] }

func fault(code, msg string) *Error {
	st := statusOf[code]
	if st == 0 {
		st = http.StatusInternalServerError
	}
	return &Error{Code: code, Status: st, Message: msg, Hint: hints[code]}
}

// Fault is the exported constructor (the API layer needs it for its own
// request checks).
func Fault(code, msg string) *Error { return fault(code, msg) }
