// ops 组规则（4）：extensions / backup / restore / backup-delete
package preflight

import (
	"strconv"

	"phpo/internal/config"
	"phpo/pkg/errs"
)

func itoa(n int) string { return strconv.Itoa(n) }

func (r *run) extensions() {
	c := r.c
	if !contains(r.w.Snap.Installed["php"], c.Version) {
		r.errf("%s: PHP %s", errs.NotInstalled, c.Version)
		return
	}
	for _, e := range c.FinalExts {
		if !config.ValidateExt(e) {
			r.errf("%s: %s", errs.ExtInvalid, e)
			break
		}
	}
}

func (r *run) backup() {
	// 环境为空时仅警告，仍允许生成（最小限制）
	if len(r.w.Snap.Installed["php"]) == 0 && len(r.w.Snap.Sites) == 0 && len(r.w.Snap.Installed["nginx"]) == 0 {
		r.warnf("当前环境为空，备份将生成空归档")
	}
}

func (r *run) restore() {
	if !contains(r.w.Backups, r.c.File) {
		r.errf("%s: %s", errs.BackupMissing, r.c.File)
	}
}

func (r *run) backupDelete() {
	if !contains(r.w.Backups, r.c.File) {
		r.errf("%s: %s", errs.BackupMissing, r.c.File)
	}
}
