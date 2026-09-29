// Docker 全量资源清理 · 删除层的宿主文件那一半：把用户勾中的「宿主磁盘上的东西」删掉。
//
// 这层为什么必须和 clean_delete.go 分开写，而不是往那个 switch 里再加几个 case：
//
//  1. 走 Docker SDK 的那九类（容器、镜像、卷、网络…）删的是**引擎里的记录**，一次调用即成；
//     宿主这一侧删的是**文件与内核对象**——`rm -rf` 一份目录、`ip link delete` 一张网卡、
//     `rmdir` 一个 cgroup、`gpasswd -d` 把一个用户移出组。这些活儿 SDK 一件都办不了。
//  2. 宿主这一侧的**安全门完全不同**。SDK 删错了一颗镜像，最坏是这台机器上少一个镜像；
//     这里删错了一个路径，最坏是整台机器起不来。所以每一条进来的指令都要先过一道
//     「这是不是一个允许删的位置」的判定（checkHostPath），过不了就**只让这一项失败**，
//     不影响同一单里其余已经过关的项（需求 ㉘）。
//  3. 有些东西属主是 root，phpo 以自己那个身份动不了（`/var/lib/docker` 下面大半如此）。
//     这类要么提权、要么失败，不能假装成功——所以提权是**逐条决定**的，由服务层按
//     「这一行属不属于要授权的那一档」把 NeedsRoot 传进来。
//
// 删除的循环本身不在这里重写：复用 clean_delete.go 的 runDeletes，于是「一颗失败不停整单」
// 「每完成一项回调一次」两条行为在两半之间是同一份实现，不会各说一套。
package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 宿主侧要删的对象类型。值只在本层与服务层之间流动，不进快照、不落库。
//
// 分成五种而不是一种「删路径」，是因为它们的**恢复难度**和**失败原因**根本不同：
// 删文件是删文件；删网卡会让容器起不来；删 cgroup 目录在有进程占用时必然失败（那是正常保护，
// 不是 bug，所以要单独一句话说明，不能混进「删除失败」里让用户以为出了事）。
const (
	// DelHostPath：宿主上的一份文件或目录（容器检查点、卷目录、日志、构建缓存目录…）。
	DelHostPath = "host_path"
	// DelHostLink：一张网络接口（veth / CNI 留下的网卡）。`ip link delete <名字>`。
	DelHostLink = "host_netdev"
	// DelHostNetns：一个网络命名空间。`ip netns delete <名字>`。
	DelHostNetns = "host_netns"
	// DelHostCgroup：容器留下的 cgroup 目录。**不递归**删——里面有进程时 rmdir 必失败，
	// 这个失败恰恰是「还在用，别删」的正确回答。
	DelHostCgroup = "host_cgroup"
	// DelHostGroup：把一个用户移出 docker 组。`gpasswd -d <用户> docker`。
	DelHostGroup = "host_group_user"
)

// runLocal 在本机以**当前用户身份**跑一条命令。
//
// 两个不变的做法：argv 直接传、**不经 shell**（拼 shell 字符串等于把用户勾出来的名字交给
// 一个解释器去二次理解，那是注入的入口）；用 LookPath 拿绝对路径，这样命令名永远排在
// 参数位之前，一个以 `-` 开头的名字不可能被当成参数。
//
// 这里 stdout **不是数据**，是给人看的那句原因，所以两路合起来收（CombinedOutput）——
// 与提权读取那份（elevateFind 用 Output）正好相反：那边混进 stderr 会把一行解析成假条目，
// 这边失败信息少了半句用户就不知道该做什么（需求 ㉘）。
var runLocal = func(ctx context.Context, argv []string) (string, error) {
	exe, err := exec.LookPath(argv[0])
	if err != nil {
		return "", fmt.Errorf("本机没有 %s 命令：%w", argv[0], err)
	}
	cmd := exec.CommandContext(ctx, exe, argv[1:]...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runDeleteCmd 是删除侧唯一的命令出口，也是单测唯一的注入点。
//
// 提权不在这层猜：服务层看过每一行的档位与这台机器的 Docker 是不是 rootless，
// 决定这条要不要授权（elevated）。这样一次「彻底清空」里不会有一半本来能删的东西
// 先白失败一次再去提权，也不会在 rootless 的机器上弹一个没用的授权框。
//
// pkexec 只认绝对路径，所以提权分支要先把命令名找成绝对路径再交给它——
// 与 elevateFind 里那份 findPath 同一个道理。
var runDeleteCmd = func(ctx context.Context, elevated bool, name string, args ...string) (string, error) {
	if !elevated {
		return runLocal(ctx, append([]string{name}, args...))
	}
	exe, err := pkexecPath()
	if err != nil {
		return "", fmt.Errorf("系统无 pkexec（polkit），无法自动提权删除: %w", err)
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("本机没有 %s 命令，无法提权删除: %w", name, err)
	}
	return runLocal(ctx, append([]string{exe, bin}, args...))
}

// catastrophicRoots 是「无论如何不许整份删掉」的那一圈目录。
//
// 比对只用**完全相等**，不用前缀：`/etc/cni/net.d/00-...conflist` 是 network.cni 行里
// 货真价实的一条（需求 ⑫ 给了删除按钮），它就在 `/etc` 底下——按前缀判等于把这一行的
// 按钮永远锁死。而「删掉整个根」这件事由 checkHostPath 里另一道判定挡住（条目必须
// 真的落在某个根目录**之下**，不能就等于根本身），两道各管一件事，都在这里说清了。
var catastrophicRoots = map[string]bool{
	"/": true, "/bin": true, "/boot": true, "/dev": true, "/etc": true, "/home": true,
	"/lib": true, "/lib32": true, "/lib64": true, "/libx32": true, "/media": true,
	"/mnt": true, "/opt": true, "/proc": true, "/root": true, "/run": true,
	"/sbin": true, "/srv": true, "/sys": true, "/tmp": true, "/usr": true, "/var": true,
}

// nameSafe 判定一个名字能不能安全地放进命令行参数位。
//
// 为什么这里要比文件路径更严：`ip` 与 `gpasswd` **没有** `--` 这颗分隔符，
// 一个以 `-` 开头的名字会被它当成自己的选项（`ip link delete -f` 之类），
// 那就不是「删错一样东西」而是「多做了别的事」。因此空、以 - 开头、含路径分隔符或
// 空字节的，一律拒。
func nameSafe(name string) error {
	switch {
	case name == "":
		return errors.New("名字是空的")
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("名字 %q 以 - 开头，会被命令当成选项，拒绝执行", name)
	case strings.ContainsAny(name, `/\\`):
		return fmt.Errorf("名字 %q 含路径分隔符，不是一个对象名", name)
	case strings.ContainsRune(name, 0):
		return fmt.Errorf("名字 %q 含空字节", name)
	}
	return nil
}

// hostRowRoots 回答「这一行的东西落在哪些目录下」。
//
// 它是 hostRowSources 的亲兄弟，但**不能复用那张表**：那张表是在没有 DaemonInfo 的时候建的，
// 里面 rootDep 那几条的根是空串，只能回答「行 → 统计段」；删除要知道的是真路径，
// 因此必须拿**这一次扫描用的那份 DaemonInfo** 重新算。两处共用同一个 hostSources，
// 所以采集改了落点、删除这侧不可能还按老路径去找。
func hostRowRoots(row string, info DaemonInfo) []string {
	var out []string
	for _, s := range hostSources(info) {
		for _, k := range s.rows {
			if k == row {
				out = append(out, s.roots...)
				break
			}
		}
	}
	return dedupe(out)
}

// resolveHostPath 把采集层给出的那条引用变成「一个绝对路径」。
//
// 为什么要分两种形状：采集层往外给的东西本来就不齐（见 finishVolumes / finishIfaces）——
// 有的行给绝对路径（netns、cgroup、日志），有的行只给一个**裸名字**（volume.driver 给卷名、
// network.bridge 给网卡名），因为它数的时候就是在那个目录下数目录项的。裸名字只在该行
// **恰好只有一个根**时才落得出唯一路径；两个根（比如宿主日志同时看 /var/log 与 journal）
// 就宁缺毋滥——猜错根等于删到别的地方去。
func resolveHostPath(row, ref string, roots []string) (string, error) {
	if filepath.IsAbs(ref) {
		return ref, nil
	}
	if len(roots) != 1 || roots[0] == "" {
		return "", fmt.Errorf("%s 这一行的条目 %q 只是个名字，落不出唯一路径（这一行有 %d 个候选目录）", row, ref, len(roots))
	}
	if ref == ".." || strings.ContainsAny(ref, `/\\`) || strings.ContainsRune(ref, 0) {
		return "", fmt.Errorf("%s 这一行的条目名 %q 不是一个安全的文件/目录名", row, ref)
	}
	return filepath.Join(roots[0], ref), nil
}

// checkHostPath 是宿主删除的安全门（硬红线 3）。
//
// 五道判定，从「形状对不对」一路收到「位置准不准」：
// 非空 → 无空字节 → 必须是绝对路径 → Clean 之后不动点（说明它已经归一，没有藏 `..`）
// → 不等于任何一圈灾难根 → **必须真的落在这一行自己的某个根目录之下，且不等于那个根本身**。
// 最后一道是最要紧的：它把 `/var/lib/docker` 整份、`/run/docker` 整份、`/var/log` 整份
// 这类「扫出来确实会有一条叫这个」的情况挡在外面，同时不挡 `/etc/cni/net.d/00-x.conflist`
// 这种合法条目。分类用的 matchRoot 与提权读取那边是同一个函数，两路口径不会漂。
//
// 这道门是**纵深防御**：界面上勾出去的每一条 ID 都由服务层先比对过那份当场扫出来的清单，
// 到这儿不该再有东西被拦；被拦即说明上面漏了，宁可这一项失败。
func checkHostPath(p string, roots []string) error {
	switch {
	case p == "":
		return errors.New("路径是空的")
	case strings.ContainsRune(p, 0):
		return errors.New("路径含空字节")
	case !filepath.IsAbs(p):
		return fmt.Errorf("路径 %q 不是绝对路径", p)
	case filepath.Clean(p) != p:
		return fmt.Errorf("路径 %q 未归一（可能含 .. 或多余分隔符）", p)
	case catastrophicRoots[p]:
		return fmt.Errorf("拒绝删除系统关键目录 %q", p)
	}
	rel, ok := matchRoot(p, roots)
	if !ok || rel == "." {
		return fmt.Errorf("路径 %q 不在这一行扫描过的目录之下，或就是那个目录本身，拒绝删除", p)
	}
	return nil
}

// linkNameOf 把采集层给的一条网卡/命名空间引用收成裸名字。
//
// 必须是 `<dir>/<一个名字>` 这个形状：`/sys/class/net` 下面是扁平的（内核不会在里面再开子目录），
// 出现第二段就说明这不是一个接口名。收出来的名字再过一遍 nameSafe。
func linkNameOf(ref, dir, row string) (string, error) {
	if !filepath.IsAbs(ref) {
		return "", fmt.Errorf("%s 的条目 %q 不是绝对路径", row, ref)
	}
	rel, ok := matchRoot(ref, []string{dir})
	if !ok || rel == "." || strings.Contains(rel, "/") {
		return "", fmt.Errorf("%s 的条目 %q 不在 %s 之下，不是一个接口名", row, ref, dir)
	}
	name := filepath.Base(ref)
	if err := nameSafe(name); err != nil {
		return "", fmt.Errorf("%s 的条目名不安全: %w", row, err)
	}
	return name, nil
}

// hostKindOf 给一条**没过分类**的指令打个类型标签。
//
// 为什么要打：runDeletes 的回执是从调用方那份 op 上取 Kind 的，分类失败的条目 Kind 还是空的，
// 界面就会拿到一行「不知道是什么东西删失败了」。按行名推一个最接近的类型填上，
// 失败信息至少能落到对的行上。
func hostKindOf(row string) string {
	switch row {
	case rowNetworkVeth:
		return DelHostLink
	case rowNetworkNetns:
		return DelHostNetns
	case rowContainerCgroup:
		return DelHostCgroup
	case rowSystemGroup:
		return DelHostGroup
	default:
		return DelHostPath
	}
}

// classifyHostOp 把「一行 + 一条引用」翻成「一种删法 + 一个安全参数」。
//
// 这是宿主删除的**唯一一道分类门**：服务层传来的 Kind 不作数，一律以这里现算的为准——
// 免得外面传个 `host_path` 就把一张网卡当目录 `rm` 掉。分类失败即这一项失败（原因带在错误里），
// 同单其余项照删（需求 ㉘）。
//
// network.cni 这一行有两种形状是刻意的：CNI 插件留下的东西既可能是**网卡**（minikube /
// kind / calico 建的），也可能是 `/etc/cni/net.d` 下面的一份**配置**。先按网卡认，
// 认不出再按配置路径走——反过来的话，`rm -rf /sys/class/net/x` 既删不掉那张 veth
// （那是个内核对象的符号链接，删了只会留下一条看不懂的报错），又把「其实是配置」那种情况堵死了。
func classifyHostOp(op DeleteOp, info DaemonInfo) (DeleteOp, error) {
	switch op.Row {
	case rowNetworkVeth:
		name, err := linkNameOf(op.Ref, dirSysNet, op.Row)
		if err != nil {
			return DeleteOp{}, err
		}
		op.Kind, op.Ref = DelHostLink, name
		return op, nil

	case rowNetworkNetns:
		name, err := linkNameOf(op.Ref, dirNetns, op.Row)
		if err != nil {
			return DeleteOp{}, err
		}
		op.Kind, op.Ref = DelHostNetns, name
		return op, nil

	case rowNetworkCni:
		if name, err := linkNameOf(op.Ref, dirSysNet, op.Row); err == nil {
			op.Kind, op.Ref = DelHostLink, name
			return op, nil
		}
		return hostPathTarget(op, info, DelHostPath)

	case rowContainerCgroup:
		return hostPathTarget(op, info, DelHostCgroup)

	case rowSystemGroup:
		// 这一行数的是 /etc/group 里的组成员，压根没有目录（hostRowRoots 为空），
		// 所以不走路径门，只判名字——注意删的是「把这个人移出 docker 组」，不是删这个用户。
		if err := nameSafe(op.Ref); err != nil {
			return DeleteOp{}, fmt.Errorf("%s 的条目不安全: %w", op.Row, err)
		}
		op.Kind = DelHostGroup
		return op, nil

	default:
		return hostPathTarget(op, info, DelHostPath)
	}
}

// hostPathTarget 是「这一条要走路径门」那几类的共同收尾：先落出绝对路径，再过安全门。
func hostPathTarget(op DeleteOp, info DaemonInfo, kind string) (DeleteOp, error) {
	roots := hostRowRoots(op.Row, info)
	p, err := resolveHostPath(op.Row, op.Ref, roots)
	if err != nil {
		return DeleteOp{}, err
	}
	if err := checkHostPath(p, roots); err != nil {
		return DeleteOp{}, err
	}
	op.Kind, op.Ref = kind, p
	return op, nil
}

// DeleteHostObjects 逐项删除宿主上的文件与内核对象，回执顺序与 ops 一致。
//
// 与 Docker 那一半共用 runDeletes，所以「单项失败不停整单」「每项完成回调一次」两条一字不差。
//
// 分类为什么要**先全部做完再开始删**：这样一条不合法的指令会在第一个字节被动之前就被拒掉，
// 而不是删到一半发现后面那颗不该删、于是那一次「彻底清空」留下一个说不清的状态。
// 分类失败的条目仍然进循环，带着自己的原因失败——用户在界面上看得见是哪一行、为什么（需求 ㉘）。
func (c *Client) DeleteHostObjects(ctx context.Context, ops []DeleteOp, info DaemonInfo, tr *Trash, onItem ItemFn) []DeleteResult {
	prepared := make([]DeleteOp, len(ops))
	for i, op := range ops {
		k, err := classifyHostOp(op, info)
		if err != nil {
			prepared[i] = DeleteOp{
				Row: op.Row, Kind: hostKindOf(op.Row), Ref: op.Ref, Size: op.Size,
				NeedsRoot: op.NeedsRoot, TrashIt: op.TrashIt,
			}
			continue
		}
		prepared[i] = k
	}
	return runDeletes(ctx, prepared, onItem, func(ctx context.Context, op DeleteOp) (int64, string, error) {
		return c.deleteHostOne(ctx, op, info, tr)
	})
}

// deleteHostOne 删一条已经（希望）过了门的指令。
//
// 进来先重新过一遍分类门：DeleteHostObjects 正常都会先跑，但这个方法也可能被单测或以后
// 别的路径直接调到，安全判定不能指望调用方守规矩。
//
// 「先进回收站」只对有数据的那几项开（需求 ④/⑳），且只有真文件目录进得了回收站：
// 一张网卡、一个 cgroup 目录没有内容可留，硬塞进回收站等于造一个假的恢复入口。
func (c *Client) deleteHostOne(ctx context.Context, op DeleteOp, info DaemonInfo, tr *Trash) (int64, string, error) {
	k, err := classifyHostOp(op, info)
	if err != nil {
		return 0, "", err
	}
	op = k

	if op.TrashIt {
		if op.Kind != DelHostPath {
			return 0, "", fmt.Errorf("%s 这一项不是宿主上的文件，进不了回收站，只能直接删", op.Row)
		}
		if tr == nil {
			// 宁可这一项不删，也不能「删了但没留底」——绕过 7 天保留等于把回收站那句话变成假话。
			return 0, "", fmt.Errorf("回收站还没准备好，这一项不能删（直接删等于绕过 7 天保留）")
		}
		dest, err := moveForTrash(ctx, op, tr)
		if err != nil {
			return 0, "", err
		}
		// 进回收站**不腾出磁盘**：东西还在盘上，只是挪了个位置。
		// 因此这里 Freed 给 0，回收站路径另记账（界面上那句「7 天内可恢复」用的是它）。
		return 0, dest, nil
	}
	freed, err := deleteHostRef(ctx, op)
	if err != nil {
		return 0, "", err
	}
	return freed, "", nil
}

// moveForTrash 把一份目录挪进回收站，按属主决定要不要授权。
//
// 提权那一路的代价必须让用户知道：挪进去的东西属主仍是 root，phpo 自己既读不动也删不动，
// 所以「从回收站恢复」和「到期自动清」这两步到时候还得再授权一次（trash.go 的 MoveElevated 注释里
// 已经写清），不是一句「已放入回收站」就从此不用管了。
func moveForTrash(ctx context.Context, op DeleteOp, tr *Trash) (string, error) {
	if op.NeedsRoot {
		return tr.MoveElevated(ctx, op.Ref)
	}
	return tr.Move(op.Ref)
}

// deleteHostRef 是真正动手的那一步，按类型分五种删法。
//
// 每一种都带一句「失败时用户看得见什么」：路径删不掉是权限或还在用；cgroup 删不掉几乎总是
// 里面还有进程（这是保护，不是故障，就得说清）；网卡与命名空间删不掉通常是还挂在容器上。
func deleteHostRef(ctx context.Context, op DeleteOp) (int64, error) {
	switch op.Kind {
	case DelHostPath:
		if op.NeedsRoot {
			if _, err := runDeleteCmd(ctx, true, "rm", "-rf", "--", op.Ref); err != nil {
				return 0, fmt.Errorf("提权删除 %s 失败: %w", op.Ref, err)
			}
			return op.Size, nil
		}
		if err := os.RemoveAll(op.Ref); err != nil {
			return 0, fmt.Errorf("删除 %s 失败: %w", op.Ref, err)
		}
		return op.Size, nil

	case DelHostCgroup:
		// 不递归：能删掉说明这个 cgroup 真的空了。删不掉就是里面还有进程，
		// 那种时候「帮你强删」等于把正在跑的容器踢出资源组，所以失败即如实报失败。
		var err error
		if op.NeedsRoot {
			_, err = runDeleteCmd(ctx, true, "rmdir", "--", op.Ref)
		} else {
			err = os.Remove(op.Ref)
			if err != nil && errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return 0, fmt.Errorf("删不掉 %s（通常说明这个 cgroup 里还有进程在跑）: %w", op.Ref, err)
		}
		return op.Size, nil

	case DelHostLink:
		if ok, _ := hostGone(filepath.Join(dirSysNet, op.Ref)); ok {
			return op.Size, nil
		}
		if _, err := runDeleteCmd(ctx, op.NeedsRoot, "ip", "link", "delete", op.Ref); err != nil {
			return 0, fmt.Errorf("删除网络接口 %s 失败（多半它还挂在某个容器上）: %w", op.Ref, err)
		}
		return op.Size, nil

	case DelHostNetns:
		if ok, _ := hostGone(filepath.Join(dirNetns, op.Ref)); ok {
			return op.Size, nil
		}
		if _, err := runDeleteCmd(ctx, op.NeedsRoot, "ip", "netns", "delete", op.Ref); err != nil {
			return 0, fmt.Errorf("删除网络命名空间 %s 失败: %w", op.Ref, err)
		}
		return op.Size, nil

	case DelHostGroup:
		// 先问一句在不在组里：不在就是已经删好了，直接算成功，
		// 不去跑 gpasswd——那会在用户眼前白弹一次授权框。
		has, err := dockerGroupHas(op.Ref)
		if err != nil {
			return 0, err
		}
		if !has {
			return 0, nil
		}
		if _, err := runDeleteCmd(ctx, op.NeedsRoot, "gpasswd", "-d", op.Ref, "docker"); err != nil {
			return 0, fmt.Errorf("把用户 %s 移出 docker 组失败: %w", op.Ref, err)
		}
		return 0, nil

	default:
		return 0, fmt.Errorf("不认识要删的宿主对象类型: %s", op.Kind)
	}
}

// hostGone 判断一个路径还在不在，只用来做「已经不在了即幂等成功」。
//
// 判得动的是这两处：`/sys/class/net` 与 `/var/run/netns` 都是谁都能读的目录，
// 所以这一道不花授权。返回 false 时可能是「还在」也可能是「读不动」——读不动就照常
// 往下走真正的删除命令，让那条命令给出准确的原因，不在这里替它编一个。
func hostGone(p string) (bool, error) {
	_, err := os.Lstat(p)
	if err == nil {
		return false, nil
	}
	return errors.Is(err, fs.ErrNotExist), err
}

// dockerGroupHas 回答「这个人现在还在不在 docker 组里」。
//
// 读 /etc/group 不需要授权（clean_hostfs.go 里统计这一行用的 probeGroup 就是普通 os.ReadFile，
// 那个 `"docker:"` 前缀判定是全仓唯一一处组名的写法，这里保持同一口径而不另立常量）。
// 没有 docker 这一行 → 这台机器上没有这个组 → 返回 false，即「不用删，已经是这样了」。
func dockerGroupHas(user string) (bool, error) {
	b, err := os.ReadFile(dirEtcGroup)
	if err != nil {
		return false, fmt.Errorf("读 %s 失败，无法确认用户 %s 在不在 docker 组里: %w", dirEtcGroup, user, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "docker:") {
			continue
		}
		field := strings.Split(line, ":")
		if len(field) < 4 {
			return false, nil
		}
		for _, u := range strings.Split(field[3], ",") {
			if strings.TrimSpace(u) == user {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}
