// 端口实探验收：InUse 只把「确实被占」判成占用，权限类失败不得当占用（否则探针无权判定会误拦重建）
package port

import (
	"net"
	"syscall"
	"testing"
)

func TestProbeReportsInUseOnBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p := ln.Addr().(*net.TCPAddr).Port
	if err := Probe(p); !InUse(err) {
		t.Fatalf("被占端口应判为 InUse，端口 %d 实得 %v", p, err)
	}
}

// TestInUseIgnoresPermissionFailure 非 root 绑 <1024 端口时 net.Listen 返回 *net.OpError{Err: EACCES}
// （Linux/macOS 常态）：这不是占用。判成占用就把「探测能力不足」变成了阻断，会误杀正在运行的 nginx。
func TestInUseIgnoresPermissionFailure(t *testing.T) {
	if InUse(nil) {
		t.Fatal("空闲（无错误）不得判为占用")
	}
	if InUse(&net.OpError{Op: "listen", Net: "tcp", Err: syscall.EACCES}) {
		t.Fatal("权限错误不得判为占用")
	}
}
