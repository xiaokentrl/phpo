// hosts 管理器：读写系统 hosts 文件的 `127.0.0.1 {domain}` 条目；不可写时降级为人话警告（§5.7 / §8）
// 提权（Windows UAC / macOS osascript admin / Linux polkit）通过 elevate 注入；默认直写失败即警告，绝不阻断建站
package hosts

import (
	"errors"
	"fmt"
	"os"
	"runtime"
)

// Result 一次加/删的结果；Warning 非空表示未落盘但调用方应继续（仅提示）
type Result struct {
	Changed bool
	Warning string
}

// Elevator 以管理员权限把整份内容写入受管路径；返回 error 视为提权被拒/失败
type Elevator func(content string) error

// Manager 绑定一个 hosts 文件路径与映射 IP
type Manager struct {
	path    string
	ip      string
	elevate Elevator
}

// DefaultPath 返回当前平台的 hosts 文件路径（§8）
func DefaultPath() string {
	switch runtime.GOOS {
	case "windows":
		dir := os.Getenv("WINDIR")
		if dir == "" {
			dir = `C:\Windows`
		}
		return dir + `\System32\drivers\etc\hosts`
	default: // darwin / linux
		return "/etc/hosts"
	}
}

// New 使用平台默认 hosts 路径、127.0.0.1 映射，并挂载平台提权器（UAC / osascript / polkit）
func New() *Manager {
	path := DefaultPath()
	return &Manager{path: path, ip: "127.0.0.1", elevate: platformElevate(path)}
}

// NewAt 指定路径（测试或自定义 hosts 文件）；不挂提权器，直写失败即降级为警告
func NewAt(path string) *Manager {
	return &Manager{path: path, ip: "127.0.0.1"}
}

// Path 受管 hosts 文件路径
func (m *Manager) Path() string { return m.path }

// Has 查询 domain 是否已解析到 127.0.0.1
func (m *Manager) Has(domain string) (bool, error) {
	content, err := m.read()
	if err != nil {
		return false, err
	}
	return Has(content, m.ip, domain), nil
}

// Add 幂等追加条目；不可写且无提权时返回 Warning（不报错）
func (m *Manager) Add(domain string) (Result, error) {
	return m.mutate(domain, true)
}

// Remove 删除条目；不可写且无提权时返回 Warning（不报错）
func (m *Manager) Remove(domain string) (Result, error) {
	return m.mutate(domain, false)
}

func (m *Manager) mutate(domain string, add bool) (Result, error) {
	content, err := m.read()
	if err != nil {
		return Result{}, err
	}
	var next string
	var changed bool
	if add {
		next, changed = Add(content, m.ip, domain)
	} else {
		next, changed = Remove(content, m.ip, domain)
	}
	if !changed {
		return Result{Changed: false}, nil
	}
	if err := m.writeDirect(next); err != nil {
		if !isPermission(err) {
			return Result{}, err
		}
		// 直写被拒：有提权器则经 polkit / UAC / osascript 重写整份；被拒或无提权器降级为人话警告，
		// 不阻断调用方（§5.7 doctor 口径「无法修改 hosts。请以管理员身份运行」）
		verb := "添加"
		if !add {
			verb = "删除"
		}
		guide := fmt.Sprintf("请以管理员身份运行后手动%s：%s %s", verb, m.ip, domain)
		if m.elevate == nil {
			return Result{Warning: fmt.Sprintf("无法修改 hosts（%s）。%s", m.path, guide)}, nil
		}
		if e := m.elevate(next); e != nil {
			return Result{Warning: fmt.Sprintf("提权写入 hosts 失败（%s）：%v。%s", m.path, e, guide)}, nil
		}
	}
	return Result{Changed: true}, nil
}

func (m *Manager) read() (string, error) {
	b, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 hosts 失败: %w", err)
	}
	return string(b), nil
}

// writeDirect 以普通权限直写整份内容；被拒（权限）由 mutate 决定是否提权或降级警告
func (m *Manager) writeDirect(content string) error {
	return os.WriteFile(m.path, []byte(content), 0o644)
}

func isPermission(err error) bool {
	return errors.Is(err, os.ErrPermission) || os.IsPermission(err)
}
