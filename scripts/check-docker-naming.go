//go:build ignore

// check-docker-naming.go —— Docker 资源命名规范校验（TX03，T304 验收）
// 目的：确保所有 phpo 托管资源名走 dockerutil 命名助手，禁止裸写字面量绕过 phpo- 命名空间。
// 运行：go run scripts/check-docker-naming.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 资源名字面量形态：phpo- 前缀 或 固定网络名 phpo-network
var resourceLiteral = regexp.MustCompile(`^phpo-[a-z0-9_.-]*$`)

// naming.go 与 dockerutil 包自身允许定义这些常量/构造逻辑，扫描时排除
// 以下文件仅把 "phpo-" 前缀用作文件系统临时名/下载包名/hosts 临时文件/mock 事件演示数据，
// 完全不创建 Docker 资源（不经 dockerutil 助手），故豁免；新增 Docker 资源名的文件不得加入。
var allowSuffixes = []string{
	filepath.Join("pkg", "dockerutil", "naming.go"),
	filepath.Join("internal", "app", "mock_emitter.go"),               // M1 mock 事件演示载荷里的假资源名字符串
	filepath.Join("internal", "service", "backup_service.go"),         // os.MkdirTemp 前缀 "phpo-restore-"
	filepath.Join("internal", "updater", "update.go"),                 // 下载包文件名 "phpo-"+version
	filepath.Join("internal", "vhost", "hosts", "elevate_windows.go"), // hosts 提权临时文件名 "phpo-hosts.txt"
}

type violation struct {
	file string
	line int
	lit  string
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail("获取工作目录失败: %v", err)
	}
	var vios []violation
	dirs := []string{"internal", "pkg"}
	for _, d := range dirs {
		base := filepath.Join(root, d)
		walkErr := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if allowed(path) {
				return nil
			}
			vios = append(vios, scanFile(path)...)
			return nil
		})
		if walkErr != nil {
			fail("遍历 %s 失败: %v", d, walkErr)
		}
	}
	if len(vios) > 0 {
		fmt.Printf("✗ 发现 %d 处裸写 Docker 资源名字面量（应改用 dockerutil 助手）：\n", len(vios))
		for _, v := range vios {
			rel, _ := filepath.Rel(root, v.file)
			fmt.Printf("  %s:%d  %q\n", rel, v.line, v.lit)
		}
		os.Exit(1)
	}
	fmt.Println("✓ 命名规范校验通过：无裸写资源名字面量，phpo- 命名空间统一由 dockerutil 管理")
}

func allowed(path string) bool {
	for _, s := range allowSuffixes {
		if strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(s)) {
			return true
		}
	}
	return false
}

func scanFile(path string) []violation {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil
	}
	var out []violation
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s := strings.Trim(lit.Value, "\"`")
		// 只拦截「看起来像资源名」却写成裸字面量的；含 { } 的模板/格式串放行
		if resourceLiteral.MatchString(s) && !strings.ContainsAny(s, "{}") {
			out = append(out, violation{path, fset.Position(lit.Pos()).Line, s})
		}
		return true
	})
	return out
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(2)
}
