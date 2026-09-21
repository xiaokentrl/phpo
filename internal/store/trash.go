// 回收站条目：`<用户数据目录>/trash/` 的登记簿（保留期 7 天，§5.13.7）
package store

import "time"

type TrashItem struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	OrigPath  string    `json:"origPath"`
	TrashPath string    `json:"trashPath"`
	MovedAt   time.Time `json:"movedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// TrashRetention 回收站保留期（§0.3 权威值 7 天）
const TrashRetention = 7 * 24 * time.Hour

func (s *Store) AddTrashItem(item TrashItem) (int64, error) {
	if item.MovedAt.IsZero() {
		item.MovedAt = time.Now().UTC()
	}
	if item.ExpiresAt.IsZero() {
		item.ExpiresAt = item.MovedAt.Add(TrashRetention)
	}
	db, err := s.ensure()
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(`INSERT INTO trash(kind,orig_path,trash_path,moved_at,expires_at) VALUES(?,?,?,?,?)`,
		item.Kind, item.OrigPath, item.TrashPath,
		item.MovedAt.Format(time.RFC3339Nano), item.ExpiresAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListTrash() ([]TrashItem, error) {
	db, err := s.ensure()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id,kind,orig_path,trash_path,moved_at,expires_at FROM trash ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrashItem
	for rows.Next() {
		var it TrashItem
		var mt, et string
		if err := rows.Scan(&it.ID, &it.Kind, &it.OrigPath, &it.TrashPath, &mt, &et); err != nil {
			return nil, err
		}
		it.MovedAt, _ = time.Parse(time.RFC3339Nano, mt)
		it.ExpiresAt, _ = time.Parse(time.RFC3339Nano, et)
		out = append(out, it)
	}
	return out, nil
}

func (s *Store) RemoveTrashItem(id int64) error {
	db, err := s.ensure()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM trash WHERE id=?`, id)
	return err
}

// ExpiredTrash 到期条目（清空动作在 engine/trash.go，M6 接入）
func (s *Store) ExpiredTrash(now time.Time) ([]TrashItem, error) {
	db, err := s.ensure()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id,kind,orig_path,trash_path,moved_at,expires_at FROM trash WHERE expires_at <= ?`,
		now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrashItem
	for rows.Next() {
		var it TrashItem
		var mt, et string
		if err := rows.Scan(&it.ID, &it.Kind, &it.OrigPath, &it.TrashPath, &mt, &et); err != nil {
			return nil, err
		}
		it.MovedAt, _ = time.Parse(time.RFC3339Nano, mt)
		it.ExpiresAt, _ = time.Parse(time.RFC3339Nano, et)
		out = append(out, it)
	}
	return out, nil
}
