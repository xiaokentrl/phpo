//go:build ignore

// check-i18n-keys.go —— i18n 键对齐校验（TX02，T103 验收；v2.9.16 起含占位符集合校验，对齐 AGENTS §6 第 1 项）
// 目的：
//  1. zh-CN.ts 与 en-US.ts 键集合完全相等（切语言无缺键）；
//  2. 每条键的占位符集合（{name} 记号）两侧相等——英文把 {n} 写成 {count} 这类漂移在这里拦下，
//     否则英文界面渲染时 {n} 原样漏出、参数悄悄丢掉；
//  3. 条目行文法唯一：双引号键 + 双引号值。长得像条目却按别的方式写（模板字符串值、单引号值、
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
			out = append(out, entry{key: m[1], value: m[2], ph: ph})
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
	// 三语（AGENTS §5.15：每种语言一个 locale 文件）：zh-CN 是基准，en-US / zh-TW 各与它对齐。
	// 两个可选参数覆盖路径（负向演练用）：go run scripts/check-i18n-keys.go <a> <b>
	zhPath, otherPath := "frontend/src/locales/zh-CN.ts", "frontend/src/locales/en-US.ts"
	paths := []string{zhPath, otherPath, "frontend/src/locales/zh-TW.ts"}
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
