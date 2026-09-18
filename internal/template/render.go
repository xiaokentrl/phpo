// 模板渲染层：把 templates/ 下的模板按服务种类/版本渲染为配置内容
// 直译原型 DEFAULT_CONFIGS（5 服务 7 文件）与 defaultVhost；内容逐字对齐 SSOT
package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// File 对应原型 DEFAULT_CONFIGS 单条目 {name, path, content}
type File struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// kindFiles 记录每种服务的配置文件名与顺序（与原型一致：5 服务共 7 文件）
// 已知限制（继承自 SSOT）：php.ini 的 session.save_path 逐字硬编码为 "tcp://phpo-redis-8:6379"，
// 隐含依赖版本 8 的 redis 容器；若安装其它版本 redis 会话将连不上。真实修复需在服务层按实际
// redis 容器注入该主机（并同步更新原型/总纲），不属本层「逐字对齐」范围，故保留原样。
var kindFiles = map[string][]string{
	"php":   {"php.ini", "php-fpm.conf"},
	"mysql": {"my.cnf"},
	"pgsql": {"postgresql.conf", "pg_hba.conf"},
	"redis": {"redis.conf"},
	"nginx": {"nginx.conf"},
}

// Kinds 返回受支持的服务种类（校验脚本用）
func Kinds() []string { return []string{"php", "mysql", "pgsql", "redis", "nginx"} }

// configData 配置模板渲染变量
type configData struct {
	Version    string // 版本号原样（含点）
	VersionEnv string // 版本号规整为合法环境变量名片段，供 redis requirepass
}

func dataFor(version string) configData {
	return configData{Version: version, VersionEnv: envVarName(version)}
}

// envVarName 生成合法 POSIX 环境变量名片段：先按原型语义去点，再把剩余非法字符（如 '-'）转下划线。
// 常规版本（如 8 / 8.4 / 17）逐字与原型一致；仅对含非标识符字符的版本做兼容。
func envVarName(version string) string {
	var b strings.Builder
	for _, r := range strings.ReplaceAll(version, ".", "") {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// FilesFor 渲染某服务版本的全部默认配置文件（getDefaultFiles 直译）；未知种类返回空。
func FilesFor(kind, version string) ([]File, error) {
	names := kindFiles[kind]
	out := make([]File, 0, len(names))
	for _, name := range names {
		content, err := render(name+".tmpl", dataFor(version))
		if err != nil {
			return nil, fmt.Errorf("渲染 %s/%s 失败: %w", kind, name, err)
		}
		out = append(out, File{
			Name:    name,
			Path:    kind + "/" + version + "/conf/" + name,
			Content: content,
		})
	}
	return out, nil
}

// VhostInput 站点 vhost 渲染入参；Upstream 为 php-{version}-fpm（不含端口）。
// ContainerRoot 与 Rule 由调用方（vhost 管理器）解析完成，本层只做拼装与缩进。
type VhostInput struct {
	Domain        string
	Port          int
	ContainerRoot string
	Upstream      string
	Rule          string // 已解析的伪静态规则原文（未缩进）
}

// RenderVhost 渲染 vhost.conf（defaultVhost 直译：规则整块按行缩进 4 空格）
func RenderVhost(in VhostInput) (string, error) {
	d := struct {
		Domain    string
		Port      int
		Root      string
		Upstream  string
		RuleBlock string
	}{
		Domain:    in.Domain,
		Port:      in.Port,
		Root:      in.ContainerRoot,
		Upstream:  in.Upstream,
		RuleBlock: indentBlock(in.Rule),
	}
	return render("vhost.conf.tmpl", d)
}

// indentBlock 每行前缀 4 空格（原型 rule.split('\n').map(l => '    '+l).join('\n')）
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// tpl 解析全部嵌入模板；ParseFS 以文件基名命名模板，8 个基名互不相同
var tpl = template.Must(
	template.New("").ParseFS(templateFS, "templates/*.tmpl", "templates/*/*.tmpl"),
)

func render(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
