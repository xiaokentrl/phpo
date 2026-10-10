//go:build ignore

// check-i18n-keys.go —— i18n 键对齐校验（TX02，T103 验收；v2.9.16 起含占位符集合校验，对齐 AGENTS §6 第 1 项）
// 目的：
//  1. zh-CN.ts 与 en-US.ts 键集合完全相等（切语言无缺键）；
//  2. 每条键的占位符集合（{name} 记号）两侧相等——英文把 {n} 写成 {count} 这类漂移在这里拦下，
//     否则英文界面渲染时 {n} 原样漏出、参数悄悄丢掉。
//
// 运行：go run scripts/check-i18n-keys.go
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

// 占位符记号：{name}——与前端渲染的 split('{'+k+'}') 一致
var phRe = regexp.MustCompile(`\{(\w+)\}`)

// entry 一条键值对
type entry struct {
	key   string
	value string
	ph    map[string]struct{}
}

func entriesOf(path string) ([]entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, line := range strings.Split(string(b), "\n") {
		if m := kvRe.FindStringSubmatch(line); m != nil {
			ph := map[string]struct{}{}
			for _, name := range phRe.FindAllStringSubmatch(m[2], -1) {
				ph[name[1]] = struct{}{}
			}
			out = append(out, entry{key: m[1], value: m[2], ph: ph})
		}
	}
	return out, nil
}

func main() {
	zh, err := entriesOf("frontend/src/locales/zh-CN.ts")
	if err != nil {
		fail("读取 zh-CN.ts 失败: %v", err)
	}
	en, err := entriesOf("frontend/src/locales/en-US.ts")
	if err != nil {
		fail("读取 en-US.ts 失败: %v", err)
	}
	zset := keySet(zh)
	eset := keySet(en)

	var onlyZh, onlyEn []string
	for k := range zset {
		if _, ok := eset[k]; !ok {
			onlyZh = append(onlyZh, k)
		}
	}
	for k := range eset {
		if _, ok := zset[k]; !ok {
			onlyEn = append(onlyEn, k)
		}
	}
	sort.Strings(onlyZh)
	sort.Strings(onlyEn)

	dupZh, dupEn := dupKeys(zh), dupKeys(en)

	// 占位符集合逐键比对（两侧都有的键才比）
	var phDiffs []string
	enMap := enMapOf(en)
	for _, e := range zh {
		oe, ok := enMap[e.key]
		if !ok {
			continue
		}
		var missZh, missEn []string
		for name := range e.ph {
			if _, ok := oe.ph[name]; !ok {
				missEn = append(missEn, name)
			}
		}
		for name := range oe.ph {
			if _, ok := e.ph[name]; !ok {
				missZh = append(missZh, name)
			}
		}
		if len(missZh) == 0 && len(missEn) == 0 {
			continue
		}
		sort.Strings(missZh)
		sort.Strings(missEn)
		phDiffs = append(phDiffs, fmt.Sprintf("%s（zh 缺 %v / en 缺 %v）", e.key, missZh, missEn))
	}

	if len(onlyZh) == 0 && len(onlyEn) == 0 && len(dupZh) == 0 && len(dupEn) == 0 && len(phDiffs) == 0 {
		fmt.Printf("✓ i18n 对齐：zh-CN / en-US 各 %d 键，键集相等、无重复、占位符集合相等\n", len(zh))
		return
	}
	fmt.Println("✗ i18n 不对齐：")
	if len(onlyZh) > 0 {
		fmt.Printf("  仅 zh-CN 有 (%d): %v\n", len(onlyZh), onlyZh)
	}
	if len(onlyEn) > 0 {
		fmt.Printf("  仅 en-US 有 (%d): %v\n", len(onlyEn), onlyEn)
	}
	if len(dupZh) > 0 {
		fmt.Printf("  zh-CN 重复键: %v\n", dupZh)
	}
	if len(dupEn) > 0 {
		fmt.Printf("  en-US 重复键: %v\n", dupEn)
	}
	if len(phDiffs) > 0 {
		fmt.Println("  占位符集合不一致:")
		for _, d := range phDiffs {
			fmt.Printf("    %s\n", d)
		}
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
