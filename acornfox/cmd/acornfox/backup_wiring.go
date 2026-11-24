package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/reconcile"
	"github.com/acornfox/acornfox/internal/state"
)

// backupKeepEnv sets how many manual backups are kept per app (default 5).
// The upgrade's snapshot (1) and the restore's own backup (1) are fixed.
const backupKeepEnv = "ACORNFOX_BACKUP_KEEP"

// newBackups wires the app data backups (package appbackup): files in
// <data>/app-backups, serialized with the reconciler's rounds of each app, the
// reconciler kicked when an app is started again. The space it checks is the
// partition of the backup directory and, for a restore, the one Docker keeps
// volumes on (as the runner reports it); neither is known off Linux, where
// the check is then skipped.
// AppBackupsDirName is where the app data backups live under the data
// directory. Not "backups": upgrade.sh keeps its own pre-upgrade copies of the
// program and database there as root and prunes all but the newest 5 entries,
// which would make the directory unwritable for the server and delete app
// backups.
const AppBackupsDirName = "app-backups"

func newBackups(st *state.Store, dataDir string, runnerAPI serverRunner, rec *reconcile.Reconciler) *appbackup.Manager {
	dir := filepath.Join(dataDir, AppBackupsDirName)
	keep := 0
	if v, err := strconv.Atoi(os.Getenv(backupKeepEnv)); err == nil && v >= 1 && v <= 50 {
		keep = v
	}
	return appbackup.New(appbackup.Config{
		Dir:        dir,
		Store:      st,
		Runner:     runnerAPI,
		Version:    cli.Version,
		WithApp:    rec.WithApp,
		Kick:       rec.Kick,
		KeepManual: keep,
		Free: func(ctx context.Context) (appbackup.Space, error) {
			free, err := hostcheck.FreeBytes(hostcheck.ExistingDir(dir))
			if err != nil {
				return appbackup.Space{}, err
			}
			sp := appbackup.Space{BackupFree: free}
			root := hostcheck.DefaultDockerRoot
			if ping, err := runnerAPI.Ping(ctx); err == nil && ping.DockerRootDir != "" {
				root = ping.DockerRootDir
			}
			if d, err := hostcheck.FreeBytes(hostcheck.ExistingDir(root)); err == nil {
				sp.DockerFree = d
			}
			return sp, nil
		},
	})
}
