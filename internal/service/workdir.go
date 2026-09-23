// prepareService：安装前把 bind 挂载所需的宿主工作目录与默认配置落盘到 PHPO_HOME，
// 使 MOUNTS 表解析出的每个挂载源在 Docker 建容器前即存在且类型正确（目录/文件），
// 兑现「容器创建即可起」并兑现 §0.2 规则 19 / §5.13 清洁性（幂等：仅在缺失时写入，重装不覆盖用户改动；
// 唯一例外是旧版 postgresql.conf 的日志段按原文精确匹配后就地修复，见 healPgLogging）。
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/template"
	"phpo/internal/util"
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
		if err := util.MkdirAll(filepath.Join(root, sub)); err != nil {
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
		if err := util.MkdirAll(target); err != nil {
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
		if err := util.MkdirAll(filepath.Dir(host)); err != nil {
			return err
		}
		if err := util.WriteFile(host, []byte(f.Content)); err != nil {
			return fmt.Errorf("写入配置失败 %s: %w", host, err)
		}
		logf(log, model.LogOk, "写入默认配置 "+host)
	}
	if kept > 0 {
		logf(log, model.LogDim, fmt.Sprintf("保留既有配置 %d 个（重装不覆盖用户改动）", kept))
	}
	healPgLogging(env, kind, version, log)
	return nil
}

// 旧版默认 postgresql.conf 的日志三行：logging_collector 要往宿主 bind 挂进来的 ./pgsql/{ver}/logs
// 建文件，而那目录由宿主用户创建（当时 0755）、容器内 postgres 是另一个 uid，建文件即 Permission denied →
// postgres FATAL 退出 → unless-stopped 无限重启，服务永远启不来（真机取证退出码 1、重启 40 次）。
const (
	oldPgLogBlock = "logging_collector = on\nlog_directory = '/var/log/postgresql'\nlog_filename = 'postgresql-%Y-%m-%d.log'\n"
	newPgLogBlock = "log_destination = 'stderr'\nlogging_collector = off\n"
)

// healPgLogging 就地把磁盘上仍是旧默认写法的 postgresql.conf 换成「日志走 stderr」。
// 必须在「启用」路径上也跑一次：prepareService 只在装/重建时执行，且对已存在的配置一律保留不覆盖，
// 于是旧装机的坏配置永远等不到被换掉——除非卸载重装。写入用 WriteFile 原地截断：postgresql.conf 是
// 单文件 bind，换成新 inode（写临时文件再 rename）容器读到的仍是旧那份。
func healPgLogging(env config.Env, kind model.ServiceKind, version string, log task.StepLog) {
	if kind != model.KindPgsql {
		return
	}
	path := filepath.Join(env.RootFor(string(kind), version), "conf", "postgresql.conf")
	raw, err := os.ReadFile(path)
	if err != nil {
		return // 尚未落盘：新装由模板直接给出正确内容
	}
	s := string(raw)
	fixed := strings.Replace(s, oldPgLogBlock, newPgLogBlock, 1)
	if fixed == s {
		if strings.Contains(s, "logging_collector = on") {
			logf(log, model.LogDim, "postgresql.conf 里 logging_collector 已被改成非默认写法，不自动改写；容器反复重启时请自行核对该项")
		}
		return
	}
	if err := util.WriteFile(path, []byte(fixed)); err != nil {
		logf(log, model.LogErr, fmt.Sprintf("修复 %s 失败: %v", path, err))
		return
	}
	logf(log, model.LogOk, "已把 "+path+" 的日志改回 stderr（旧写法往宿主 logs 目录建文件，容器内无权限即崩溃循环）")
}

// logf 向步骤日志写一行；未注入 logger（只读校验路径）时静默
func logf(log task.StepLog, level model.LogLevel, text string) {
	if log != nil {
		log.Log(string(level), text)
	}
}
