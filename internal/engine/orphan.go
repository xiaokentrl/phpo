// 孤儿资源扫描（§5.13.5）：在 phpo- 命名空间内检出「Docker 有、期望无 / 无人引用」的容器/卷/网络/镜像。
// DetectOrphans 为纯判定，输入采集后的记录即可确定性单测（预置孤儿矩阵）；ScanOrphans 负责按归属标签从 SDK 采集。
// 隔离性：只报 phpo 托管资源，绝不触碰外部镜像/卷/网络（§5.13.3）。默认保留卷——是否删除由清理三模式决定（§5.13.6）。
package engine

import (
	"context"
	"sort"
	"strings"

	"phpo/internal/model"
	"phpo/pkg/dockerutil"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
)

// ContainerRecord 采集后的单个 phpo 容器概览
type ContainerRecord struct {
	Name    string // phpo-{kind}-{version}
	Image   string // 运行镜像引用（repo:tag）
	Running bool
	Volumes []string // 挂载的具名卷名
}

// VolumeRecord 采集后的单个 phpo 卷
type VolumeRecord struct {
	Name string
	Size int64
}

// NetworkRecord 采集后的单个 phpo 网络
type NetworkRecord struct {
	Name     string
	Attached int // 连接中的容器数
}

// ImageRecord 采集后的镜像
type ImageRecord struct {
	ID   string
	Refs []string // RepoTags
	Size int64
}

// OrphanInput 纯判定的输入：installed 为期望存在的容器名集合，其余为 Docker 实际采集
type OrphanInput struct {
	Installed  map[string]bool
	Containers []ContainerRecord
	Volumes    []VolumeRecord
	Networks   []NetworkRecord
	Images     []ImageRecord
}

// DetectOrphans 纯判定四类孤儿（均限定 phpo 命名空间）：
//   - 容器：不在期望安装集
//   - 卷：无任何「已安装容器」挂载（孤儿容器所占卷亦计为孤儿，以便整体清除）
//   - 网络：phpo-* 且非共享 phpo-network 且无连接容器
//   - 镜像：phpo/ 前缀（扩展固化镜像）且无已安装容器引用
//
// 输出各列表按名称排序，结果确定。
func DetectOrphans(in OrphanInput) model.OrphanReport {
	var rep model.OrphanReport

	usedVol := map[string]bool{}
	usedImg := map[string]bool{}
	for _, c := range in.Containers {
		if !in.Installed[c.Name] {
			if !dockerutil.IsPhpoResource(c.Name) {
				continue // 防御性隔离：非 phpo 命名空间容器绝不报孤儿
			}
			rep.Containers = append(rep.Containers, model.DockerResource{
				Type: model.ResContainer, ID: c.Name, Name: c.Name, InUse: c.Running,
			})
			continue // 孤儿容器不计入引用方：其所占卷/镜像一并暴露为孤儿
		}
		for _, v := range c.Volumes {
			usedVol[v] = true
		}
		if c.Image != "" {
			usedImg[c.Image] = true
		}
	}

	for _, v := range in.Volumes {
		if dockerutil.IsPhpoResource(v.Name) && !usedVol[v.Name] {
			rep.Volumes = append(rep.Volumes, model.DockerResource{
				Type: model.ResVolume, ID: v.Name, Name: v.Name, Size: v.Size,
			})
		}
	}

	for _, n := range in.Networks {
		if n.Name == dockerutil.NetworkName {
			continue // 共享网络常驻，永不算孤儿
		}
		if dockerutil.IsPhpoResource(n.Name) && n.Attached == 0 {
			rep.Networks = append(rep.Networks, model.DockerResource{
				Type: model.ResNetwork, ID: n.Name, Name: n.Name,
			})
		}
	}

	for _, im := range in.Images {
		for _, ref := range im.Refs {
			if strings.HasPrefix(ref, "phpo/") && !usedImg[ref] {
				rep.Images = append(rep.Images, model.DockerResource{
					Type: model.ResImage, ID: im.ID, Name: ref, Size: im.Size,
				})
				break
			}
		}
	}

	sortResources(rep)
	return rep
}

func sortResources(r model.OrphanReport) {
	by := func(rs []model.DockerResource) {
		sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	}
	by(r.Containers)
	by(r.Volumes)
	by(r.Networks)
	by(r.Images)
}

// managedFilter 归属标签过滤（phpo.managed=true）
func managedFilter() filters.Args {
	return filters.NewArgs(filters.Arg("label", "phpo.managed=true"))
}

// ScanOrphans 从 Docker 按归属标签采集 phpo 资源后交纯判定（installed 为期望容器名集）。
func (c *Client) ScanOrphans(ctx context.Context, installed map[string]bool) (model.OrphanReport, error) {
	cs, err := c.listPhpoContainers(ctx)
	if err != nil {
		return model.OrphanReport{}, err
	}
	vs, err := c.listPhpoVolumes(ctx)
	if err != nil {
		return model.OrphanReport{}, err
	}
	ns, err := c.listPhpoNetworks(ctx)
	if err != nil {
		return model.OrphanReport{}, err
	}
	is, err := c.listPhpoImages(ctx)
	if err != nil {
		return model.OrphanReport{}, err
	}
	return DetectOrphans(OrphanInput{
		Installed:  installed,
		Containers: cs,
		Volumes:    vs,
		Networks:   ns,
		Images:     is,
	}), nil
}

// listPhpoContainers 列出带 phpo 归属标签的容器（含已停止）并转记录
func (c *Client) listPhpoContainers(ctx context.Context) ([]ContainerRecord, error) {
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: managedFilter()})
	if err != nil {
		return nil, err
	}
	out := make([]ContainerRecord, 0, len(list))
	for _, cs := range list {
		kind, ver := cs.Labels["phpo.kind"], cs.Labels["phpo.version"]
		if kind == "" || ver == "" {
			continue // 无 kind/version 归属（如临时容器）不作比对单元
		}
		var vols []string
		for _, m := range cs.Mounts {
			if string(m.Type) == "volume" && m.Name != "" {
				vols = append(vols, m.Name)
			}
		}
		out = append(out, ContainerRecord{
			Name:    dockerutil.ContainerName(kind, ver),
			Image:   cs.Image,
			Running: cs.State == "running",
			Volumes: vols,
		})
	}
	return out, nil
}

func (c *Client) listPhpoVolumes(ctx context.Context) ([]VolumeRecord, error) {
	resp, err := c.cli.VolumeList(ctx, volume.ListOptions{Filters: managedFilter()})
	if err != nil {
		return nil, err
	}
	out := make([]VolumeRecord, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil || !dockerutil.IsPhpoResource(v.Name) {
			continue
		}
		var size int64
		if v.UsageData != nil {
			size = v.UsageData.Size
		}
		out = append(out, VolumeRecord{Name: v.Name, Size: size})
	}
	return out, nil
}

func (c *Client) listPhpoNetworks(ctx context.Context) ([]NetworkRecord, error) {
	list, err := c.cli.NetworkList(ctx, network.ListOptions{Filters: managedFilter()})
	if err != nil {
		return nil, err
	}
	out := make([]NetworkRecord, 0, len(list))
	for _, n := range list {
		if !dockerutil.IsPhpoResource(n.Name) {
			continue
		}
		out = append(out, NetworkRecord{Name: n.Name, Attached: len(n.Containers)})
	}
	return out, nil
}

// listPhpoImages 列出扩展固化镜像（phpo/ 前缀）；其余镜像归 Docker/用户，不纳入 phpo 命名空间扫描。
func (c *Client) listPhpoImages(ctx context.Context) ([]ImageRecord, error) {
	list, err := c.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]ImageRecord, 0, len(list))
	for _, im := range list {
		hasPhpo := false
		for _, ref := range im.RepoTags {
			if strings.HasPrefix(ref, "phpo/") {
				hasPhpo = true
				break
			}
		}
		if !hasPhpo {
			continue
		}
		out = append(out, ImageRecord{ID: im.ID, Refs: im.RepoTags, Size: im.Size})
	}
	return out, nil
}

// RemoveNetwork 删除网络（幂等：不存在视为成功）
func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	if err := c.cli.NetworkRemove(ctx, name); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
