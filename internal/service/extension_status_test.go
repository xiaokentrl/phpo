// §5.16.2「启用态的唯一真值」：打开「管理扩展」时后端到容器里现查一次 php -m，
// 归一后回写权威库并随快照回流；拿不到实测就退回库里那份并明示非实时，绝不整片画成 off。
// 停用只删 conf.d 里的 ini，因此「能不能停用」的判据也只能是那份目录清单。
package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
)

// 真机 php -m 的形状（干净基座 php:8.4-fpm）：两段标题 + 显示名，其中 PDO / SQLite3 / Zend OPcache
// 与扩展目录用小写的名字不同，必须先归一才谈得上比对。
const fakePhpM = "[PHP Modules]\nbcmath\nCore\ncurl\ndate\ngd\nlibxml\nmbstring\nmysqli\nopenssl\nPDO\nsqlite3\nsodium\nzlib\n\n[Zend Modules]\nZend OPcache\n"

// 真机 ls /usr/local/etc/php/conf.d：docker-fpm.ini 与 zz-phpo.ini 不是扩展 ini
// （前者是镜像自带、后者是挂载进去的 php.ini），只有 docker-php-ext-<name>.ini 才是可删的那一份。
var fakeConfD = []string{"docker-fpm.ini", "docker-php-ext-gd.ini", "docker-php-ext-opcache.ini", "docker-php-ext-sodium.ini", "zz-phpo.ini"}

// extStatusEnabled 14 项归一后的实测集（稳定序）
var extStatusEnabled = []string{
	"bcmath", "core", "curl", "date", "gd", "libxml", "mbstring",
	"mysqli", "opcache", "openssl", "pdo", "sodium", "sqlite3", "zlib",
}

// extStatusBuiltIn 其中无 ini 可删的 11 项：静态编进基座，对它执行 rm -f 是空操作
var extStatusBuiltIn = []string{
	"core", "curl", "date", "libxml", "mbstring", "mysqli", "openssl", "pdo", "sqlite3", "zlib", "bcmath",
}

func joined(xs []string) string {
	c := append([]string(nil), xs...)
	sort.Strings(c)
	return strings.Join(c, ",")
}

func TestExtStatus_ProbesPhpM_NormalizesAndWritesBack(t *testing.T) {
	svc, rt, _, st, em, _ := newExtSvc(t)
	name := dockerName("php", "8.4")
	rt.running[name] = true
	rt.phpMOut = fakePhpM
	rt.iniFiles = fakeConfD

	stt, err := svc.Status(context.Background(), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if !stt.Live {
		t.Fatalf("容器在跑且两条探针都成功，应报实时值")
	}
	if got := joined(stt.Enabled); got != joined(extStatusEnabled) {
		t.Fatalf("启用集应为归一后的实测集\n期望 %s\n实得 %s", joined(extStatusEnabled), got)
	}
	if got := joined(stt.BuiltIn); got != joined(extStatusBuiltIn) {
		t.Fatalf("内建集应为实测集里没有 ini 的那几项\n期望 %s\n实得 %s", joined(extStatusBuiltIn), got)
	}
	// 只读：打开弹窗不该改动容器状态，也不该被当成编译命令
	if len(rt.execs) != 0 {
		t.Fatalf("现取启用态不得执行任何编译命令，实得 %v", rt.execs)
	}
	wantReads := []string{"php -m", "ls -1 " + config.ExtConfDir}
	if got := strings.Join(rt.reads, "|"); got != strings.Join(wantReads, "|") {
		t.Fatalf("两条探针应各发一次\n期望 %v\n实得 %v", wantReads, rt.reads)
	}
	// 回写实测集（不是库里旧值），并随快照回流
	if got := joined(st.saved["8.4"]); got != joined(extStatusEnabled) {
		t.Fatalf("应把实测集回写权威库，实得 %s", got)
	}
	if !em.has("state:changed") {
		t.Fatalf("回写后应广播快照，实得 %v", em.events)
	}
}

// TestExtStatus_ContainerNotRunning_FallsBackToDB 容器没跑就没法实测。此时界面必须显示库里那份
// 并说明「非实时」——拿不到实测就当没装，等于把「不知道」画成「没有」。
func TestExtStatus_ContainerNotRunning_FallsBackToDB(t *testing.T) {
	svc, _, _, st, em, _ := newExtSvc(t)
	st.snap.PHPExtensions["8.4"] = []string{"redis", "gd"}

	stt, err := svc.Status(context.Background(), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if stt.Live {
		t.Fatalf("容器未运行不得报实时值")
	}
	if got := strings.Join(stt.Enabled, ","); got != "redis,gd" {
		t.Fatalf("应退回库里那份，实得 %s", got)
	}
	if len(stt.BuiltIn) != 0 {
		t.Fatalf("非实时时不得断言可停用性，实得 %v", stt.BuiltIn)
	}
	if len(st.saved) != 0 {
		t.Fatalf("非实时不应回写库（那是伪造权威），实得 %v", st.saved)
	}
	if em.has("state:changed") {
		t.Fatalf("没改任何东西就不该发事件，实得 %v", em.events)
	}
}

// TestExtStatus_ProbeFailure_FallsBackToDB 容器在跑但 php -m 失败（镜像里没有 php / exec 报错）：同上去库里那份。
func TestExtStatus_ProbeFailure_FallsBackToDB(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	name := dockerName("php", "8.4")
	rt.running[name] = true
	rt.phpMErr = errors.New("exec failed")
	st.snap.PHPExtensions["8.4"] = []string{"redis"}

	stt, err := svc.Status(context.Background(), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if stt.Live || strings.Join(stt.Enabled, ",") != "redis" {
		t.Fatalf("探针失败应退回非实时的库里集，实得 %+v", stt)
	}
	if len(st.saved) != 0 {
		t.Fatalf("探针失败不应回写库，实得 %v", st.saved)
	}
}

// TestExtStatus_EmptyProbe_FallsBackToDB 真机上的 php 无论怎么裁都会报 Core / date，
// 一条都没有就等于探针没读到东西——把空结果当「这个版本一个扩展都没装」是假的。
func TestExtStatus_EmptyProbe_FallsBackToDB(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	rt.running[dockerName("php", "8.4")] = true
	st.snap.PHPExtensions["8.4"] = []string{"redis"}

	stt, err := svc.Status(context.Background(), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if stt.Live || strings.Join(stt.Enabled, ",") != "redis" {
		t.Fatalf("空实测集不得当作启用态，实得 %+v", stt)
	}
	if len(st.saved) != 0 {
		t.Fatalf("空实测集不应回写库，实得 %v", st.saved)
	}
}

// TestExtStatus_IniListFailure_NonLive 启用态知道、可停用性不知道：
// 把「ls 失败」报成「全都删不掉」是断言了一件具体错事（界面上每颗开关都会变哑）。
func TestExtStatus_IniListFailure_NonLive(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	rt.running[dockerName("php", "8.4")] = true
	rt.phpMOut = fakePhpM
	rt.iniListErr = errors.New("no such directory")
	st.snap.PHPExtensions["8.4"] = []string{"redis", "gd"}

	stt, err := svc.Status(context.Background(), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if stt.Live {
		t.Fatalf("分不清可停用性就不该报实时值")
	}
	if got := strings.Join(stt.Enabled, ","); got != "redis,gd" {
		t.Fatalf("应退回库里那份，实得 %s", got)
	}
	if len(st.saved) != 0 {
		t.Fatalf("非实时不应回写库，实得 %v", st.saved)
	}
}

// TestExtStatus_NoWriteBackWhenUnchanged 幂等静默：每次开弹窗都现取一次，
// 但实测集与库里一致时不得写库、不得重发快照——否则「打开弹窗」这件事本身就会刷屏。
func TestExtStatus_NoWriteBackWhenUnchanged(t *testing.T) {
	svc, rt, _, st, em, _ := newExtSvc(t)
	rt.running[dockerName("php", "8.4")] = true
	rt.phpMOut = fakePhpM
	rt.iniFiles = fakeConfD
	st.snap.PHPExtensions["8.4"] = append([]string(nil), extStatusEnabled...)

	if _, err := svc.Status(context.Background(), "8.4"); err != nil {
		t.Fatal(err)
	}
	if st.setCalls != 0 {
		t.Fatalf("实测集与库里相同不应回写，回写了 %d 次", st.setCalls)
	}
	if em.has("state:changed") || em.has("service:changed") {
		t.Fatalf("未变化不应广播，实得 %v", em.events)
	}
}

// TestApply_PersistsMeasuredSet 任务结尾落库的必须是容器里实测到的那一份，不是本次请求的目标集。
// 基座自带的扩展（curl / mbstring / PDO …）从来没进过目标集：只写目标集就等于
// 库里说「这个版本只装了 redis 和 gd」，而 php -m 里有十四项——界面从此与容器不符。
func TestApply_PersistsMeasuredSet(t *testing.T) {
	svc, rt, _, st, _, _ := newExtSvc(t)
	rt.copiedFrom[config.ExtStagingPECL] = []string{"redis-6.0.2.tgz"}
	rt.phpMOut = fakePhpM + "redis\n"
	rt.iniFiles = append([]string{"docker-php-ext-redis.ini"}, fakeConfD...)

	if err := svc.Apply(context.Background(), "8.4", []string{"redis", "gd"}); err != nil {
		t.Fatal(err)
	}
	got := joined(st.saved["8.4"])
	if !strings.Contains(got, ",redis,") && !strings.HasPrefix(got, "redis,") {
		t.Fatalf("落库集应含实测到的 redis，实得 %s", got)
	}
	if !strings.Contains(got, "bcmath") || !strings.Contains(got, "curl") {
		t.Fatalf("基座自带的扩展必须一并落库（它们确实开着），实得 %s", got)
	}
}

// TestApply_MeasuredProbeFailure_KeepsTargetSet 实测拿不到时不能把整单判死：
// 扩展已经编译进镜像、容器已在固化镜像上运行，回滚等于毁掉做对了的工作。
// 此处退回本次请求的目标集落库，并给一行说明——非实时由下次打开弹窗的现取纠正。
func TestApply_MeasuredProbeFailure_KeepsTargetSet(t *testing.T) {
	svc, rt, _, st, em, _ := newExtSvc(t)
	rt.phpMErr = errors.New("exec failed")

	if err := svc.Apply(context.Background(), "8.4", []string{"redis", "gd"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.saved["8.4"], ","); got != "redis,gd" {
		t.Fatalf("拿不到实测应退回本次目标集，实得 %s", got)
	}
	if all := strings.Join(em.logs, "\n"); !strings.Contains(all, "非实时") {
		t.Fatalf("应有一行说明本次落库的不是实测集，实得 %v", em.logs)
	}
}

// TestApply_SkipsBuiltinInRemoved_WithDimLine 用户取消勾选静态内建的扩展时，
// 对它执行 rm -f 是空操作（没有 .so、也没有 ini 可删）。不得让这种项进停用循环：
// 那既不会有任何效果，又让日志说「已停用 gd」而 php -m 里 gd 还在——界面撒谎。
func TestApply_SkipsBuiltinInRemoved_WithDimLine(t *testing.T) {
	svc, rt, _, st, em, _ := newExtSvc(t)
	_ = st.SetPHPExtensions("8.4", []string{"redis", "gd"})
	rt.hasImages = []string{engine.CommittedPHPRef("8.4")}
	// conf.d 里只有 redis 的 ini：gd 是基座静态内建
	rt.iniFiles = []string{"docker-fpm.ini", "docker-php-ext-redis.ini", "zz-phpo.ini"}

	if err := svc.Apply(context.Background(), "8.4", []string{}); err != nil {
		t.Fatal(err)
	}
	want := "rm -f " + config.ExtConfDir + "/docker-php-ext-redis.ini"
	if len(rt.execs) != 1 || rt.execs[0] != want {
		t.Fatalf("只应停用有 ini 可删的那项\n期望 %s\n实得 %v", want, rt.execs)
	}
	if all := strings.Join(em.logs, "\n"); !strings.Contains(all, "内建") {
		t.Fatalf("跳过内建项必须给一行说明，不得静默，实得 %v", em.logs)
	}
}
