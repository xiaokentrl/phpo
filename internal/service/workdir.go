// prepareService：安装前把 bind 挂载所需的宿主工作目录与默认配置落盘到 PHPO_HOME，
// 使 MOUNTS 表解析出的每个挂载源在 Docker 建容器前即存在且类型正确（目录/文件），
// 兑现「容器创建即可起」并兑现 §0.2 规则 19 / §5.13 清洁性（幂等：仅在缺失时写入，重装不覆盖用户改动）。
package service

import (
	"fmt"
	"os"
	"path/filepath"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/template"
)

// prepareService 建目录 + 渲染默认配置；kind 无模板（如未支持种类）时仅建目录树。
// log 逐行吐真实落盘位置与新建/保留的配置，用户才能分辨「装到哪了、是否覆盖了我的改动」（可为 nil）。
func prepareService(env config.Env, kind model.ServiceKind, version string, log task.StepLog) error {
	root := env.RootFor(string(kind), version)
	logf(log, model.LogDim, "工作目录: "+root)
	// 版本子目录树（conf/logs/data/…，§5.13.2 隔离于 phpo 命名空间）
	for _, sub := range config.VersionSubdirs[string(kind)] {
		// 数据目录已自定义（需求 7）：默认的 {KIND_ROOT}/{version}/data 不再凭空建出，避免两处数据目录并存
		if sub == "data" && env.HasCustomDataDir(string(kind), version) {
			continue
		}
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return fmt.Errorf("创建目录失败 %s/%s: %w", root, sub, err)
		}
	}
	// 遍历挂载表：目录型挂载确保宿主目录存在；文件型挂载确保其父目录存在（文件本体由模板渲染）
	for _, m := range env.ResolveMounts(string(kind), version) {
		if m.Host == "" {
			continue
		}
		target := m.Host
		if m.From != "" {
			target = filepath.Dir(m.Host)
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("准备挂载目录失败 %s: %w", target, err)
		}
	}
	// 渲染默认配置：仅当宿主文件缺失时写入（重装保留用户既有编辑）
	files, err := template.FilesFor(string(kind), version)
	if err != nil {
		return err
	}
	kept := 0
	for _, f := range files {
		host := filepath.Join(env.PHPOHome, filepath.FromSlash(f.Path))
		if _, err := os.Stat(host); err == nil {
			kept++
			continue // 已存在，不覆盖
		}
		if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(host, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("写入配置失败 %s: %w", host, err)
		}
		logf(log, model.LogOk, "写入默认配置 "+host)
	}
	if kept > 0 {
		logf(log, model.LogDim, fmt.Sprintf("保留既有配置 %d 个（重装不覆盖用户改动）", kept))
	}
	return nil
}

// logf 向步骤日志写一行；未注入 logger（只读校验路径）时静默
func logf(log task.StepLog, level model.LogLevel, text string) {
	if log != nil {
		log.Log(string(level), text)
	}
}
