// 模板引擎嵌入层：把 templates/ 下的服务配置与 vhost 模板随二进制打包（go:embed）
// §5.3：模板按服务种类分目录，不按版本分；版本仅在渲染时作为变量注入
package template

import "embed"

// templateFS 嵌入 templates/ 全部 *.tmpl 文件
//
//go:embed templates
var templateFS embed.FS
