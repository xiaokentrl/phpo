// 端口实探测试：占用后探测必须失败、释放后必须成功
package port

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestProbeOccupiedThenFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("环境无法监听本地端口")
	}
	p, _ := strconv.Atoi(strings.Split(ln.Addr().String(), ":")[1])
	if Probe(p) == nil {
		t.Errorf("端口 %d 被监听，Probe 应报告占用", p)
	}
	ln.Close()
	if !Available(p) {
		t.Errorf("端口 %d 释放后应可用", p)
	}
}
