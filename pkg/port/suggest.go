// validatePort：格式 → 范围 → 排除 → 占用；占用处置有三档（§5.8）
// 新建站点端口占用 → 保留用户所填端口只标占用（上层告警 + 站点降级）；改站点端口占用 → 顺延首个可用（无窗口上限、不报错）；
// 服务端口占用 → 直接报 portInUse（用户可显式指定任意端口）
package port

import (
	"fmt"
	"strconv"
	"strings"

	"phpo/pkg/errs"
)

// 端口范围常量（§0.3：可用端口 1–65535）
const (
	DefaultPort  = 80 // 站点默认端口
	MinSupported = 1
	MaxSupported = 65535
)

type Options struct {
	// Exclude：视为空闲的端口（如正在编辑的站点自身端口）
	Exclude []int
	// AutoAdvance：true 时占用则顺延首个可用（改已有站点的端口专用）
	AutoAdvance bool
	// KeepOnConflict：true 时占用**不改端口**，仅置 Result.Occupied 交上层告警降级（新建站点专用，§5.8）
	KeepOnConflict bool
}

type Result struct {
	Ok       bool
	Value    int
	Adjusted bool
	Occupied bool // 端口已被占用但按 KeepOnConflict 原样保留
	Original int
	Msg      string
}

// Validate 输入原始字符串端口值 + 逻辑占用表（调用方完成 collectUsedPorts）
func Validate(raw string, used Used, opts Options) Result {
	s := strings.TrimSpace(raw)
	if !isDigits(s) || len(s) > 5 {
		return Result{Msg: errs.PortInvalid}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < MinSupported || n > MaxSupported {
		return Result{Msg: errs.PortInvalid}
	}
	for _, e := range opts.Exclude {
		if e == n {
			return Result{Ok: true, Value: n}
		}
	}
	if _, ok := used[n]; !ok {
		return Result{Ok: true, Value: n}
	}
	// 新建站点端口占用：端口原样保留，只回传占用信息，由上层弹框告警 + 站点降级（§5.8，不顺延不阻断）
	if opts.KeepOnConflict {
		return Result{Ok: true, Value: n, Occupied: true, Msg: fmt.Sprintf("%s: %d (%s)", errs.PortInUse, n, used[n])}
	}
	// 改站点端口占用：在 [1,65535] 内顺延首个可用（向上优先，再回绕）
	if opts.AutoAdvance {
		if next := FindNextAvailable(n, MaxSupported, MinSupported, used); next != nil {
			return Result{Ok: true, Value: *next, Adjusted: true, Original: n}
		}
	}
	return Result{Msg: fmt.Sprintf("%s: %d (%s)", errs.PortInUse, n, used[n])}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
