//go:build ignore

// check-i18n-keys.go —— i18n 键对齐校验（TX02，T103 验收）
// 目的：确保 zh-CN.ts 与 en-US.ts 键集合完全相等（切语言无缺键）。
// 运行：go run scripts/check-i18n-keys.go
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
)

// 匹配键行：  "some.key": "value"
var keyRe = regexp.MustCompile(`^\s*"((?:[^"\\]|\\.)*)"\s*:`)

func keysOf(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range splitLines(string(b)) {
		if m := keyRe.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

func main() {
	zh, err := keysOf("frontend/src/locales/zh-CN.ts")
	if err != nil {
		fail("读取 zh-CN.ts 失败: %v", err)
	}
	en, err := keysOf("frontend/src/locales/en-US.ts")
	if err != nil {
		fail("读取 en-US.ts 失败: %v", err)
	}
	zset := toSet(zh)
	eset := toSet(en)

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

	dup := dupKeys(zh)
	if len(onlyZh) == 0 && len(onlyEn) == 0 && len(dup) == 0 {
		fmt.Printf("✓ i18n 键对齐：zh-CN / en-US 各 %d 键，集合相等、无重复\n", len(zh))
		return
	}
	fmt.Println("✗ i18n 键不对齐：")
	if len(onlyZh) > 0 {
		fmt.Printf("  仅 zh-CN 有 (%d): %v\n", len(onlyZh), onlyZh)
	}
	if len(onlyEn) > 0 {
		fmt.Printf("  仅 en-US 有 (%d): %v\n", len(onlyEn), onlyEn)
	}
	if len(dup) > 0 {
		fmt.Printf("  zh-CN 重复键: %v\n", dup)
	}
	os.Exit(1)
}

func dupKeys(ks []string) []string {
	seen := map[string]int{}
	var d []string
	for _, k := range ks {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			d = append(d, k)
		}
	}
	sort.Strings(d)
	return d
}

func toSet(ks []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ks))
	for _, k := range ks {
		m[k] = struct{}{}
	}
	return m
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(2)
}
