//go:build ignore

// check-ext-catalog.go —— PHP 扩展目录与后端分类对账（Request G · G1）
// 目的：frontend/src/constants/ext.ts 是扩展的唯一清单，其 tool 必须与 config.ClassifyExt 的判定一致。
// 分类错一个扩展，容器里就跑错命令（内置名走 pecl / 第三方名走 docker-php-ext-install），必然编译失败。
// 运行：go run scripts/check-ext-catalog.go
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"phpo/internal/config"
)

type catalogEntry struct {
	name string
	tool string
}

var (
	catalogBlock = regexp.MustCompile(`(?s)EXT_CATALOG:\s*ExtDef\[\]\s*=\s*\[(.*?)\n\]`)
	entryObject  = regexp.MustCompile(`(?s)\{[^{}]*\}`)
	fieldName    = regexp.MustCompile(`name:\s*'([^']+)'`)
	fieldTool    = regexp.MustCompile(`tool:\s*'([^']+)'`)
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "获取工作目录失败:", err)
		os.Exit(1)
	}
	path := "frontend/src/constants/ext.ts"
	src, err := os.ReadFile(root + "/" + path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取扩展目录失败:", err)
		os.Exit(1)
	}
	block := catalogBlock.FindStringSubmatch(string(src))
	if block == nil {
		fmt.Fprintf(os.Stderr, "%s 里找不到 EXT_CATALOG 数组，对账失效\n", path)
		os.Exit(1)
	}

	var entries []catalogEntry
	seen := map[string]bool{}
	var bad []string
	for _, obj := range entryObject.FindAllString(block[1], -1) {
		n := fieldName.FindStringSubmatch(obj)
		tl := fieldTool.FindStringSubmatch(obj)
		if n == nil || tl == nil {
			bad = append(bad, "条目缺少 name/tool 字段: "+strings.TrimSpace(obj))
			continue
		}
		name := n[1]
		if seen[name] {
			bad = append(bad, fmt.Sprintf("扩展 %s 重复登记", name))
			continue
		}
		seen[name] = true
		entries = append(entries, catalogEntry{name, tl[1]})
	}

	wantBuiltin := map[string]bool{"builtin": true, "pecl": true}
	for _, e := range entries {
		if !wantBuiltin[e.tool] {
			bad = append(bad, fmt.Sprintf("%s: tool 只能是 builtin 或 pecl，实得 %q", e.name, e.tool))
			continue
		}
		if got := string(config.ClassifyExt(e.name)); got != e.tool {
			bad = append(bad, fmt.Sprintf("%s: 目录标 %s，后端 ClassifyExt 判 %s", e.name, e.tool, got))
		}
		if !config.ValidateExt(e.name) {
			bad = append(bad, fmt.Sprintf("%s: 未过后端扩展名校验（ValidateExt）", e.name))
		}
	}
	// 目录为空即正则失配（改过写法就会静默通过），必须当作失败
	if len(entries) < 40 {
		bad = append(bad, fmt.Sprintf("目录条目数 %d 异常偏低，疑似解析失配", len(entries)))
	}

	if len(bad) > 0 {
		fmt.Printf("扩展目录对账失败（%d 项）:\n", len(bad))
		for _, l := range bad {
			fmt.Println("  -", l)
		}
		os.Exit(1)
	}
	fmt.Printf("扩展目录对账通过：%d 个扩展的分类与 config.ClassifyExt 一致\n", len(entries))
}
