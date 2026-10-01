// Docker 事实源发现层：看这台机器上 Docker 里**实际有哪些服务容器**，而不是只翻 phpo 自己的安装记录。
//
// 为什么要有这一层：用户用 `docker run`、Docker Desktop 或 compose 装了一个 MySQL，phpo 的库里没这一条，
// 服务页就看不见它；反过来用户在 Docker 那边把容器删了，库里还写着「已安装」，界面也说谎。
// 本层回答的是「现在到底有什么」，把结果交给服务层决定要不要进库存。
//
// 这里只认不删，也只认不建。改不改库里那份「已安装」是服务层的决定，动 Docker 资源是删除链路的事。
//
// 认出三种情况之一才算一个服务：
//  1. 容器名符合 phpo 的命名规矩（phpo-mysql-8.0）——这是 phpo 自己装的；
//  2. 镜像引用能对上某个服务种类的官方镜像，且 tag 能反推出版本（mysql:8 → mysql 8）——外部装的也能认；
//  3. 端口只用来**佐证**：看着像 MySQL（发布 3306）但镜像名认不出版本时，照样不纳管，
//     而是把这一颗单独说清楚——凭端口猜版本等于编数字。
//
// 认不出来的容器不隐身：一个个点名进「认不出的那一堆」，附上为什么认不出（§0.2 规则 41：不得把「不知道」画成「没有」）。
// Swarm 服务跑出来的任务容器**一律不纳管**——那种名字是调度器随起随换的，纳管进去等于让 phpo 的启停去跟 Swarm 抢同一个容器。
package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"

	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

// 认出来的依据。服务层按它决定抽屉那一行怎么说（「按容器名认出」／「按镜像认出」）。
const (
	MatchedName  = "name"
	MatchedImage = "image"
)

// swarmServiceLabel 是 Swarm 给任务容器打的标签。认得出这一颗就够了，不必引 swarm 类型。
const swarmServiceLabel = "com.docker.swarm.service.name"

// FoundService 是认出来的一个服务版本。Name 是容器**在 Docker 上的真名**——
// 外部装的容器名字千奇百怪，后面启停、重建都要用这个真名，不能再拿 phpo-{kind}-{version} 去猜。
type FoundService struct {
	Kind      string
	Version   string
	Name      string
	Image     string // 容器上记的那份镜像引用，可能已被重新 tag
	Running   bool
	MatchedBy string // MatchedName / MatchedImage
	PhpoNamed bool   // 真名正好是 phpo-{kind}-{version}
}

// Unrecognized 是「这里有个容器，但我说不清它是哪个服务的哪个版本」，附一句人话原因。
type Unrecognized struct {
	Name   string
	Image  string
	State  string
	Reason string
}

// Discovery 是一次发现的结果。index 让服务层能按 (kind, version) 查到真名，不必自己再拼一遍。
type Discovery struct {
	Services     []FoundService
	Unrecognized []Unrecognized
	Duplicates   []FoundService // 同一个 (kind, version) 上挤了不止一个容器：只认一个，其余点名报差异
	index        map[string]string
}

// NameFor 查这个服务版本此刻该动哪个容器。查不到即 false——说明 Docker 上没有它的容器。
func (d *Discovery) NameFor(kind, version string) (string, bool) {
	if d == nil {
		return "", false
	}
	name, ok := d.index[discoveryKey(kind, version)]
	return name, ok
}

// discoveryKey 把 kind 与 version 拼成索引键。用一个不可能出现在两者里的分隔符，避免歧义。
func discoveryKey(kind, version string) string { return kind + "\x1f" + version }

// portKinds 是「这个宿主端口一般是谁在用」——只回答服务种类，不回答版本。
// php 不在表里：php-fpm 的 9000 只在容器网络内部用，不发布到宿主（见 service 层的端口口径）。
var portKinds = map[uint16]string{
	3306: string(model.KindMySQL),
	5432: string(model.KindPgsql),
	6379: string(model.KindRedis),
	80:   string(model.KindNginx),
	443:  string(model.KindNginx),
	8080: string(model.KindNginx),
}

// imageKindByRepo 把 registry.go 的「服务种类 → 官方镜像仓库名」反过来用。
// 反过来是必须的：发现层手上只有镜像名，要回答「这是哪种服务」；再手写一份正向表就等于放两份事实，迟早漂移。
var imageKindByRepo = func() map[string]string {
	m := make(map[string]string, len(imageRepos))
	for kind, repo := range imageRepos {
		m[repo] = string(kind)
	}
	return m
}()

// DiscoverServices 列出这台机器上的**所有**容器并逐个分类。一次 ContainerList 问完，不逐个 inspect。
//
// 出错直接上抛：一次 Docker 调用挂了不能当成「这台机器上没有服务」——那会让服务层把库存清空（§0.2 规则 41）。
func (c *Client) DiscoverServices(ctx context.Context) (*Discovery, error) {
	if c == nil {
		return nil, errors.New("尚未准备好 Docker 客户端")
	}
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("列出 Docker 容器失败: %w", err)
	}

	// 先按容器名排序再分类：同一个 (kind, version) 上挤了几个容器时，「谁被认下、谁进差异」必须每次一样，
	// 否则两次「同步状态」会把同一件事报成两个样子（clean_inventory 的稳定输出同一口径）。
	sorted := make([]container.Summary, 0, len(list))
	for _, cs := range list {
		if cs.Labels[swarmServiceLabel] != "" {
			continue
		}
		sorted = append(sorted, cs)
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return firstContainerName(sorted[i].Names) < firstContainerName(sorted[j].Names)
	})

	// swarm 那一堆单独收：它们的标签说明「这是 Swarm 调度的任务」，与纳管无关，但仍要点名。
	swarmTasks := make([]Unrecognized, 0)
	for _, cs := range list {
		if cs.Labels[swarmServiceLabel] == "" {
			continue
		}
		swarmTasks = append(swarmTasks, Unrecognized{
			Name:   firstContainerName(cs.Names),
			Image:  cs.Image,
			State:  string(cs.State),
			Reason: "Swarm 服务的任务容器，名字由调度器随时重建，phpo 不接管",
		})
	}

	d := &Discovery{index: make(map[string]string)}
	for _, cs := range sorted {
		name := firstContainerName(cs.Names)
		if name == "" {
			// Docker 没给名字（极少见，例如刚创建还没起名的容器），跳过而不是纳成一个无名服务。
			continue
		}
		fs, ok := classifyContainer(name, cs)
		if !ok {
			d.Unrecognized = append(d.Unrecognized, Unrecognized{
				Name:   name,
				Image:  cs.Image,
				State:  string(cs.State),
				Reason: unmatchedReason(name, cs),
			})
			continue
		}
		key := discoveryKey(fs.Kind, fs.Version)
		if cur, seen := d.serviceAt(fs.Kind, fs.Version); seen {
			// 冲突只认一个：符合 phpo 命名规矩的那份优先（那才是 phpo 一直在用的容器），
			// 否则保留先到的那份（已按容器名排序，先到即名字更小，两次扫描的结果因此一模一样）。
			// 输的那颗进差异清单点名，绝不重复纳管。
			drop := fs
			if fs.PhpoNamed && !cur.PhpoNamed {
				d.replaceService(fs.Kind, fs.Version, fs)
				drop = cur
			}
			d.Duplicates = append(d.Duplicates, drop)
			continue
		}
		d.index[key] = fs.Name
		d.Services = append(d.Services, fs)
	}

	sort.SliceStable(d.Services, func(i, j int) bool {
		if d.Services[i].Kind != d.Services[j].Kind {
			return d.Services[i].Kind < d.Services[j].Kind
		}
		if d.Services[i].Version != d.Services[j].Version {
			return d.Services[i].Version < d.Services[j].Version
		}
		return d.Services[i].Name < d.Services[j].Name
	})
	sort.SliceStable(d.Unrecognized, func(i, j int) bool { return d.Unrecognized[i].Name < d.Unrecognized[j].Name })
	sort.SliceStable(d.Duplicates, func(i, j int) bool { return d.Duplicates[i].Name < d.Duplicates[j].Name })
	d.Unrecognized = append(d.Unrecognized, swarmTasks...)
	sort.SliceStable(d.Unrecognized, func(i, j int) bool { return d.Unrecognized[i].Name < d.Unrecognized[j].Name })
	return d, nil
}

// serviceAt 按索引找回已认下的那颗。索引与 Services 一一对应，找到即原地改。
func (d *Discovery) serviceAt(kind, version string) (FoundService, bool) {
	for _, s := range d.Services {
		if s.Kind == kind && s.Version == version {
			return s, true
		}
	}
	return FoundService{}, false
}

func (d *Discovery) replaceService(kind, version string, keep FoundService) {
	for i, s := range d.Services {
		if s.Kind == kind && s.Version == version {
			d.Services[i] = keep
			d.index[discoveryKey(kind, version)] = keep.Name
			return
		}
	}
}

// classifyContainer 是三层判据的落点：容器名 → 镜像引用 → （端口只作佐证，见 unmatchedReason）。
func classifyContainer(name string, cs container.Summary) (FoundService, bool) {
	kind, version, ok := kindVersionFromName(name)
	if ok {
		return FoundService{
			Kind: kind, Version: version, Name: name, Image: cs.Image,
			Running: runningState(cs), MatchedBy: MatchedName,
			PhpoNamed: name == dockerutil.ContainerName(kind, version),
		}, true
	}
	kind, version, ok = kindVersionFromImage(cs.Image)
	if ok {
		return FoundService{
			Kind: kind, Version: version, Name: name, Image: cs.Image,
			Running: runningState(cs), MatchedBy: MatchedImage,
			PhpoNamed: name == dockerutil.ContainerName(kind, version),
		}, true
	}
	return FoundService{}, false
}

func runningState(cs container.Summary) bool { return cs.State == container.StateRunning }

// kindVersionFromName 按 phpo 的命名规矩拆容器名：phpo-{kind}-{version}。
//
// 必须逐个服务种类去比前缀，不能「按第一个连字符切」——版本本身可以带连字符
// （phpo-pgsql-17-alpine3.19 的版本是 17-alpine3.19），按第一个连字符切会把它切坏。
// 也不能只靠 IsPhpoResource 判：它对 php- 前缀之外的 phpo-network 同样为真（那是网络不是服务）。
func kindVersionFromName(name string) (kind, version string, ok bool) {
	for _, k := range model.AllKinds {
		prefix := dockerutil.NamespacePrefix + string(k) + "-"
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		v := name[len(prefix):]
		if !versionSafe(v) {
			return "", "", false
		}
		return string(k), v, true
	}
	return "", "", false
}

// kindVersionFromImage 从镜像引用反推服务种类与版本。
//
// 两步：仓库名（取最后一段，这样从镜像源拉下来的 docker.m.daocloud.io/library/mysql:8 也认得出 mysql）
// 对上某个服务种类的官方仓库名；tag 再**顺着正向规则验一遍**——ImageTagFor(kind, 候选版本) 必须正好等于这个 tag。
//
// 为什么要反向验一次：这样只接受「phpo 自己也会装成这个样子」的名字，
// php:latest、php:8.4-cli、以及只带摘要不带 tag 的引用一律认不出版本——不编造版本号（§0.2 规则 2）。
// 注意别用 registry.go 的 splitRef，也别用 dockerutil 的 TagOrDefault/String/FormatRef：
// 那几处无 tag 时都回落 latest，等于替用户凭空造一个版本。
func kindVersionFromImage(image string) (kind, version string, ok bool) {
	ref := dockerutil.ParseImageRef(image)
	if ref.Tag == "" {
		return "", "", false
	}
	repo := ref.Name
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	k, known := imageKindByRepo[repo]
	if !known {
		return "", "", false
	}
	v, ok := versionFromTag(k, ref.Tag)
	if !ok {
		return "", "", false
	}
	return k, v, true
}

// versionFromTag 把 tag 换成版本：php 的 -fpm 后缀是 ImageTagFor 加的，去掉即版本；其余原样。
// 去掉后还要正着再算一次对得上才算数（大小写、多余后缀都在这一步挡掉）。
func versionFromTag(kind, tag string) (string, bool) {
	cand := tag
	if kind == string(model.KindPHP) {
		if !strings.HasSuffix(tag, "-fpm") {
			return "", false
		}
		cand = strings.TrimSuffix(tag, "-fpm")
	}
	if !versionSafe(cand) || ImageTagFor(kind, cand) != tag {
		return "", false
	}
	return cand, true
}

// unmatchedReason 给一句「为什么这颗认不出来」。端口就是第三层判据：
// 端口能说出这是哪种服务（发布 3306 看着像 MySQL），但版本仍必须来自镜像引用——
// 只凭端口纳管等于猜版本，所以这里把它写清楚，让用户知道缺的是哪一半。
func unmatchedReason(name string, cs container.Summary) string {
	if hint := kindHintFromPorts(cs); hint != "" {
		return fmt.Sprintf("看着像 %s，但镜像引用 %q 认不出版本，phpo 不接管", hint, cs.Image)
	}
	if name != "" && strings.HasPrefix(name, dockerutil.NamespacePrefix) {
		return "容器名带 phpo- 前缀，但不属于五个服务种类之一，phpo 不接管"
	}
	return fmt.Sprintf("认不出服务种类或版本（镜像 %q）：既不是 phpo 的容器名，也不是五种服务的官方镜像", cs.Image)
}

// kindHintFromPorts 看这个容器有没有发布某个「一眼能认出种类」的宿主端口，并把端口一起报出来。
// 只看 tcp——服务端口规格里全是 /tcp，UDP 撞上同一号端口不该被当成同一个服务。
// 光说「看着像 nginx」不够：用户得能核对是凭哪颗端口这么猜的，而猜不出版本这件事恰恰发生在他要决定下一步的时候。
func kindHintFromPorts(cs container.Summary) string {
	kinds := make([]string, 0, len(cs.Ports))
	ports := make(map[string]map[uint16]bool, len(cs.Ports))
	for _, p := range cs.Ports {
		if !strings.EqualFold(p.Type, "tcp") || p.PublicPort == 0 {
			continue
		}
		k, ok := portKinds[p.PrivatePort]
		if !ok {
			continue
		}
		if ports[k] == nil {
			ports[k] = make(map[uint16]bool, 2)
			kinds = append(kinds, k)
		}
		ports[k][p.PrivatePort] = true
	}
	if len(kinds) == 0 {
		return ""
	}
	sort.Strings(kinds)
	hints := make([]string, 0, len(kinds))
	for _, k := range kinds {
		nums := make([]int, 0, len(ports[k]))
		for n := range ports[k] {
			nums = append(nums, int(n))
		}
		sort.Ints(nums)
		parts := make([]string, 0, len(nums))
		for _, n := range nums {
			parts = append(parts, strconv.Itoa(n))
		}
		hints = append(hints, fmt.Sprintf("%s（发布了 %s 的端口）", k, strings.Join(parts, "/")))
	}
	return strings.Join(hints, "、")
}

// versionSafe 是 §5.4 那条版本校验在这里的最小落地：非空、不含路径分隔符与 ..、不含空字节、长度 ≤ 128。
//
// 为什么要在引擎层自己判一遍：config 层没有导出的版本校验出口（只有域名/站点根/扩展名/镜像源/根路径），
// 而 dockerutil.ValidName 要求带 phpo- 前缀，用它等于把外部容器全挡掉。
// Docker 自己的 tag 语法本来也不允许这些字符，这一层是兜底——万一读到坏值，宁可不认，
// 也不要把带 `..` 的东西当成版本号写进库存（硬红线 3）。
func versionSafe(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	if strings.ContainsAny(v, `/\`) || strings.Contains(v, "..") || strings.ContainsRune(v, 0) {
		return false
	}
	return v == strings.TrimSpace(v)
}
