//go:build !unix

// 这是干什么的：给非类 Unix 平台（此刻就是 Windows）补一个「这份 pid 文件里的进程还活着吗」的答复。
//
// 为什么必须有这一格：探进程号用的 `kill(pid, 0)` 只在类 Unix 内核有，Windows 的 Go 标准库里
// 没有这个调用——v0.1.115 首次 Windows 打包就是被它挡住的（判据本体在 `clean_pid_unix.go`）。
//
// 这一侧一律答「问不出结果」（交 0 = 不拦），不是「确认它死了」：Windows / macOS 上的宿主深扫，
// Linux 专属那一片运行时目录（`/run/user/<uid>/…`）本来就被标成「这台机器够不着，那份数据在容器
// 引擎的虚拟机磁盘里」，那些行不是可删状态，走不到删除这一步；真到了删除，拦着的是挂载点与
// 套接字那两判，以及「读不动一律算活的」那条兜底。
package engine

// livePidIn 在 Windows 上问不出进程号，一律交 0。
func livePidIn(string) int { return 0 }
