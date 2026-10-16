//go:build ignore

// check-i18n-keys.go —— i18n 键对齐校验（TX02，T103 验收；v2.9.16 起含占位符集合校验，对齐 AGENTS §6 第 1 项）
// 目的：
//  1. zh-CN.ts 与 en-US.ts 键集合完全相等（切语言无缺键）；
//  2. 每条键的占位符集合（{name} 记号）两侧相等——英文把 {n} 写成 {count} 这类漂移在这里拦下，
//     否则英文界面渲染时 {n} 原样漏出、参数悄悄丢掉；
//  3. 每条键的转义形状（`\n`、`\"` 这类反斜杠记号）两侧相等——译文里把 `\n` 误写成 `\\n`，
//     界面会显示出一个字面的反斜杠 n，占位符校验看不见它，所以单独拦一道；
//  4. 条目行文法唯一：双引号键 + 双引号值。长得像条目却按别的方式写（模板字符串值、单引号值、
//     字符串拼接）一律判红——静默跳过 = 双方各漏各的，对齐校验形同虚设。
//     值内的反引号只是普通字符（如 `/var/lib/...` 代码标记），不影响解析。
//
// 运行：go run scripts/check-i18n-keys.go（可选参数 <zh> <en> 覆盖路径，供负向演练）
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// 匹配键值行：  "some.key": "value"（值内允许转义引号）
var kvRe = regexp.MustCompile(`^\s*"((?:[^"\\]|\\.)*)"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,?\s*$`)

// keyLineRe 行首长成 "键名": 的行——条目行的必要形状，用于抓「像条目却没解析成功」的漏网行
var keyLineRe = regexp.MustCompile(`^\s*"((?:[^"\\]|\\.)*)"\s*:`)

// 占位符记号：{name}——与前端渲染的 split('{'+k+'}') 一致
var phRe = regexp.MustCompile(`\{(\w+)\}`)

// entry 一条键值对
type entry struct {
	key   string
	value string
	ph    map[string]struct{}
	esc   map[string]int
}

// escOf 值里的换行/制表记号数量（只认 `\n`、`\t`、`\r` 三种控制记号）。
// 引号类转义不进比对：中文用「」、法语用 «»、德语用 „“，数量本就各异，比了是假红。
// `"a\nb"` → {"n":1}；误写成 `"a\\nb"`（双重转义）→ 这里记为 {"":1}，与基准形状不同，因而可辨。
func escOf(raw string) map[string]int {
	m := map[string]int{}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			continue
		}
		switch raw[i+1] {
		case 'n', 't', 'r':
			m[string(raw[i+1])]++
			i++
		case '\\':
			m["\\"]++
			i++
		}
	}
	return m
}

func entriesOf(path string) ([]entry, []string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var out []entry
	var suspicious []string
	parsed := map[string]bool{}
	for i, line := range strings.Split(string(b), "\n") {
		if m := kvRe.FindStringSubmatch(line); m != nil {
			ph := map[string]struct{}{}
			for _, name := range phRe.FindAllStringSubmatch(m[2], -1) {
				ph[name[1]] = struct{}{}
			}
			parsed[m[1]] = true
			out = append(out, entry{key: m[1], value: m[2], ph: ph, esc: escOf(m[2])})
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "//") {
			continue
		}
		if km := keyLineRe.FindStringSubmatch(line); km != nil && !parsed[km[1]] {
			suspicious = append(suspicious, fmt.Sprintf("%s:%d %s", path, i+1, trim))
		}
	}
	return out, suspicious, nil
}

func main() {
	// 八语（AGENTS §5.15：每种语言一个 locale 文件）：zh-CN 是基准，其余各与它对齐。
	// 两个可选参数覆盖路径（负向演练用）：go run scripts/check-i18n-keys.go <a> <b>
	zhPath, otherPath := "frontend/src/locales/zh-CN.ts", "frontend/src/locales/en-US.ts"
	paths := []string{zhPath, otherPath,
		"frontend/src/locales/zh-TW.ts", "frontend/src/locales/ko.ts", "frontend/src/locales/fr.ts",
		"frontend/src/locales/de.ts", "frontend/src/locales/es.ts", "frontend/src/locales/tr.ts"}
	if len(os.Args) == 3 {
		paths = []string{os.Args[1], os.Args[2]} // 演练模式只比对这两份
	}

	var base []entry
	var baseName string
	var problems []string
	for idx, path := range paths {
		es, sus, err := entriesOf(path)
		if err != nil {
			fail("读取 %s 失败: %v", path, err)
		}
		for _, x := range sus {
			problems = append(problems, fmt.Sprintf("%s 有未解析的条目行: %s", path, x))
		}
		if d := dupKeys(es); len(d) > 0 {
			problems = append(problems, fmt.Sprintf("%s 重复键: %v", path, d))
		}
		if idx == 0 {
			base, baseName = es, path
			continue
		}
		// 与基准比：仅基准有 / 仅此语言有 / 占位符集合
		bset := keySet(base)
		var onlyBase, onlyThis []string
		eset := keySet(es)
		for k := range bset {
			if _, ok := eset[k]; !ok {
				onlyBase = append(onlyBase, k)
			}
		}
		for k := range eset {
			if _, ok := bset[k]; !ok {
				onlyThis = append(onlyThis, k)
			}
		}
		sort.Strings(onlyBase)
		sort.Strings(onlyThis)
		if len(onlyBase) > 0 {
			problems = append(problems, fmt.Sprintf("仅 %s 有 (%d): %v", baseName, len(onlyBase), onlyBase))
		}
		if len(onlyThis) > 0 {
			problems = append(problems, fmt.Sprintf("仅 %s 有 (%d): %v", path, len(onlyThis), onlyThis))
		}
		thisMap := enMapOf(es)
		for _, e := range base {
			oe, ok := thisMap[e.key]
			if !ok {
				continue
			}
			var missBase, missThis []string
			for name := range e.ph {
				if _, ok := oe.ph[name]; !ok {
					missThis = append(missThis, name)
				}
			}
			for name := range oe.ph {
				if _, ok := e.ph[name]; !ok {
					missBase = append(missBase, name)
				}
			}
			if len(missBase) == 0 && len(missThis) == 0 {
				continue
			}
			sort.Strings(missBase)
			sort.Strings(missThis)
			problems = append(problems, fmt.Sprintf("%s（%s 缺 %v / %s 缺 %v）", e.key, baseName, missBase, path, missThis))
		}
		// 转义形状比对：`\n` 写成 `\\n` 会让界面显示字面反斜杠，占位符校验看不见，单独拦
		for _, e := range base {
			oe, ok := thisMap[e.key]
			if !ok {
				continue
			}
			if d := escDiff(e.esc, oe.esc); d != "" {
				problems = append(problems, fmt.Sprintf("%s 转义形状不一致：%s（基准记号 %v / 本语言记号 %v）",
					e.key, d, escKeys(e.esc), escKeys(oe.esc)))
			}
		}
	}

	if len(problems) == 0 {
		fmt.Printf("✓ i18n 对齐：%d 个语言各 %d 键（%s），键集相等、无重复、占位符集合相等、无未解析条目行\n", len(paths), len(base), strings.Join(paths, " / "))
		return
	}
	fmt.Println("✗ i18n 不对齐：")
	for _, p := range problems {
		fmt.Printf("  %s\n", p)
	}
	os.Exit(1)
}

func enMapOf(es []entry) map[string]entry {
	m := make(map[string]entry, len(es))
	for _, e := range es {
		m[e.key] = e
	}
	return m
}

func keySet(es []entry) map[string]struct{} {
	m := make(map[string]struct{}, len(es))
	for _, e := range es {
		m[e.key] = struct{}{}
	}
	return m
}

// escKeys 排序后的记号列表，仅用于报错信息
func escKeys(m map[string]int) []string {
	var out []string
	for k, n := range m {
		out = append(out, fmt.Sprintf("backslash-%s x%d", k, n))
	}
	sort.Strings(out)
	return out
}

// escDiff 两组转义记号是否逐符号等量；不等时回一句人话说明差在哪。
// 抓的就是「把 \n 写成 \\n」这一类：记号本身变成反斜杠，界面于是显示出一个字面反斜杠。
func escDiff(base, other map[string]int) string {
	var parts []string
	for k, n := range base {
		if o := other[k]; o != n {
			parts = append(parts, fmt.Sprintf("反斜杠+%s 基准 %d 个、本语言 %d 个", k, n, o))
		}
	}
	for k, n := range other {
		if _, ok := base[k]; !ok {
			parts = append(parts, fmt.Sprintf("本语言多出 反斜杠+%s x%d", k, n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "；")
}

func dupKeys(es []entry) []string {
	seen := map[string]int{}
	for _, e := range es {
		seen[e.key]++
	}
	var d []string
	for k, n := range seen {
		if n > 1 {
			d = append(d, k)
		}
	}
	sort.Strings(d)
	return d
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(2)
}
