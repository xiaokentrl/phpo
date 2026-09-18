// 镜像引用解析/拼接纯函数："name:tag" / "host/repo:tag" / "name@sha256:digest"
package dockerutil

import "strings"

// ImageRef 一个镜像引用的分解；Tag 为空表示未显式指定
type ImageRef struct {
	Name   string
	Tag    string
	Digest string
}

const defaultTag = "latest"

// ParseImageRef 解析镜像引用字符串。仅做轻量拆分，不联网、不查 manifest。
func ParseImageRef(ref string) ImageRef {
	r := ImageRef{}
	// 先剥离 digest
	if i := strings.Index(ref, "@"); i >= 0 {
		r.Digest = ref[i+1:]
		ref = ref[:i]
	}
	// 再剥离 tag：只有 name 部分最后一个 ":" 且其后不含 "/" 才是 tag
	r.Name = ref
	if i := strings.LastIndex(ref, ":"); i >= 0 && !strings.Contains(ref[i+1:], "/") {
		r.Name = ref[:i]
		r.Tag = ref[i+1:]
	}
	return r
}

// String 反解析回引用串；无 tag 且无 digest 时补 latest
func (r ImageRef) String() string {
	s := r.Name
	switch {
	case r.Tag != "" && r.Digest != "":
		s += ":" + r.Tag + "@" + r.Digest
	case r.Tag != "":
		s += ":" + r.Tag
	case r.Digest != "":
		s += "@" + r.Digest
	default:
		s += ":" + defaultTag
	}
	return s
}

// TagOrDefault 返回显式 tag，缺省为 latest
func (r ImageRef) TagOrDefault() string {
	if r.Tag == "" {
		return defaultTag
	}
	return r.Tag
}

// FormatRef 由仓库名与 tag 拼接引用（tag 为空回退 latest）
func FormatRef(name, tag string) string {
	if tag == "" {
		tag = defaultTag
	}
	return name + ":" + tag
}
