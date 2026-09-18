// T604d · Updater 编排单测：成功升级留待确认标记 / 篡改包双校验拒绝 / 安装同步失败即时回滚 / 无新版直接返回 / 中断下次启动回滚
package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/model"
)

type fakeSrc struct {
	rel *Release
	err error
}

func (s fakeSrc) FetchLatest(context.Context) (*Release, error) { return s.rel, s.err }

type fakeDL struct {
	content []byte
	err     error
	called  int
}

func (d *fakeDL) Download(_ context.Context, _, dst string, on ProgressFunc) error {
	d.called++
	if d.err != nil {
		return d.err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, d.content, 0o644); err != nil {
		return err
	}
	if on != nil {
		on(int64(len(d.content)), int64(len(d.content)))
	}
	return nil
}

type fakeInst struct {
	err    error
	called int
	last   string
}

func (i *fakeInst) Install(_ context.Context, pkg string) error {
	i.called++
	i.last = pkg
	return i.err
}

// newTestUpdater 构造带可控 exe/备份/公钥的编排器；返回其临时目录与替身
func newTestUpdater(t *testing.T, content []byte, pub ed25519.PublicKey) (*Updater, *fakeDL, *fakeInst, string) {
	t.Helper()
	dir := t.TempDir()
	dlDir := filepath.Join(dir, "downloads")
	bkDir := filepath.Join(dir, "backups")
	exeDir := filepath.Join(dir, "bin")
	_ = os.MkdirAll(exeDir, 0o755)
	exe := filepath.Join(exeDir, "phpo")
	_ = os.WriteFile(exe, []byte("OLD-BINARY"), 0o755)

	u := New("0.1.0", dlDir, bkDir, fakeSrc{}, &fakeDL{}, &fakeInst{}, NewRollback(dir), &capEmitter{})
	u.pub = pub
	u.dl = &fakeDL{content: content}
	u.exePath = func() (string, error) { return exe, nil }
	fdl := u.dl.(*fakeDL)
	fin := u.inst.(*fakeInst)
	return u, fdl, fin, exe
}

// signedRelease 用给定私钥对内容签名，产出可通过双校验的发布元数据
func signedRelease(t *testing.T, content []byte, priv ed25519.PrivateKey) *Release {
	t.Helper()
	sum := shaHex(content)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(sum)))
	return &Release{Version: "0.2.0", Size: int64(len(content)), SHA256: sum, Signature: sig}
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestUpdater_Success_LeavesConfirmMarker(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	content := []byte("NEW-PACKAGE-BYTES")
	u, _, fin, exe := newTestUpdater(t, content, pub)
	u.src = fakeSrc{rel: signedRelease(t, content, priv)}

	done, err := u.Apply(context.Background())
	if err != nil {
		t.Fatalf("升级应成功: %v", err)
	}
	if done.Status != model.TaskSuccess || done.Version != "0.2.0" {
		t.Fatalf("done 应为 success/0.2.0，实得 %+v", done)
	}
	if fin.called != 1 {
		t.Fatalf("Install 应被调用 1 次，实得 %d", fin.called)
	}
	// 安装成功后仍保留 pending 标记，待新版本下次启动确认
	p, ok, _ := u.rb.Load()
	if !ok || p.Target != "0.2.0" {
		t.Fatalf("成功后应保留 0.2.0 的确认标记，ok=%v p=%+v", ok, p)
	}
	// 备份目录应存有旧二进制
	if _, err := os.Stat(filepath.Join(u.backups, filepath.Base(exe)+"-0.1.0")); err != nil {
		t.Fatalf("旧版备份缺失: %v", err)
	}
}

func TestUpdater_TamperedPkg_Rejected(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	u, _, fin, _ := newTestUpdater(t, []byte("ACTUAL-BYTES"), pub)
	// 发布元数据声称的 SHA256 与实际下载内容不符 → 篡改包
	u.src = fakeSrc{rel: &Release{Version: "0.2.0", SHA256: shaHex([]byte("DIFFERENT")), Signature: "AAAA"}}

	_, err := u.Apply(context.Background())
	if err == nil || !errors.Is(err, ErrChecksum) {
		t.Fatalf("篡改包应因 SHA256 失败被拒，实得 %v", err)
	}
	if fin.called != 0 {
		t.Fatal("校验失败不得进入安装")
	}
	// 未写标记（Begin 在校验之后）
	if _, ok, _ := u.rb.Load(); ok {
		t.Fatal("校验失败不应写 pending 标记")
	}
}

func TestUpdater_NoPublicKeyConfigured_Rejects(t *testing.T) {
	content := []byte("pkg")
	u, _, _, _ := newTestUpdater(t, content, nil) // pub=nil
	u.src = fakeSrc{rel: signedRelease2(content)}
	if _, err := u.Apply(context.Background()); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("未配置公钥应拒绝升级（硬红线6），实得 %v", err)
	}
}

// signedRelease2 仅用于 nil-pub 用例：签名内容无意义，只要 SHA 匹配即可推进到签名分支
func signedRelease2(content []byte) *Release {
	return &Release{Version: "0.2.0", SHA256: shaHex(content), Signature: "irrelevant"}
}

func TestUpdater_InstallFails_ImmediateRollback(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	content := []byte("NEW-PACKAGE")
	u, _, fin, exe := newTestUpdater(t, content, pub)
	fin.err = errors.New("boom") // 安装同步失败
	u.src = fakeSrc{rel: signedRelease(t, content, priv)}

	done, err := u.Apply(context.Background())
	if err == nil {
		t.Fatal("安装失败应返回错误")
	}
	if done.Status != model.TaskFailed || done.Version != "0.1.0" {
		t.Fatalf("应报 failed/回退当前版本，实得 %+v", done)
	}
	// 回滚：当前二进制被备份内容覆盖回旧版
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD-BINARY" {
		t.Fatalf("应恢复旧二进制，实得 %q", got)
	}
	// 同步失败即时清除标记
	if _, ok, _ := u.rb.Load(); ok {
		t.Fatal("同步失败回滚后应清除标记")
	}
}

func TestUpdater_Interrupted_RollbackNextStartup(t *testing.T) {
	dir := t.TempDir()
	rb := NewRollback(dir)
	exe := filepath.Join(dir, "phpo")
	_ = os.WriteFile(exe, []byte("NEW-BUT-BROKEN"), 0o755)
	backup := filepath.Join(dir, "phpo-0.1.0")
	_ = os.WriteFile(backup, []byte("OLD-BINARY"), 0o755)
	// 模拟：安装中被杀，仍运行旧版 0.1.0，标记目标 0.2.0
	_ = rb.Begin(PendingUpdate{Target: "0.2.0", Backup: backup})

	u := New("0.1.0", dir, dir, fakeSrc{}, &fakeDL{}, &fakeInst{}, rb, &capEmitter{})
	u.exePath = func() (string, error) { return exe, nil }
	u.cpFile = copyFile

	rolled, err := rb.RecoverOnStartup(u.current, u.Restore)
	if !rolled || err != nil {
		t.Fatalf("下次启动应自动回滚，rolled=%v err=%v", rolled, err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD-BINARY" {
		t.Fatalf("应回滚为旧二进制，实得 %q", got)
	}
	if _, ok, _ := rb.Load(); ok {
		t.Fatal("回滚后应清除标记")
	}
}
