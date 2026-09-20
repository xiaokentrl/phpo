// 离线缓存索引与清单镜像表（manifest.json 原文权威在磁盘，此处为加速视图）
package store

import (
	"time"

	"phpo/internal/model"
)

func (s *Store) UpsertOfflineEntry(e model.CacheEntry) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	li, vi := 0, 0
	if e.HasImage {
		li = 1
	}
	if e.VerifyOK {
		vi = 1
	}
	var lv any
	if !e.LastVerify.IsZero() {
		lv = e.LastVerify.UTC().Format(time.RFC3339Nano)
	}
	_, err = db.Exec(`INSERT INTO offline_entries(kind,version,has_image,total_size,last_verify,verify_ok)
		VALUES(?,?,?,?,?,?) ON CONFLICT(kind,version) DO UPDATE SET
		has_image=excluded.has_image, total_size=excluded.total_size, last_verify=excluded.last_verify, verify_ok=excluded.verify_ok`,
		e.Kind, e.Version, li, e.TotalSize, lv, vi)
	return err
}

func (s *Store) ListOfflineEntries() ([]model.CacheEntry, error) {
	db, err := s.ensure()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT kind,version,has_image,total_size,IFNULL(last_verify,''),verify_ok FROM offline_entries ORDER BY kind,version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CacheEntry
	for rows.Next() {
		var (
			e      model.CacheEntry
			hi, vo int
			lv     string
		)
		if err := rows.Scan(&e.Kind, &e.Version, &hi, &e.TotalSize, &lv, &vo); err != nil {
			return nil, err
		}
		e.HasImage, e.VerifyOK = hi == 1, vo == 1
		if lv != "" {
			e.LastVerify, _ = time.Parse(time.RFC3339Nano, lv)
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) SaveManifest(kind, version, rawJSON string, updatedAt time.Time) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO cache_manifest(kind,version,manifest,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(kind,version) DO UPDATE SET manifest=excluded.manifest, updated_at=excluded.updated_at`,
		kind, version, rawJSON, updatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) LoadManifest(kind, version string) (string, bool, error) {
	db, err := s.ensure()
	if err != nil {
		return "", false, err
	}
	var raw string
	err = db.QueryRow(`SELECT manifest FROM cache_manifest WHERE kind=? AND version=?`, kind, version).Scan(&raw)
	if err == sqlNoRows {
		return "", false, nil
	}
	return raw, err == nil, err
}
