package main

// The app data backups line of `acornfox doctor` (fault codes BKP-01,
// BKP-08, BKP-10 and BKP-14; the full entries are in internal/faults,
// `acornfox faults BKP-14`). It reads GET /v1/backups of acornfox-server:
// whether the backup directory can be written, the free space on its disk,
// half-written backups and a restore that could not go back. Read only.

import (
	"context"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// doctorBackupsQuery asks acornfox-server for the backup directory's state;
// ok is false when it cannot be asked or is older than backups (tests
// replace it).
var doctorBackupsQuery = queryBackups

func queryBackups(ctx context.Context, socket string) (client.BackupOverview, bool) {
	qctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return client.BackupOverview{}, false
	}
	defer c.Close()
	v, err := c.Backups(qctx)
	if err != nil {
		return client.BackupOverview{}, false
	}
	return v, true
}

// doctorBackups adds one line per finding.
func doctorBackups(ctx context.Context, r *doctorReport, socket string) {
	v, ok := doctorBackupsQuery(ctx, socket)
	if !ok {
		return
	}
	for _, f := range v.Findings {
		switch f.Severity {
		case "fail":
			r.add(false, f.Code, "%s", f.Text)
		case "warn":
			r.warn(f.Code, "%s", f.Text)
		default:
			r.add(true, f.Code, "%s", f.Text)
		}
	}
}
