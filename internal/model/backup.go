// 备份领域模型：~/phpo/backups/backup-*.tar.gz
package model

import "time"

type BackupFile struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}
