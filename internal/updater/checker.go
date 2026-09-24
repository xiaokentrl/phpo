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

// Asset 发布清单里的一条平台安装包（多平台各一份，由 resolvePlatform 按本机选出一颗）
type Asset struct {
	OS        string `json:"os"`        // linux / windows / darwin（runtime.GOOS 同词表）
	Format    string `json:"format"`    // 仅 linux 有意义：deb / rpm / appimage
	URL       string `json:"url"`       //
	Size      int64  `json:"size"`      //
	SHA256    string `json:"sha256"`    //
	Signature string `json:"signature"` //
}

// Release 一条远端发布信息（含下载与完整性元数据）
// URL/Size/SHA256/Signature 是「本次要装的那一颗」：来自顶层（旧式单包清单）或由 Assets 按本机平台解析而来。
type Release struct {
	Version      string  `json:"version"`
	Changelog    string  `json:"changelog"`
	DownloadPage string  `json:"download_page,omitempty"` // 「打开下载页」按钮的目标地址（发布页）；清单不写即无此按钮
	Size         int64   `json:"size"`
	URL          string  `json:"url"`
	SHA256       string  `json:"sha256"`    // 期望校验值（小写 hex）
	Signature    string  `json:"signature"` // base64(Ed25519 over SHA256 hex)
	Assets       []Asset `json:"assets,omitempty"`

	// Source 命中本清单的发布源名（github / gitee / …）；清单里不写，由 Sources 探测成功后回填，供界面显示「更新源」
	Source string `json:"-"`
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
		Version:      r.Version,
		Changelog:    r.Changelog,
		Size:         r.Size,
		Source:       r.Source,
		DownloadPage: r.DownloadPage,
	})
	return r, true, nil
}
