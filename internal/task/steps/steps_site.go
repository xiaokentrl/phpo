// 站点步骤构件（T403）：建站/删站的单文件系统动作，各自带回滚，供 SiteService 组合成 task
// steps 层不得 import service；此处只依赖 vhost / engine / config / model 等下层
package steps

import (
	"context"
	"fmt"
	"os"

	"phpo/internal/config"
	"phpo/internal/task"
	"phpo/internal/vhost"
	"phpo/internal/vhost/hosts"
)

// HostsOps 抽象系统 hosts 写入（*hosts.Manager 满足）；不可写时返回 warning 而非 error
type HostsOps interface {
	Add(domain string) (hosts.Result, error)
}

// VHosts 抽象 vhost 管理器写盘部分（*vhost.Manager 满足）
type VHosts interface {
	Save(ctx context.Context, v vhost.Validator, domain, content string) error
	DeleteFile(domain string) error
}

// TrashMover 抽象回收站（*engine.Trash 满足）
type TrashMover interface {
	Move(origPath string) (string, error)
	Restore(trashPath, origPath string) error
}

// PrepareSiteDir 创建站点根目录（宿主路径，~ 展开）；已存在则跳过（幂等）。Rollback 仅删本次新建的空目录。
type PrepareSiteDir struct {
	task.BaseStep
	env  config.Env
	root string
	made string // 记录本次实际创建的目录，供回滚
}

func NewPrepareSiteDir(name string, env config.Env, root string) *PrepareSiteDir {
	return &PrepareSiteDir{BaseStep: task.BaseStep{StepName: name}, env: env, root: root}
}

func (s *PrepareSiteDir) Execute(_ context.Context, log task.StepLog) error {
	abs := config.ExpandHome(s.root)
	if _, err := os.Stat(abs); err == nil {
		log.Log("dim", "站点目录已存在，跳过创建: "+abs)
		return nil
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("创建站点目录失败 %s: %w", abs, err)
	}
	s.made = abs
	log.Log("ok", "已创建站点目录: "+abs)
	return nil
}

func (s *PrepareSiteDir) Rollback(_ context.Context) error {
	if s.made == "" {
		return nil
	}
	// 只删本次新建目录；非空则保留，避免误删用户已有内容
	if empty(s.made) {
		return os.Remove(s.made)
	}
	return nil
}

// WriteVHost 校验并落盘 vhost 正文；回滚删除文件（硬红线 2）
type WriteVHost struct {
	task.BaseStep
	mgr      VHosts
	validate vhost.Validator
	domain   string
	content  string
	wrote    bool
}

func NewWriteVHost(name string, mgr VHosts, validate vhost.Validator, domain, content string) *WriteVHost {
	return &WriteVHost{BaseStep: task.BaseStep{StepName: name}, mgr: mgr, validate: validate, domain: domain, content: content}
}

func (s *WriteVHost) Execute(ctx context.Context, log task.StepLog) error {
	if err := s.mgr.Save(ctx, s.validate, s.domain, s.content); err != nil {
		return err
	}
	s.wrote = true
	log.Log("ok", "已写入 vhost: "+s.domain)
	return nil
}

func (s *WriteVHost) Rollback(_ context.Context) error {
	if !s.wrote {
		return nil
	}
	return s.mgr.DeleteFile(s.domain)
}

// AddHosts 追加 hosts 条目；提权被拒/不可写仅记录 warning，不阻断建站
type AddHosts struct {
	task.BaseStep
	hosts  HostsOps
	domain string
}

func NewAddHosts(name string, h HostsOps, domain string) *AddHosts {
	return &AddHosts{BaseStep: task.BaseStep{StepName: name}, hosts: h, domain: domain}
}

func (s *AddHosts) Execute(_ context.Context, log task.StepLog) error {
	res, err := s.hosts.Add(s.domain)
	if err != nil {
		// hosts 失败不阻断建站：仅警告（§5.7）
		log.Log("err", "hosts 写入失败（站点仍创建成功）: "+err.Error())
		return nil
	}
	if res.Warning != "" {
		log.Log("err", res.Warning)
	}
	if res.Changed {
		log.Log("ok", "已添加 hosts: 127.0.0.1 "+s.domain)
	} else {
		log.Log("dim", "hosts 已存在，跳过")
	}
	return nil
}

// TrashSiteDir 把站点根目录移入回收站；回滚从回收站恢复
type TrashSiteDir struct {
	task.BaseStep
	trash     TrashMover
	origPath  string
	trashPath string
	moved     bool
}

func NewTrashSiteDir(name string, tr TrashMover, origPath string) *TrashSiteDir {
	return &TrashSiteDir{BaseStep: task.BaseStep{StepName: name}, trash: tr, origPath: origPath}
}

func (s *TrashSiteDir) Execute(_ context.Context, log task.StepLog) error {
	abs := config.ExpandHome(s.origPath)
	if _, err := os.Stat(abs); err != nil {
		log.Log("dim", "站点目录不存在，跳过回收: "+abs)
		return nil
	}
	dest, err := s.trash.Move(abs)
	if err != nil {
		return err
	}
	s.trashPath = dest
	s.moved = true
	log.Log("ok", "站点目录已移入回收站: "+dest)
	return nil
}

func (s *TrashSiteDir) Rollback(_ context.Context) error {
	if !s.moved {
		return nil
	}
	return s.trash.Restore(s.trashPath, s.origPath)
}

// TrashPath 暴露回收后路径（供 service 记录回收站条目）
func (s *TrashSiteDir) TrashPath() string { return s.trashPath }

// DeleteVHostFile 删除站点 vhost 文件；回滚重新写入原内容
type DeleteVHostFile struct {
	task.BaseStep
	mgr     VHosts
	domain  string
	content string // 删除前的正文，用于回滚
	deleted bool
}

func NewDeleteVHostFile(name string, mgr VHosts, domain, prevContent string) *DeleteVHostFile {
	return &DeleteVHostFile{BaseStep: task.BaseStep{StepName: name}, mgr: mgr, domain: domain, content: prevContent}
}

func (s *DeleteVHostFile) Execute(_ context.Context, log task.StepLog) error {
	if err := s.mgr.DeleteFile(s.domain); err != nil {
		return err
	}
	s.deleted = true
	log.Log("ok", "已移除 vhost: "+s.domain)
	return nil
}

func (s *DeleteVHostFile) Rollback(ctx context.Context) error {
	if !s.deleted {
		return nil
	}
	return s.mgr.Save(ctx, nil, s.domain, s.content) // 回滚不再校验，直接复原
}

func empty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) == 0
}
