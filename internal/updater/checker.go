// 升级检查器（§5.9 / 决策 §1.10）：拉取发布清单 → 版本比较 → 发现新版本发 update:available
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"phpo/internal/model"
	"phpo/pkg/version"
)

// Emitter 本地最小事件接口（避免反向依赖装配层）
type Emitter interface {
	Emit(event string, payload any)
}

// NopEmitter 单测占位
type NopEmitter struct{}

func (NopEmitter) Emit(string, any) {}

// Release 一条远端发布信息（含下载与完整性元数据）
type Release struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`    // 期望校验值（小写 hex）
	Signature string `json:"signature"` // base64(Ed25519 over SHA256 hex)
}

// ReleaseSource 发布清单来源；HTTPSource 为默认实现
type ReleaseSource interface {
	FetchLatest(ctx context.Context) (*Release, error)
}

// HTTPSource 从受信任 URL 拉取 JSON 发布清单
type HTTPSource struct {
	URL    string
	Client *http.Client
}

func (s HTTPSource) FetchLatest(ctx context.Context) (*Release, error) {
	c := s.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: 发布清单返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var r Release
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("updater: 解析发布清单失败: %w", err)
	}
	return &r, nil
}

// Checker 版本检查核心；current 为当前应用版本
type Checker struct {
	current string
	src     ReleaseSource
	em      Emitter
}

func NewChecker(current string, src ReleaseSource, em Emitter) *Checker {
	if em == nil {
		em = NopEmitter{}
	}
	return &Checker{current: current, src: src, em: em}
}

// IsNewer latest 是否新于 current（用 CmpVer 降序比较器：新者返回负）
func IsNewer(latest, current string) bool {
	return version.CmpVer(latest, current) < 0
}

// Check 拉取清单；若有新版本则发射 update:available 并返回该发布
func (c *Checker) Check(ctx context.Context) (*Release, bool, error) {
	r, err := c.src.FetchLatest(ctx)
	if err != nil {
		return nil, false, err
	}
	if !IsNewer(r.Version, c.current) {
		return r, false, nil
	}
	c.em.Emit("update:available", model.UpdateAvailable{
		Version:   r.Version,
		Changelog: r.Changelog,
		Size:      r.Size,
	})
	return r, true, nil
}
