// 伪静态预设：REWRITE_PRESETS（9 种）后端化，rule 原文逐字对齐 SSOT
package vhost

import "strings"

// RewritePreset 单个伪静态预设（icon/nameKey/tag 供前端展示，rule 为写入 vhost 的原文）
type RewritePreset struct {
	Key     string
	Icon    string
	NameKey string
	Tag     string
	Rule    string
}

// presets REWRITE_PRESETS 九键；顺序与原型一致
var presets = []RewritePreset{
	{Key: "none", Icon: "📄", NameKey: "rw.none", Tag: "Static", Rule: "# Rewrite disabled\nlocation / {\n    try_files $uri $uri/ =404;\n}"},
	{Key: "laravel", Icon: "🔺", NameKey: "rw.laravel", Tag: "PHP", Rule: "# Laravel 5+ / Lumen\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
	{Key: "thinkphp", Icon: "🐘", NameKey: "rw.thinkphp", Tag: "PHP", Rule: "# ThinkPHP 3/5/6\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /index.php?s=$1 last;\n    }\n}"},
	{Key: "yii2", Icon: "🟦", NameKey: "rw.yii2", Tag: "PHP", Rule: "# Yii2 Advanced / Basic\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}"},
	{Key: "thinkcmf", Icon: "🎯", NameKey: "rw.thinkcmf", Tag: "PHP", Rule: "# ThinkCMF 5.x / 6.x\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?s=$1 last;\n    }\n}"},
	{Key: "ci", Icon: "🔥", NameKey: "rw.ci", Tag: "PHP", Rule: "# CodeIgniter 3/4\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
	{Key: "symfony", Icon: "⚫", NameKey: "rw.symfony", Tag: "PHP", Rule: "# Symfony 5+ (public dir)\nlocation / {\n    try_files $uri /index.php$is_args$args;\n}"},
	{Key: "wordpress", Icon: "📰", NameKey: "rw.wordpress", Tag: "CMS", Rule: "# WordPress 5/6\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}"},
	{Key: "custom", Icon: "✏️", NameKey: "rw.custom", Tag: "Custom", Rule: "# Custom rewrite rules\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
}

// Presets 返回全部伪静态预设副本（顺序稳定）
func Presets() []RewritePreset {
	out := make([]RewritePreset, len(presets))
	copy(out, presets)
	return out
}

var presetByKey = func() map[string]RewritePreset {
	m := make(map[string]RewritePreset, len(presets))
	for _, p := range presets {
		m[p.Key] = p
	}
	return m
}()

// RuleFor 复刻 defaultVhost 取规则：custom 且给定非空 customRule 用之，否则回落预设 rule，未知键回落 none
func RuleFor(key, customRule string) string {
	if key == "custom" && strings.TrimSpace(customRule) != "" {
		return customRule
	}
	if p, ok := presetByKey[key]; ok {
		return p.Rule
	}
	return presetByKey["none"].Rule
}
