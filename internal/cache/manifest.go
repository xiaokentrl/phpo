// 缓存清单读写：manifest.json（schema_version=1，snake_case 冻结契约）
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"phpo/internal/model"
)

const SchemaVersion = 1

// LoadManifest 读取清单；文件不存在返回空清单（非错误）
func (m *Manager) LoadManifest(kind, version string) (*model.CacheManifest, error) {
	path := m.env.OfflineManifestFile(kind, version)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newManifest(kind, version), nil
		}
		return nil, err
	}
	var mf model.CacheManifest
	if err := json.Unmarshal(b, &mf); err != nil {
		return nil, err
	}
	return &mf, nil
}

// SaveManifest 原子写清单到 {kind}/{version}/manifest.json
func (m *Manager) SaveManifest(mf *model.CacheManifest) error {
	path := m.env.OfflineManifestFile(mf.Kind, mf.Version)
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	mf.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newManifest(kind, version string) *model.CacheManifest {
	now := time.Now().UTC()
	return &model.CacheManifest{
		SchemaVersion: SchemaVersion,
		Kind:          kind,
		Version:       version,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func dirOf(p string) string {
	// 截到最后一个 '/'
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
