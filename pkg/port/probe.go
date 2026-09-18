// 端口实探：本机 TCP 绑定探测（逻辑占用另由 collectUsedPorts 提供，二者在 preflight 汇合）
package port

import (
	"fmt"
	"net"
)

// Probe 返回 nil 表示本机可绑定（空闲）；被占用返回错误
func Probe(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return ln.Close()
}

// Available 便捷判定
func Available(port int) bool { return Probe(port) == nil }
