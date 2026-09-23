//go:build ignore

// check-cache-manifest.go —— 缓存清单 manifest.json schema 校验（§5.14.2 / §11.3）
// 目的：manifest.json 的 snake_case 键是冻结契约（跨版本兼容），任何 Go 侧字段改名/漏 tag
//
//	都会破坏已落盘清单的读取。此脚本静态解析 model，确保三张结构体的 json tag 与权威键集逐字一致。
//
// 运行：go run scripts/check-cache-manifest.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
)

// 权威键集（AGENTS.md §5.14.2 manifest.json 结构，逐字冻结）
var frozen = map[string][]string{
	"CacheManifest":   {"schema_version", "kind", "version", "created_at", "updated_at", "image", "extensions_image", "apk", "pecl"},
	"ManifestImage":   {"name", "digest", "size", "sha256", "cached_at"},
	"ManifestPackage": {"name", "sha256", "size", "cached_at"},
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail("获取工作目录失败: %v", err)
	}
	path := root + "/internal/model/cache_entry.go"
	got, err := parseStructs(path)
	if err != nil {
		fail("解析 %s 失败: %v", path, err)
	}
	var bad []string
	for name, want := range frozen {
		tags, ok := got[name]
		if !ok {
			bad = append(bad, fmt.Sprintf("  结构体 %s 缺失", name))
			continue
		}
		if !equalSet(tags, want) {
			bad = append(bad, fmt.Sprintf("  %s 键集漂移：期望 %v，实得 %v", name, want, tags))
		}
	}
	if len(bad) > 0 {
		fmt.Printf("✗ 缓存清单 schema 与 §5.14.2 冻结契约不符：\n%s\n", strings.Join(bad, "\n"))
		os.Exit(1)
	}
	fmt.Println("✓ 缓存清单 schema 校验通过：CacheManifest/ManifestImage/ManifestPackage 键集与 §5.14.2 逐字一致")
}

// parseStructs 提取指定文件中每个 struct 类型字段的首个 json tag 名（去 omitempty）
func parseStructs(path string) (map[string][]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			var tags []string
			for _, fld := range st.Fields.List {
				if fld.Tag == nil || len(fld.Names) == 0 {
					continue
				}
				if name := jsonTagName(fld.Tag); name != "" {
					tags = append(tags, name)
				}
			}
			out[ts.Name.Name] = tags
		}
	}
	return out, nil
}

func jsonTagName(tag *ast.BasicLit) string {
	raw, err := strconv.Unquote(tag.Value)
	if err != nil {
		return ""
	}
	for _, part := range strings.Split(raw, " ") {
		if !strings.HasPrefix(part, `json:"`) {
			continue
		}
		v := strings.TrimSuffix(strings.TrimPrefix(part, `json:"`), `"`)
		return strings.Split(v, ",")[0]
	}
	return ""
}

func equalSet(got, want []string) bool {
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(2)
}
