// 端口实探：本机 TCP 绑定探测（逻辑占用另由 collectUsedPorts 提供，二者在 preflight 汇合）
package port

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// Probe 返回 nil 表示本机可绑定（空闲）；被占用返回错误
func Probe(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return ln.Close()
}

// InUse 判定探测错误确为「端口已被占用」。其余失败（如非 root 绑 <1024 端口的权限错误）不算占用：
// 探针无权判定不等于端口不可用，判成占用会把探测能力不足变成阻断。
func InUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }

// Available 便捷判定
func Available(port int) bool { return Probe(port) == nil }
