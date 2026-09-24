// 多发布源探测（§5.9）：并发拉取所有源（各自独立超时），取版本序最新的一份；命中后按本机平台选出安装包。
// 源清单来自 config.yaml 的 update_sources（数组，用户可增删），匿名读取、不存任何凭据（§1.5）。
package updater

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"phpo/pkg/version"
)

// SourceTimeout 单个发布源的连接超时；超时即判该源不可用（并发探测，不拖累其余源）（需求：15 秒）
const SourceTimeout = 15 * time.Second

// Source 一个可检测的发布清单来源
type Source struct {
	Name        string // github / gitee / 自定义名
	ManifestURL string // 发布清单（JSON）地址
}

// Sources 并发探测多个发布源并取版本最新的一份；实现 ReleaseSource，故 Checker / Updater 无需改动
type Sources struct {
	list    []Source
	timeout time.Duration
}

func NewSources(list []Source) *Sources {
	return &Sources{list: list, timeout: SourceTimeout}
}

// List 返回已配置的源（供界面显示「更新源」候选）
func (s *Sources) List() []Source { return s.list }

// FetchLatest 并发探测全部源（每源独立超时），在成功的源里取**版本序最新**的一份。
// 不采用「按序命中即返」：镜像源常滞后于原始发布（gitee 尚未同步 GitHub 的最新版），
// 命中即返等于把「自动检测最后发布的版本」做成「第一个可用源宣称的版本」。
// 版本并列时取配置顺序靠前的一份（数组顺序仍是并列裁决与报错点名顺序）；全部失败才报错并逐源点名。
func (s *Sources) FetchLatest(ctx context.Context) (*Release, error) {
	if len(s.list) == 0 {
		return nil, fmt.Errorf("updater: 未配置任何发布源")
	}
	type outcome struct {
		rel *Release
		err error
	}
	// 每源一个槽位，goroutine 只写自己那一格，无需加锁
	results := make([]outcome, len(s.list))
	var wg sync.WaitGroup
	for i, src := range s.list {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			rel, err := s.fetchOne(ctx, src)
			results[i] = outcome{rel, err}
		}(i, src)
	}
	wg.Wait()

	var best *Release
	fails := make([]string, 0, len(s.list))
	for i, o := range results {
		if o.err != nil {
			fails = append(fails, fmt.Sprintf("%s: %v", s.list[i].Name, o.err))
			continue
		}
		if best == nil || version.CmpVer(o.rel.Version, best.Version) < 0 {
			best = o.rel
		}
	}
	if best == nil {
		return nil, fmt.Errorf("updater: 所有发布源均不可用（%s）", strings.Join(fails, "；"))
	}
	return best, nil
}

// fetchOne 单源探测：独立超时（一个源卡住不拖住整次检查），并解析出本机平台包
func (s *Sources) fetchOne(ctx context.Context, src Source) (*Release, error) {
	if src.ManifestURL == "" {
		return nil, fmt.Errorf("未配置清单地址")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rel, err := HTTPSource{URL: src.ManifestURL}.FetchLatest(ctx)
	if err != nil {
		return nil, err
	}
	if rel.Version == "" {
		return nil, fmt.Errorf("清单缺 version 字段")
	}
	if err := rel.resolvePlatform(currentPlatform()); err != nil {
		return nil, err
	}
	rel.Source = src.Name
	return rel, nil
}

// platform 本机安装目标：操作系统 + Linux 的包格式
type platform struct{ goos, format string }

// currentPlatform 取法可被单测替换（本机 linux 恒为 deb，无法覆盖 rpm 分支）
var currentPlatform = func() platform {
	p := platform{goos: runtime.GOOS}
	if p.goos == "linux" {
		p.format = linuxPkgFormat()
	}
	return p
}

// linuxPkgFormat Debian 系（含 Ubuntu）用 deb，其余按 rpm
func linuxPkgFormat() string {
	if _, err := os.Stat("/etc/debian_version"); err == nil {
		return "deb"
	}
	return "rpm"
}

// resolvePlatform 从 assets 里挑本机那一份，写进顶层 URL/Size/SHA256/Signature，
// 使下载、双校验、安装三步继续读同一组字段。无 assets 即旧式单包清单，顶层字段原样可用。
func (r *Release) resolvePlatform(p platform) error {
	if len(r.Assets) == 0 {
		if r.URL == "" {
			return fmt.Errorf("清单既无 assets 也无 url")
		}
		return nil
	}
	// 先精确匹配 os+format（Linux 上 deb 与 rpm 并存时取对的那一份），再放宽到仅匹配 os
	for _, strict := range []bool{true, false} {
		for i := range r.Assets {
			a := r.Assets[i]
			if !strings.EqualFold(a.OS, p.goos) {
				continue
			}
			if strict && p.format != "" && !strings.EqualFold(a.Format, p.format) {
				continue
			}
			r.URL, r.Size, r.SHA256, r.Signature = a.URL, a.Size, a.SHA256, a.Signature
			return nil
		}
	}
	want := p.goos
	if p.format != "" {
		want += "/" + p.format
	}
	return fmt.Errorf("清单内无本机平台（%s）的安装包", want)
}
