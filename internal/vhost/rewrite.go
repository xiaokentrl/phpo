// 伪静态预设：REWRITE_PRESETS（21 种）后端化，rule 原文逐字对齐 SSOT
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

// presets REWRITE_PRESETS 二十一键；顺序与原型一致
var presets = []RewritePreset{
	{Key: "none", Icon: "📄", NameKey: "rw.none", Tag: "Static", Rule: "# Rewrite disabled\nlocation / {\n    try_files $uri $uri/ =404;\n}"},
	{Key: "laravel", Icon: "🔺", NameKey: "rw.laravel", Tag: "PHP", Rule: "# Laravel 5+ / Lumen\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
	{Key: "thinkphp", Icon: "🐘", NameKey: "rw.thinkphp", Tag: "PHP", Rule: "# ThinkPHP 3/5/6\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /index.php?s=$1 last;\n    }\n}"},
	{Key: "yii2", Icon: "🟦", NameKey: "rw.yii2", Tag: "PHP", Rule: "# Yii2 Advanced / Basic\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}"},
	{Key: "thinkcmf", Icon: "🎯", NameKey: "rw.thinkcmf", Tag: "PHP", Rule: "# ThinkCMF 5.x / 6.x\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?s=$1 last;\n    }\n}"},
	{Key: "ci", Icon: "🔥", NameKey: "rw.ci", Tag: "PHP", Rule: "# CodeIgniter 3/4\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
	{Key: "symfony", Icon: "⚫", NameKey: "rw.symfony", Tag: "PHP", Rule: "# Symfony 5+ (public dir)\nlocation / {\n    try_files $uri /index.php$is_args$args;\n}"},
	{Key: "wordpress", Icon: "📰", NameKey: "rw.wordpress", Tag: "CMS", Rule: "# WordPress 5/6\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}"},
	{Key: "drupal", Icon: "💧", NameKey: "rw.drupal", Tag: "CMS", Rule: "# Drupal 9/10/11\nlocation / {\n    try_files $uri /index.php?$query_string;\n}"},
	{Key: "joomla", Icon: "🟧", NameKey: "rw.joomla", Tag: "CMS", Rule: "# Joomla 4/5\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}"},
	{Key: "mediawiki", Icon: "📚", NameKey: "rw.mediawiki", Tag: "CMS", Rule: "# MediaWiki 1.35+\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
	{Key: "dokuwiki", Icon: "📝", NameKey: "rw.dokuwiki", Tag: "CMS", Rule: "# DokuWiki\nlocation / {\n    index index.php doku.php;\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /doku.php?id=$1&$args last;\n    }\n}"},
	{Key: "typecho", Icon: "🌱", NameKey: "rw.typecho", Tag: "CMS", Rule: "# Typecho 1.2\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /index.php$1 last;\n    }\n}"},
	{Key: "pbootcms", Icon: "🏗️", NameKey: "rw.pbootcms", Tag: "CMS", Rule: "# PbootCMS 3.x\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?p=$1 last;\n    }\n}"},
	{Key: "discuz", Icon: "💬", NameKey: "rw.discuz", Tag: "Forum", Rule: "# Discuz! X3.4 / X3.5\nlocation / {\n    rewrite ^(.*)-htm-(.*)$ $1.php?$2 last;\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /forum.php last;\n    }\n}"},
	{Key: "phpbb", Icon: "📢", NameKey: "rw.phpbb", Tag: "Forum", Rule: "# phpBB 3.3\nlocation / {\n    try_files $uri $uri/ /app.php?$query_string;\n}"},
	{Key: "opencart", Icon: "🛒", NameKey: "rw.opencart", Tag: "Shop", Rule: "# OpenCart 3/4\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.+)$ /index.php?_route_=$1 last;\n    }\n}"},
	{Key: "spa", Icon: "🧩", NameKey: "rw.spa", Tag: "SPA", Rule: "# Single Page App (Vue / React / Angular)\nlocation / {\n    try_files $uri $uri/ /index.html;\n}"},
	{Key: "yii1", Icon: "🟨", NameKey: "rw.yii1", Tag: "PHP", Rule: "# Yii 1.1\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?r=$1 last;\n    }\n}"},
	{Key: "cakephp", Icon: "🍰", NameKey: "rw.cakephp", Tag: "PHP", Rule: "# CakePHP 4/5\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}"},
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
