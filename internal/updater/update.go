// Updater 升级编排（§5.9 / T604 / 硬红线 6）：下载 → SHA256+Ed25519 双校验 → 备份当前 → 标记 → 安装
// 失败可回滚：安装同步失败立即恢复备份并清标记；进程在安装中被杀死则下次启动由 Rollback.RecoverOnStartup 自动回滚
// 全程以 update:progress 分阶段上报，结束以 update:done 收尾（前端唯一权威，硬红线 4）
package updater

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"phpo/internal/model"
	"phpo/internal/util"
)

// Updater 组合注入依赖；exePath/cpFile 可在单测替换以脱离真实二进制
type Updater struct {
	current  string
	src      ReleaseSource
	dl       Downloader
	pub      ed25519.PublicKey
	inst     Installer
	rb       *Rollback
	em       Emitter
	download string // 下载目录
	backups  string // 旧版备份目录
	exePath  func() (string, error)
	cpFile   func(src, dst string) error
}

// New 构造编排器；pub 取嵌入的发布公钥，dl/inst 为 nil 时回落真实实现
func New(current, downloadDir, backupsDir string, src ReleaseSource, dl Downloader, inst Installer, rb *Rollback, em Emitter) *Updater {
	if dl == nil {
		dl = HTTPDownloader{}
	}
	if inst == nil {
		inst = NewInstaller()
	}
	if em == nil {
		em = NopEmitter{}
	}
	return &Updater{
		current: current, src: src, dl: dl, pub: PublicKey(), inst: inst, rb: rb, em: em,
		download: downloadDir, backups: backupsDir,
		exePath: os.Executable, cpFile: copyFile,
	}
}

// Check 拉取发布清单判断是否有新版本（透传 Checker 语义并发 update:available）
func (u *Updater) Check(ctx context.Context) (*Release, bool, error) {
	if u.src == nil {
		return nil, false, fmt.Errorf("updater: 未配置发布源，升级检查不可用")
	}
	return NewChecker(u.current, u.src, u.em).Check(ctx)
}

// Current 返回当前应用版本
func (u *Updater) Current() string { return u.current }

// Apply 执行一次升级：无新版本直接返回当前态；否则下载→双校验→备份→安装，失败即回滚
func (u *Updater) Apply(ctx context.Context) (model.UpdateDone, error) {
	rel, newer, err := u.Check(ctx)
	if err != nil {
		u.done(model.TaskFailed, u.current)
		return model.UpdateDone{Status: model.TaskFailed, Version: u.current}, err
	}
	if !newer {
		return model.UpdateDone{Status: model.TaskSuccess, Version: u.current}, nil
	}

	pkg := filepath.Join(u.download, "phpo-"+rel.Version)
	defer os.Remove(pkg)

	// 1) 下载（stage=download）
	if err := u.downloadPkg(ctx, rel.URL, pkg); err != nil {
		return u.fail("下载失败", err, rel.Version)
	}

	// 2) 双校验（stage=verify）——SHA256 + Ed25519 缺一不可（硬红线 6）
	u.progress("verify", 0, 0)
	if err := VerifyPackage(pkg, rel.SHA256, u.pub, rel.Signature); err != nil {
		return u.fail("校验失败", err, rel.Version)
	}

	// 3) 备份当前二进制 + 写 pending 标记（下次启动确认或回滚依据）
	backup, err := u.backupCurrent()
	if err != nil {
		return u.fail("备份失败", err, rel.Version)
	}
	if err := u.rb.Begin(PendingUpdate{Target: rel.Version, Backup: backup}); err != nil {
		return u.fail("写入升级标记失败", err, rel.Version)
	}

	// 4) 安装（stage=install）
	u.progress("install", 0, 0)
	if err := u.inst.Install(ctx, pkg); err != nil {
		// 同步失败：立即恢复旧版并清标记（进程仍运行旧版，无需等下次启动）
		if rerr := u.Restore(backup); rerr != nil {
			return u.fail("安装失败且回滚失败", fmt.Errorf("%w; 回滚: %v", err, rerr), rel.Version)
		}
		_ = u.rb.Complete()
		u.done(model.TaskFailed, u.current)
		return model.UpdateDone{Status: model.TaskFailed, Version: u.current}, err
	}

	// 安装成功：保留 pending 标记，待新版本首次启动时 RecoverOnStartup 确认清除
	u.progress("install", 100, 0)
	u.done(model.TaskSuccess, rel.Version)
	return model.UpdateDone{Status: model.TaskSuccess, Version: rel.Version}, nil
}

// Restore 用备份覆盖当前运行的可执行文件（下次启动回滚与同步失败回滚共用）
func (u *Updater) Restore(backup string) error {
	if backup == "" {
		return nil
	}
	exe, err := u.exePath()
	if err != nil {
		return err
	}
	return u.cpFile(backup, exe)
}

func (u *Updater) downloadPkg(ctx context.Context, url, dst string) error {
	u.progress("download", 0, 0)
	start := time.Now()
	return u.dl.Download(ctx, url, dst, func(done, total int64) {
		pct := 0
		if total > 0 {
			pct = int(done * 100 / total)
		}
		var speed float64
		if el := time.Since(start).Seconds(); el > 0 {
			speed = float64(done) / el
		}
		u.progress("download", pct, speed)
	})
}

// backupCurrent 复制当前可执行文件到备份目录，返回备份路径
func (u *Updater) backupCurrent() (string, error) {
	exe, err := u.exePath()
	if err != nil {
		return "", err
	}
	if err := util.MkdirAll(u.backups); err != nil {
		return "", err
	}
	dst := filepath.Join(u.backups, filepath.Base(exe)+"-"+u.current)
	if err := u.cpFile(exe, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func (u *Updater) fail(label string, err error, ver string) (model.UpdateDone, error) {
	wrapped := fmt.Errorf("updater: %s: %w", label, err)
	u.em.Emit("task:log", model.TaskLogEvent{ID: "updater", Level: model.LogErr, Text: wrapped.Error()})
	u.done(model.TaskFailed, u.current)
	return model.UpdateDone{Status: model.TaskFailed, Version: u.current}, wrapped
}

func (u *Updater) progress(stage string, percent int, speed float64) {
	u.em.Emit("update:progress", model.UpdateProgress{Stage: stage, Percent: percent, Speed: speed})
}

func (u *Updater) done(status model.TaskStatus, version string) {
	u.em.Emit("update:done", model.UpdateDone{Status: status, Version: version})
}

// copyFile 全量复制（覆盖语义），供备份/恢复使用
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, util.FilePerm)
	if err != nil {
		return err
	}
	_ = out.Chmod(util.FilePerm) // 已存在的旧文件不受 OpenFile 权限位影响，显式归一
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
