// Docker 全量资源清理 · 删除层：把用户勾中的那几样，一样一样从宿主 Docker 里删掉。
//
// 这层存在的理由有三个，每一个都不能靠上层代办：
//
//  1. 已有的删除能力（orphan.go / volume.go / image.go / container.go）都是**phpo 自己那份命名空间**
//     的收尾动作——它们只认 `phpo-` 前缀的资源，且各自只删一类。总览页那 60 行是**全量**口径，
//     要删的是「这台机器上 Docker 的每一份资源」，其中大半不是 phpo 建的。故这里单独一个入口，
//     但**复用的是同一批底层删除函数**，不另立第二套删除实现。
//  2. 「一颗失败就把整单停下」在这一行清单上是错的：用户勾了 12 项，前 3 项删掉了、第 4 项因
//     镜像正被容器占用而失败，剩下 8 项照样该删完——界面要逐行点名哪颗没删掉，而不是一句「清理失败」
//     让全部落空。因此这个循环**永不因单项失败中断**，失败只写在该项的结果里。
//  3. 删除是有时长的（构建缓存那一次清扫可能几十秒）。界面要边删边看到行，所以每完成一项就回调一次，
//     不等整批结束。
//
// 这层**只删 Docker 侧的东西**：容器、镜像、卷、网络、插件、Swarm 对象、构建缓存。
// 宿主机上的文件（检查点、cgroup、日志、`/var/lib/docker` 那一片）不在这里——那需要提权，
// 且删的多半属主是 root，实现另立一文件（clean_hostdel.go），但共用这里的删除循环。
// 卷要不要先进回收站、phpo 自己的容器要不要顺手降级成「缺失态」，都由服务层决定——这里不替它做主。
package engine

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
)

// 要删的 Docker 对象类型。值与 model 侧的 ResourceType 对齐（同一份词，两处不得各说一套）。
const (
	DelContainer    = "container"
	DelImage        = "image"
	DelVolume       = "volume"
	DelNetwork      = "network"
	DelPlugin       = "plugin"
	DelSwarmService = "swarm_service"
	DelSwarmConfig  = "swarm_config"
	DelSwarmSecret  = "swarm_secret"
	// DelBuildCache 是**整份构建缓存一次清**（Docker 没有「删某一条缓存记录」这个 API）。
	// 因此这一类在预览里只算一项——用户看到的是一颗开关，不是一份逐条清单。
	DelBuildCache = "prune_build_cache"
)

// DeleteOp 是「删这一样」的指令。
// Ref 是 Docker 认的那个名字：容器/卷/网络/插件给名字或 ID 都行（SDK 两认），镜像给 tag 或 digest，
// Swarm 对象只认 ID。Size 是扫描时数出来的字节数，删成功就按它记账（见 DeleteResult.Freed）。
//
// NeedsRoot 与 TrashIt 是给宿主文件那一侧用的（见 clean_hostdel.go），Docker 侧一律为假：
// 走 SDK 就不存在「要提权」，而卷里的数据是否先进回收站由服务层按「是不是 phpo 自己建的」决定，
// 不在这层替用户做主。
type DeleteOp struct {
	Row       string
	Kind      string
	Ref       string
	Size      int64
	NeedsRoot bool // 真表示这份东西属主是 root，删除/移动必须提权
	TrashIt   bool // 真表示不直接删干净，先挪进回收站（有数据的那些，需求 ④）
}

// DeleteResult 是「这一样删得怎么样」的回执，与 ops 一一对应、顺序一致。
// Freed 只在这项真删掉时给数：普通对象取扫描时数到的那份体积，构建缓存取 Docker 自己报的回收字节数。
// 没删掉的项 Freed 恒为 0——把失败项的体积算进「已释放」等于虚报磁盘空间。
// TrashPath 是这项在回收站里的落点（只有 TrashIt 那几项有），服务层拿它登记 7 天到期记录。
type DeleteResult struct {
	Row       string
	Kind      string
	Ref       string
	Freed     int64
	TrashPath string
	Err       error
}

// ItemFn 是「每一项删完就叫一次」。允许传 nil（调用方不关心逐行回执时）。
type ItemFn func(res DeleteResult)

func (f ItemFn) report(res DeleteResult) {
	if f != nil {
		f(res)
	}
}

// DeleteDockerObjects 按顺序删完 ops 里的每一项，返回与 ops 等长、同序的回执。
//
// 三条口径：
//   - **单项失败不中断**：失败只落在该项的 Err，后面的照常删（服务层据此逐行点名）。
//   - **取消优先**：ctx 一取消就停止发起新的删除，剩余项各带一条 ctx.Err() 的回执——
//     它们没被删过，必须看得见，不能凭空消失让界面少报几行。
//   - **幂等**：目标已经不在（被第三方工具先删了）当作删除成功。用户点「彻底清空」要的结果
//     就是「这东西现在没有了」，现在确实没有了，不该报一次失败。
func (c *Client) DeleteDockerObjects(ctx context.Context, ops []DeleteOp, onItem ItemFn) []DeleteResult {
	return runDeletes(ctx, ops, onItem, c.deleteOne)
}

// runDeletes 是上面那三条口径的实现，把「怎么删」抽成参数：
// 循环语义（不中断 / 取消优先 / 回执等长同序 / 失败项不计体积）是不该依赖 Docker 守护进程的，
// 单测因此能在没有 Docker 的机器上锁死它。
//
// del 的第二个返回值是该项在回收站里的落点，只有「先进回收站再删」那几项才有；
// 它只在删成时才跟着进回执——一项没删掉，却说它「已经在回收站里」等于假账。
func runDeletes(ctx context.Context, ops []DeleteOp, onItem ItemFn,
	del func(context.Context, DeleteOp) (int64, string, error),
) []DeleteResult {
	res := make([]DeleteResult, 0, len(ops))
	for i, op := range ops {
		if err := ctx.Err(); err != nil {
			for _, p := range ops[i:] {
				out := DeleteResult{Row: p.Row, Kind: p.Kind, Ref: p.Ref, Err: err}
				res = append(res, out)
				onItem.report(out)
			}
			return res
		}

		freed, trash, err := del(ctx, op)
		out := DeleteResult{Row: op.Row, Kind: op.Kind, Ref: op.Ref, Err: err}
		if err == nil {
			out.Freed = freed
			out.TrashPath = trash
		}
		res = append(res, out)
		onItem.report(out)
	}
	return res
}

// deleteOne 把一条指令翻成一次 SDK 调用，并给出这次实际释放了多少字节。
//
// Docker 侧没有「先进回收站」这一说（卷里的数据是否留一份由服务层决定，而那一项走的是
// 宿主文件那条路，见 clean_hostdel.go），故回收站落点恒为空。
func (c *Client) deleteOne(ctx context.Context, op DeleteOp) (int64, string, error) {
	freed, err := c.removeByKind(ctx, op)
	return freed, "", err
}

// removeByKind 删掉这一样，返回本次释放的字节数。
//
// 默认沿用扫描时数到的那份体积（op.Size）；只有构建缓存例外——Docker 会在清扫的响应里
// 给出它自己确认的数字，那个数比扫描时的估计准，必须用后者。
// 体积必须由这里**返回**而不是就地改 op：op 是值传递，改它调用方看不见。
func (c *Client) removeByKind(ctx context.Context, op DeleteOp) (int64, error) {
	switch op.Kind {
	case DelContainer:
		// 复用既有实现：强制移除、不带 -v（卷里的数据必须由用户另外决定，§0.2 规则 20）
		if err := c.RemoveContainer(ctx, op.Ref); err != nil {
			return 0, err
		}
		return op.Size, nil
	case DelImage:
		if err := c.ImageRemove(ctx, op.Ref); err != nil {
			return 0, err
		}
		return op.Size, nil
	case DelVolume:
		if err := c.RemoveVolume(ctx, op.Ref); err != nil {
			return 0, err
		}
		return op.Size, nil
	case DelNetwork:
		if err := c.RemoveNetwork(ctx, op.Ref); err != nil {
			return 0, err
		}
		return op.Size, nil

	case DelPlugin:
		if err := c.cli.PluginRemove(ctx, op.Ref, types.PluginRemoveOptions{Force: true}); err != nil {
			if isNotFound(err) {
				return op.Size, nil
			}
			return 0, fmt.Errorf("docker plugin rm 失败: %w", err)
		}
		return op.Size, nil

	case DelSwarmService:
		if err := c.cli.ServiceRemove(ctx, op.Ref); err != nil {
			if isNotFound(err) {
				return op.Size, nil
			}
			return 0, fmt.Errorf("删除 swarm 服务失败: %w", err)
		}
		return op.Size, nil

	case DelSwarmConfig:
		if err := c.cli.ConfigRemove(ctx, op.Ref); err != nil {
			if isNotFound(err) {
				return op.Size, nil
			}
			return 0, fmt.Errorf("删除 swarm 配置失败: %w", err)
		}
		return op.Size, nil

	case DelSwarmSecret:
		if err := c.cli.SecretRemove(ctx, op.Ref); err != nil {
			if isNotFound(err) {
				return op.Size, nil
			}
			return 0, fmt.Errorf("删除 swarm 密文失败: %w", err)
		}
		return op.Size, nil

	case DelBuildCache:
		// 整份清扫：Docker 只在一次请求里给出回收字节数，故这里是九类里唯一「不认 Ref」的。
		// 空缓存重复清返回空报告，不报错——幂等。
		rep, err := c.cli.BuildCachePrune(ctx, build.CachePruneOptions{All: true})
		if err != nil {
			return 0, fmt.Errorf("清空构建缓存失败: %w", err)
		}
		if rep == nil {
			return 0, nil
		}
		return int64(rep.SpaceReclaimed), nil

	default:
		// 认不出的类型一律不猜：宁可让这一项失败并说清原因，也不要拿别的对象顶上去。
		return 0, fmt.Errorf("不认识要删的 Docker 对象类型: %s", op.Kind)
	}
}
