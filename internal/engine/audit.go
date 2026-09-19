// 操作审计（§5.13.10）：每次写操作以 JSON Lines 追加到 operations.log，一行一条、可逐行解析。
// 文件是审计权威；SQLite operations 表（store/operation.go）仅供 UI 历史查询。二者由服务层并行写入。
package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"phpo/internal/model"
)

// Audit 绑定一个 operations.log 路径
type Audit struct {
	Path string
}

// NewAudit 指定审计日志文件绝对路径（通常 config.AuditLogPath()）
func NewAudit(path string) *Audit { return &Audit{Path: path} }

// AuditLine 把一个操作序列化为单行 JSON（不含换行符）；纯函数，供写盘与单测复用。
func AuditLine(op model.Operation) ([]byte, error) {
	b, err := json.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("审计序列化失败: %w", err)
	}
	return b, nil
}

// Log 追加一行审计记录：确保目录存在 → 以 O_APPEND 落盘。空路径视为未配置，返回错误不静默丢弃。
func (a *Audit) Log(op model.Operation) error {
	if a == nil || a.Path == "" {
		return fmt.Errorf("审计日志路径未配置")
	}
	line, err := AuditLine(op)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return fmt.Errorf("创建审计日志目录失败: %w", err)
	}
	f, err := os.OpenFile(a.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开审计日志失败: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("写审计日志失败: %w", err)
	}
	return nil
}
