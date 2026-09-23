// 状态校验：比对「Docker 实际态 ≡ SQLite 期望态」，产出漂移结论（§5.13.1 一致性；§5.6 docker:state-drift）
// VerifyState 为纯比对无 IO，供 Post-Verify 与 T306 校准复用；实际态由 ManagedContainers 依归属标签查询。
package engine

import (
	"context"
	"sort"

	"phpo/pkg/dockerutil"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// ContainerRef phpo 托管容器的标识（kind+version 二元组，映射命名规范 phpo-{kind}-{version}）。
type ContainerRef struct {
	Kind    string
	Version string
}

// Name 依命名规范还原容器名（§5.13.2）。
func (r ContainerRef) Name() string { return dockerutil.ContainerName(r.Kind, r.Version) }

// ActualState 单个容器的实际运行态，是比对的单元。
type ActualState struct {
	Ref     ContainerRef
	Running bool
}

// Drift 「期望 ≡ 实际」被破坏时的差异集合；四者皆空即一致。列表均已排序，输出确定。
type Drift struct {
	Missing    []string // 期望存在、Docker 实际无（未落地）
	Extra      []string // Docker 实际有、期望无（脏残留 / 孤儿）
	Stopped    []string // 期望运行、实际已停止
	Unexpected []string // 期望停止、实际却在运行
}

// Empty 无漂移，即实际态 ≡ 期望态。
func (d Drift) Empty() bool {
	return len(d.Missing) == 0 && len(d.Extra) == 0 && len(d.Stopped) == 0 && len(d.Unexpected) == 0
}

// VerifyState 纯比对：installed/running 为期望版本集（running ⊆ installed），actual 为 Docker 现存托管容器。
// 不做任何 IO，逐项核对差异；供 Op.PostVerify 与状态校准共用同一判定口径。
func VerifyState(installed, running []ContainerRef, actual []ActualState) Drift {
	wantInstalled := nameSet(installed)
	wantRunning := nameSet(running)

	got := make(map[string]bool, len(actual)) // 实际存在
	gotRunning := make(map[string]bool, len(actual))
	for _, a := range actual {
		n := a.Ref.Name()
		got[n] = true
		if a.Running {
			gotRunning[n] = true
		}
	}

	var d Drift
	for n := range wantInstalled {
		if !got[n] {
			d.Missing = append(d.Missing, n)
		}
	}
	for n := range got {
		if !wantInstalled[n] {
			d.Extra = append(d.Extra, n)
		}
	}
	for n := range wantRunning {
		if got[n] && !gotRunning[n] {
			d.Stopped = append(d.Stopped, n)
		}
	}
	for n := range gotRunning {
		// 仅就「已知已安装」的服务判断意外运行；孤儿容器已归入 Extra，不重复计入
		if wantInstalled[n] && !wantRunning[n] {
			d.Unexpected = append(d.Unexpected, n)
		}
	}
	sortNames(d)
	return d
}

func nameSet(refs []ContainerRef) map[string]bool {
	set := make(map[string]bool, len(refs))
	for _, r := range refs {
		set[r.Name()] = true
	}
	return set
}

func sortNames(d Drift) {
	sort.Strings(d.Missing)
	sort.Strings(d.Extra)
	sort.Strings(d.Stopped)
	sort.Strings(d.Unexpected)
}

// ManagedContainers 列出 Docker 中 phpo 托管的服务容器实际态（按归属标签过滤，名称排序）。
// 无归属 kind/version 标签的资源（如共享网络）不计入比对单元。
func (c *Client) ManagedContainers(ctx context.Context) ([]ActualState, error) {
	list, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "phpo.managed=true")),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ActualState, 0, len(list))
	for _, cs := range list {
		kind, ver := cs.Labels["phpo.kind"], cs.Labels["phpo.version"]
		if kind == "" || ver == "" {
			continue
		}
		// container.State 为字符串别名（v28 无常量），运行态即 "running"
		out = append(out, ActualState{
			Ref:     ContainerRef{Kind: kind, Version: ver},
			Running: cs.State == "running",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.Name() < out[j].Ref.Name() })
	return out, nil
}
