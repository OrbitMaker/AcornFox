package appbackup

// 保留规则（自动另存的两种：升级前 pre-upgrade、恢复前 pre-restore，退回升级用的也是恢复前）：
//   - 年轻于 7 天（Config.AutoKeepDays）的，不因为“份数超了”被清；
//   - 每种最新的 3 份（Config.KeepPreUpgrade、KeepPreRestore）也留着，不管多老；
//   - 其余的（超过 7 天又不在最新 3 份里）按份数规则清；
//   - 磁盘放不下这次要做的备份时才可以提前清 7 天内的：从最老的自动备份清起，不清最新的一份自动备份和正在用来恢复的那份，
//     清了也不够就一份都不清（照旧报磁盘不够）。计划（PlanBackup、PlanRestore）提前写明会清哪几份，实际清掉的在任务结果的 pruned 里。
//   - 手动备份（manual）照旧：只留最新的 KeepManual 份。
// 为什么：“升级→退回→再从别的备份恢复”时，退回前另存的（升级后的新数据）不能被第二次恢复前的另存挤掉。

import (
	"context"
	"os"
	"sort"
	"time"
)

// isAuto reports whether kind is one of the automatic kinds.
func isAuto(kind string) bool { return kind == KindPreUpgrade || kind == KindPreRestore }

func (m *Manager) autoKeepFor() time.Duration {
	return time.Duration(m.cfg.AutoKeepDays) * 24 * time.Hour
}

// protected: an automatic backup younger than AutoKeepDays.
func (m *Manager) protected(b Meta) bool {
	return isAuto(b.Kind) && m.now().Sub(b.CreatedAt) < m.autoKeepFor()
}

// pruneIDs is what the count rule removes among all (newest first, as List
// returns them) for backups of kind. incoming says a new backup of that kind
// is about to be added (it counts as the newest). keepID is never removed.
func (m *Manager) pruneIDs(all []Meta, kind, keepID string, incoming bool) []string {
	var ids []string
	n := 0
	if incoming {
		n = 1
	}
	for _, b := range all {
		if b.Kind != kind {
			continue
		}
		n++
		if n <= m.Keep(kind) || b.ID == keepID || m.protected(b) {
			continue
		}
		ids = append(ids, b.ID)
	}
	return ids
}

// pickSpace chooses the automatic backups to remove, oldest first, so that
// free+freed covers need. It never picks keepID or the newest automatic
// backup, and picks nothing when removing every candidate would still not be
// enough. all is newest first.
func pickSpace(all []Meta, need int64, free uint64, keepID string) (ids []string, freed int64) {
	if int64(free) >= need {
		return nil, 0
	}
	newestSkipped := false
	var cands []Meta
	for _, b := range all {
		if !isAuto(b.Kind) {
			continue
		}
		if !newestSkipped {
			newestSkipped = true // the newest automatic backup stays
			continue
		}
		if b.ID == keepID {
			continue
		}
		cands = append(cands, b)
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].CreatedAt.Before(cands[j].CreatedAt) })
	for _, b := range cands {
		ids = append(ids, b.ID)
		freed += b.Bytes
		if int64(free)+freed >= need {
			return ids, freed
		}
	}
	return nil, 0
}

// evictForSpace removes what pickSpace chooses when the backup directory's
// free space is below need. It returns the removed ids. Nothing happens when
// the free space is not known or is enough.
func (m *Manager) evictForSpace(ctx context.Context, app string, need int64, keepID string) []string {
	if m.cfg.Free == nil || need <= 0 {
		return nil
	}
	sp, err := m.cfg.Free(ctx)
	if err != nil {
		return nil
	}
	all, err := m.List(app)
	if err != nil {
		return nil
	}
	ids, _ := pickSpace(all, need, sp.BackupFree, keepID)
	var removed []string
	for _, id := range ids {
		if err := os.RemoveAll(m.backupDir(app, id)); err != nil {
			m.cfg.Log.Warn("evict backup", "app", app, "id", id, "err", err)
			continue
		}
		removed = append(removed, id)
		m.log(ctx, app, "磁盘放不下新的备份，提前清掉自动另存的旧备份 "+id)
	}
	return removed
}
