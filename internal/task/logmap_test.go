// 多语言 Phase 2/3 的锁死用例：日志行的消息码对照表、步骤名记号、语言包覆盖、表与源码不漂移
package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"phpo/internal/model"
)

// TestLookupLine_PrefixRows 中文在句首的行：认出消息码，剩下的动态内容整段交给 {rest}。
func TestLookupLine_PrefixRows(t *testing.T) {
	cases := []struct {
		text  string
		code  string
		rest  string
		noRes bool
	}{
		{"已写入 vhost: demo.test", "log.vhostWritten", "demo.test", false},
		// 「已写入 」也在这张表里，但更长的首句必须先命中，否则 vhost 那行会说成「written」
		{"已写入 /data/phpo/nginx/conf/nginx.conf", "log.fileWritten", "/data/phpo/nginx/conf/nginx.conf", false},
		{"hosts 已存在，跳过", "log.hostsExists", "", true},
		{"已提升到离线缓存: phpo/php:8.4 → /data/offline/php/8.4/image-extensions.tar", "log.promotedToCache",
			"phpo/php:8.4 → /data/offline/php/8.4/image-extensions.tar", false},
		{"站点端口未发布到 Nginx: bind: address already in use", "log.sitePortUnpublished", "bind: address already in use", false},
		{"已删除缓存 php/8.4", "log.cacheEntryRemoved", "php/8.4", false},
		{"已删除 mysql：phpo-mysql-8.0", "log.deleted", "mysql：phpo-mysql-8.0", false},
	}
	for _, c := range cases {
		code, params := LookupLine(c.text)
		if code != c.code {
			t.Fatalf("行 %q 认成 %q，期望 %q", c.text, code, c.code)
		}
		if c.noRes {
			if len(params) != 0 {
				t.Fatalf("行 %q 不该带参数，实得 %+v", c.text, params)
			}
			continue
		}
		if params["rest"] != c.rest {
			t.Fatalf("行 %q 的 {rest} = %q，期望 %q", c.text, params["rest"], c.rest)
		}
	}
}

// TestLookupLine_PatternRows 句中夹数据的行：各段数据分别进参数，英文语序才排得下去。
func TestLookupLine_PatternRows(t *testing.T) {
	code, params := LookupLine("释放 63651126 字节（3 条目）")
	if code != "log.freed" || params["bytes"] != "63651126" || params["n"] != "3" {
		t.Fatalf("释放行 %+v %q", params, code)
	}
	code, params = LookupLine("mysql 8.0 逻辑导出失败：exit status 1 —— 本次归档不含该库数据")
	if code != "log.dumpFail" || params["kind"] != "mysql" || params["version"] != "8.0" || params["reason"] != "exit status 1" {
		t.Fatalf("逻辑导出失败行 %+v %q", params, code)
	}
	code, params = LookupLine("扩展 gd 缺编译要用的系统开发包: zlib、libpng")
	if code != "log.extMissingPkg" || params["name"] != "gd" || params["pkgs"] != "zlib、libpng" {
		t.Fatalf("缺失系统包行 %+v %q", params, code)
	}
	// 「已删除 X：Y（腾出 N 字节）」必须走这一条，不能被更短的「已删除 」首句抢走
	code, params = LookupLine("已删除 volume：phpo-redis-8-data（腾出 1024 字节）")
	if code != "log.deletedFreed" || params["kind"] != "volume" || params["bytes"] != "1024" {
		t.Fatalf("带腾出字节的删除行 %+v %q", params, code)
	}
}

// TestLookupLine_NameBecomesStepRef 参数里那个 name 若是已登记的步骤名，要换成 @消息码 记号；
// 认不出的（比如容器名）原样留着——不然英文界面里会出现一个假消息码。
func TestLookupLine_NameBecomesStepRef(t *testing.T) {
	_, params := LookupLine("nginx 容器 phpo-nginx-1.25 未运行，跳过重载")
	if got := params["name"]; got != "phpo-nginx-1.25" {
		t.Fatalf("容器名不该被换成消息码，实得 %q", got)
	}
}

// TestStepRef 步骤名 → @消息码｜中文原文。
func TestStepRef(t *testing.T) {
	if got, want := stepRef("校验"), "@step.verify|校验"; got != want {
		t.Fatalf("固定步骤名 %q，期望 %q", got, want)
	}
	got := stepRef("创建并启动 phpo-php-8.4")
	if !strings.HasPrefix(got, "@step.createAndStart?rest=") {
		t.Fatalf("带数据的步骤名没换成记号: %q", got)
	}
	if !strings.HasSuffix(got, "|创建并启动 phpo-php-8.4") {
		t.Fatalf("记号里必须带上中文原文作回退: %q", got)
	}
	if got := stepRef("没登记过的步骤"); got != "没登记过的步骤" {
		t.Fatalf("认不出的步骤名要原样返回，实得 %q", got)
	}
}

// TestStepRefOf 步骤名记号：**构造点直接给了码就用它**，没给才退到按中文名识别的兼容路（§5.26）。
func TestStepRefOf(t *testing.T) {
	// 构造点给码：中文名照旧进记号末尾作回退，参数按 key 排序拼在 ? 后面
	got := stepRefOf(&FuncStep{StepName: "启动 phpo-php-8.4", Code: MsgTaskStart, Params: map[string]string{"name": "phpo-php-8.4"}})
	if want := "@task.start?name=phpo-php-8.4|启动 phpo-php-8.4"; got != want {
		t.Fatalf("构造点的码要用上，实得 %q，期望 %q", got, want)
	}
	// 没给码：退回对照表按中文名识别，结果与 stepRef 一致
	got = stepRefOf(&FuncStep{StepName: "校验"})
	if want := stepRef("校验"); got != want {
		t.Fatalf("没给码时要走识别层，实得 %q，期望 %q", got, want)
	}
	// 两处都认不出：原样返回中文名，那一行只是不跟着换语言，不会少一行日志
	got = stepRefOf(&FuncStep{StepName: "扫描并删除孤儿资源"})
	if got != "扫描并删除孤儿资源" {
		t.Fatalf("认不出的步骤名要原样返回，实得 %q", got)
	}
	// 不内嵌 BaseStep 的步骤（没有 StepMsg 这个方法）也不能炸，直接走识别层
	got = stepRefOf(nameOnlyStep("校验"))
	if want := "@step.verify|校验"; got != want {
		t.Fatalf("无码接口实现要照旧识别，实得 %q，期望 %q", got, want)
	}
}

// nameOnlyStep 只实现 Step 五个方法、不内嵌 BaseStep 的步骤，用来验证类型断言的降级路。
type nameOnlyStep string

func (s nameOnlyStep) Name() string                           { return string(s) }
func (s nameOnlyStep) Execute(context.Context, StepLog) error { return nil }
func (s nameOnlyStep) Rollback(context.Context) error         { return nil }
func (s nameOnlyStep) Cleanup()                               {}
func (s nameOnlyStep) Cancelable() bool                       { return false }

// TestLabelRef 任务标签记号：有消息码时带码与参数，没有时原样返回中文标签（Phase 2 之后不该再有后者）。
func TestLabelRef(t *testing.T) {
	got := labelRef(&Task{Label: "安装 phpo-mysql-8.4", LabelCode: MsgTaskInstall, LabelParams: map[string]string{"name": "phpo-mysql-8.4"}})
	if !strings.HasPrefix(got, "@task.install?name=") || !strings.HasSuffix(got, "|安装 phpo-mysql-8.4") {
		t.Fatalf("标签记号形状不对: %q", got)
	}
	if got := labelRef(&Task{Label: "还没接码的任务"}); got != "还没接码的任务" {
		t.Fatalf("没接码的标签要原样返回，实得 %q", got)
	}
}

// TestEveryChineseLogLineIsMapped 对照表不得与源码漂移：源码里每一行「中文在句首」的日志，
// 都必须能在表里认出消息码。漏登记不炸编译，只会让那一行在英文界面永远显示中文——所以用这条用例拦。
// 句中夹数据的行按整句形状（logPatterns）登记，本用例同样覆盖：认不出码即失败。
func TestEveryChineseLogLineIsMapped(t *testing.T) {
	han := regexp.MustCompile(`[\p{Han}]`)
	call := regexp.MustCompile(`log\.Log\([^,]+,\s*(?:"([^"]*)"|fmt\.Sprintf\("([^"]*)")`)
	// fmt.Sprintf 的行按「形状」登记，表里存的是渲染后的整句，这里用样例值填回去再对表
	samples := map[string]string{"%s": "phpo-php-8.4", "%d": "3", "%v": "boom", "%.1f": "1.0"}
	var missed []string
	root := "../.."
	files, _ := filepath.Glob(filepath.Join(root, "internal", "*", "*.go"))
	more, _ := filepath.Glob(filepath.Join(root, "internal", "*", "*", "*.go"))
	files = append(files, more...)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, e := os.ReadFile(f)
		if e != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			m := call.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			lit := m[1]
			if lit == "" {
				lit = m[2]
				for k, v := range samples {
					lit = strings.ReplaceAll(lit, k, v)
				}
			}
			if !han.MatchString(lit) {
				continue // 纯数据行（路径、argv、容器输出）本来就不该进表
			}
			if code, _ := LookupLine(lit); code != "" || CoversTemplate(lit) {
				continue
			}
			rel, _ := filepath.Rel(root, f)
			missed = append(missed, fmt.Sprintf("%s:%d %q", rel, i+1, lit))
		}
	}
	if len(missed) > 0 {
		sort.Strings(missed)
		t.Fatalf("这些中文日志行没在对照表里认出消息码（英文界面会显示中文）:\n%s", strings.Join(missed, "\n"))
	}
}

// TestFrameworkLinesCarryCodes 任务框架行（▶ 标签 / 步骤 X / X 完成）必须带消息码，
// 且参数里的步骤名是 @记号——这三句是每一次任务都要打的，英文界面看不见码就等于白做。
func TestFrameworkLinesCarryCodes(t *testing.T) {
	em := &capturingEmitter{}
	m := NewManager(em)
	task := &Task{
		ID:          "t-1",
		Label:       "安装 phpo-nginx-alpine",
		LabelCode:   MsgTaskInstall,
		LabelParams: map[string]string{"name": "phpo-nginx-alpine"},
		Steps:       []Step{&testStep{BaseStep: BaseStep{StepName: "校验"}, rec: &recorder{}}},
	}
	if _, err := m.Run(context.Background(), task); err != nil {
		t.Fatalf("任务应当成功: %v", err)
	}
	var got []model.TaskLogEvent
	for _, e := range em.Capture() {
		if e.Name != "task:log" {
			continue
		}
		got = append(got, e.Payload.(model.TaskLogEvent))
	}
	byText := map[string]model.TaskLogEvent{}
	for _, g := range got {
		byText[g.Text] = g
	}
	for text, wantCode := range map[string]string{
		"▶ 安装 phpo-nginx-alpine": "log.taskStart",
		"步骤 校验":                  "log.step",
		"校验 完成":                  "log.stepDone",
	} {
		g, ok := byText[text]
		if !ok {
			t.Fatalf("没有收到框架行 %q，实收 %+v", text, got)
		}
		if g.Code != wantCode {
			t.Fatalf("框架行 %q 的消息码 = %q，期望 %q", text, g.Code, wantCode)
		}
	}
	// 原文必须一并带上：账本与不认码的界面都靠它
	if g := byText["步骤 校验"]; g.Params["name"] != "@step.verify|校验" {
		t.Fatalf("步骤名应换成带原文的记号，实得 %q", g.Params["name"])
	}
	if g := byText["▶ 安装 phpo-nginx-alpine"]; !strings.HasPrefix(g.Params["label"], "@task.install?name=") {
		t.Fatalf("任务开始行应引用标签自己的码，实得 %q", g.Params["label"])
	}
}

// TestFrameworkLinesUseConstructionCode 步骤名在对照表里认不出、但构造点直接给了消息码时，
// 框架行要用构造点那份码（§5.26 条款③）。锁的是 task.go 真的走 stepRefOf，不是只测助手函数。
func TestFrameworkLinesUseConstructionCode(t *testing.T) {
	em := &capturingEmitter{}
	m := NewManager(em)
	name := "启动 phpo-php-8.4" // 这个步骤名没登记进对照表，只有构造点的码认得它
	task := &Task{
		ID:    "t-coded",
		Label: name,
		Steps: []Step{&testStep{BaseStep: BaseStep{StepName: name, Code: MsgTaskStart, Params: map[string]string{"name": "phpo-php-8.4"}}, rec: &recorder{}}},
	}
	if _, err := m.Run(context.Background(), task); err != nil {
		t.Fatalf("任务应当成功: %v", err)
	}
	for _, e := range em.Capture() {
		if e.Name != "task:log" {
			continue
		}
		g := e.Payload.(model.TaskLogEvent)
		if g.Code == "log.step" {
			if want := "@task.start?name=phpo-php-8.4|" + name; g.Params["name"] != want {
				t.Fatalf("构造点给的码要用上：实得 %q，期望 %q", g.Params["name"], want)
			}
			if g.Text != "步骤 "+name {
				t.Fatalf("中文原文不得改：实得 %q", g.Text)
			}
			return
		}
	}
	t.Fatalf("没有收到「步骤」那一行，实收 %+v", em.Capture())
}

// TestTaskLabelsAllCoded 每个任务的标签都必须带消息码（Phase 2 的收口判据）：
// 后端造 Task 时漏码，队列行与抽屉标题在英文下就不会跟着换语言。
// 这条用例只查「有 Label 就没漏 LabelCode」，具体文案由下一条查语言包。
func TestTaskLabelsAllCoded(t *testing.T) {
	root := "../.."
	files, _ := filepath.Glob(filepath.Join(root, "internal", "*", "*.go"))
	more, _ := filepath.Glob(filepath.Join(root, "internal", "*", "*", "*.go"))
	files = append(files, more...)
	pattern := regexp.MustCompile(`&task\.Task\{`)
	var missed []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, e := os.ReadFile(f)
		if e != nil {
			continue
		}
		src := string(b)
		for _, m := range pattern.FindAllStringIndex(src, -1) {
			// 从 &task.Task{ 起扫到与之配对的 }
			depth, i := 0, m[1]-1
			for ; i < len(src); i++ {
				switch src[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				if depth == 0 {
					break
				}
			}
			block := src[m[0] : i+1]
			if strings.Contains(block, "Label:") && !strings.Contains(block, "LabelCode") {
				rel, _ := filepath.Rel(root, f)
				missed = append(missed, fmt.Sprintf("%s:%d %s", rel, strings.Count(src[:m[0]], "\n")+1,
					strings.SplitN(strings.TrimSpace(block), "\n", 3)[1]))
			}
		}
	}
	if len(missed) > 0 {
		t.Fatalf("这些任务构造点没带 LabelCode:\n%s", strings.Join(missed, "\n"))
	}
}

// TestLocaleKeysCoverAllCodes 表里每个消息码，中英两侧语言包都必须有对应文案，且占位符一致。
// 少一侧即视为缺陷：那一行在某种语言下会退回中文，或者渲染出键名。
func TestLocaleKeysCoverAllCodes(t *testing.T) {
	zh := localeKeys(t, filepath.Join("..", "..", "frontend", "src", "locales", "zh-CN.ts"))
	en := localeKeys(t, filepath.Join("..", "..", "frontend", "src", "locales", "en-US.ts"))
	want := append(LogCodes(), FrameworkCodes()...)
	// Phase 2 收口判据：25 条任务标签消息码 + 新增的 task.siteAdd 全部都要在两侧语言包里
	want = append(want,
		MsgTaskInstall, MsgTaskReinstall, MsgTaskStart, MsgTaskStop, MsgTaskRemove,
		MsgTaskSiteAdd, MsgTaskSitePort, MsgTaskSitePhp, MsgTaskSiteRewrite, MsgTaskSiteVhost,
		MsgTaskSiteHosts, MsgTaskSiteRemove, MsgTaskConfigSave, MsgTaskUpdate, MsgTaskExtensionApply,
		MsgTaskWizardInit, MsgTaskOfflineRemove, MsgTaskOfflineImport, MsgTaskDockerClean,
		MsgTaskCleanupOrphan, MsgTaskCleanupCache, MsgTaskTrashRestore, MsgTaskTrashEmpty,
		MsgTaskBackupCreate, MsgTaskBackupRestore, MsgTaskBackupDelete)
	var miss []string
	for _, c := range want {
		if _, ok := zh[c]; !ok {
			miss = append(miss, "zh-CN 缺 "+c)
		}
		if _, ok := en[c]; !ok {
			miss = append(miss, "en-US 缺 "+c)
		}
		if a, b := zh[c], en[c]; a != "" && b != "" && !sameTokens(a, b) {
			miss = append(miss, fmt.Sprintf("%s 两侧占位符不一致: %q vs %q", c, a, b))
		}
	}
	if len(miss) > 0 {
		sort.Strings(miss)
		t.Fatalf("语言包没覆盖全部消息码:\n%s", strings.Join(miss, "\n"))
	}
}

// TestPatternTemplatesSelfConsistent 表里每行的中文模板，按捕获组的形状填回样例值，必须还能被自己的正则认出。
// 这是「改了句式忘了改正则」这类漂移的拦截点——漂移的后果是那一行在两种语言下都退回中文原文。
func TestPatternTemplatesSelfConsistent(t *testing.T) {
	groupRe := regexp.MustCompile(`\(([^)]+)\)`)
	for _, p := range append(append([]logPattern{}, logPatterns...), stepNamePatterns...) {
		src := p.re.String()
		vals := make([]string, len(p.keys))
		for i, k := range p.keys {
			vals[i] = "样例" + k
		}
		// 组是 \d+ 就给数字，是 \S+ 就给一个不含空白的名字，其余给一段可读文本
		for i, m := range groupRe.FindAllStringSubmatch(src, -1) {
			if i >= len(vals) {
				break
			}
			switch {
			case strings.Contains(m[1], `\d`):
				vals[i] = "12"
			case strings.Contains(m[1], `\S`):
				vals[i] = "phpo-php-8.4"
			}
		}
		text := p.zh
		for i, k := range p.keys {
			text = strings.ReplaceAll(text, "{"+k+"}", vals[i])
		}
		if !p.re.MatchString(text) {
			t.Fatalf("模板 %q 填回后不再匹配自己的正则 %v:\n  填出的句子: %q", p.zh, p.re.String(), text)
		}
	}
}

// localeKeys 读出语言包文件里的扁平键与文案（本项目的 locales 是手写扁平字典，不走 vue-i18n 嵌套解析）。
func localeKeys(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读语言包失败 %s: %v", path, err)
	}
	line := regexp.MustCompile(`^\s*"((?:[^"\\]|\\.)*)":\s*"((?:[^"\\]|\\.)*)",\s*$`)
	out := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if m := line.FindStringSubmatch(l); m != nil {
			out[m[1]] = m[2]
		}
	}
	return out
}

var tokenRe = regexp.MustCompile(`\{([A-Za-z]+)\}`)

func sameTokens(a, b string) bool {
	ta, tb := map[string]bool{}, map[string]bool{}
	for _, m := range tokenRe.FindAllStringSubmatch(a, -1) {
		ta[m[1]] = true
	}
	for _, m := range tokenRe.FindAllStringSubmatch(b, -1) {
		tb[m[1]] = true
	}
	for k := range ta {
		if !tb[k] {
			return false
		}
	}
	for k := range tb {
		if !ta[k] {
			return false
		}
	}
	return true
}
