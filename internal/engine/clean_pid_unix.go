//go:build unix

// 这是干什么的：认「用户运行时目录里那份 pid 文件记着的进程还活着」，让清理面板删引擎残骸时
// 不把手上正在用的东西删掉。
//
// 为什么单独一个文件：探进程号用的 `kill(pid, 0)` 只有类 Unix 内核有（Linux、macOS），
// Windows 的 Go 标准库里没有这个调用——v0.1.115 首次 Windows 打包就是被它挡住的。
// 判据本身（文件名像不像装进程号的、号怎么读）说在一处，探法收在 `pidAlive`，
// 另一侧只有 `clean_pid_other.go` 那个「问不出结果」的实现。
package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// pidFileLike 认「这个文件名像是装进程号的」：运行时目录底下就是这几类
// （`conmon.pid`、`pidfile`、`pause.pid`）。名字不像的不去看内容，免得把普通文件当探针读。
func pidFileLike(name string) bool {
	return name == "pidfile" || strings.HasSuffix(name, ".pid") || strings.HasSuffix(name, ".pidfile")
}

// livePidIn 读一个 pid 文件，那个号还活着就交出号，否则交 0（= 不拦）。
//
// 为什么只认 pid 文件名：正在跑的容器在盘上留下的**唯一**通用痕迹就是这一份进程号——
// 它的挂载点在容器自己的挂载命名空间里，宿主这边看不到（实测：容器在跑时 `/run/user/<uid>/containers`
// 底下一条挂载都没有）。只看套接字会把这些目录当成残骸放行。
//
// 号读不出来、文件读不动、号 ≤ 1 都交 0——空头的 pid 文件是容器退出后留下的残骸，本来就该删。
func livePidIn(path string) int {
	if !pidFileLike(filepath.Base(path)) {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0]))
	if err != nil || pid <= 1 {
		return 0
	}
	if pidAlive(pid) {
		return pid
	}
	return 0
}

// pidAlive 问内核这个号现在还在不在。
//
// EPERM 也算活着：kill(pid, 0) 对别人家的进程回 EPERM，那正好证明进程在世。
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
