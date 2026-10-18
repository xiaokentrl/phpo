// 重写规则预设：逐字迁移自原型 REWRITE_PRESETS（§0.3：REWRITE_PRESETS 数 = 21）
export interface RewritePreset {
  icon: string
  nameKey: string
  tag: string
  rule: string
}

export const REWRITE_PRESETS: Record<string, RewritePreset> = {
  none: { icon: '📄', nameKey: 'rw.none', tag: 'Static', rule: '# Rewrite disabled\nlocation / {\n    try_files $uri $uri/ =404;\n}' },
  laravel: { icon: '🔺', nameKey: 'rw.laravel', tag: 'PHP', rule: '# Laravel 5+ / Lumen\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}' },
  thinkphp: { icon: '🐘', nameKey: 'rw.thinkphp', tag: 'PHP', rule: '# ThinkPHP 3/5/6\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /index.php?s=$1 last;\n    }\n}' },
  yii2: { icon: '🟦', nameKey: 'rw.yii2', tag: 'PHP', rule: '# Yii2 Advanced / Basic\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}' },
  thinkcmf: { icon: '🎯', nameKey: 'rw.thinkcmf', tag: 'PHP', rule: '# ThinkCMF 5.x / 6.x\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?s=$1 last;\n    }\n}' },
  ci: { icon: '🔥', nameKey: 'rw.ci', tag: 'PHP', rule: '# CodeIgniter 3/4\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}' },
  symfony: { icon: '⚫', nameKey: 'rw.symfony', tag: 'PHP', rule: '# Symfony 5+ (public dir)\nlocation / {\n    try_files $uri /index.php$is_args$args;\n}' },
  wordpress: { icon: '📰', nameKey: 'rw.wordpress', tag: 'CMS', rule: '# WordPress 5/6\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}' },
  drupal: { icon: '💧', nameKey: 'rw.drupal', tag: 'CMS', rule: '# Drupal 9/10/11\nlocation / {\n    try_files $uri /index.php?$query_string;\n}' },
  joomla: { icon: '🟧', nameKey: 'rw.joomla', tag: 'CMS', rule: '# Joomla 4/5\nlocation / {\n    try_files $uri $uri/ /index.php?$args;\n}' },
  mediawiki: { icon: '📚', nameKey: 'rw.mediawiki', tag: 'CMS', rule: '# MediaWiki 1.35+\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}' },
  dokuwiki: { icon: '📝', nameKey: 'rw.dokuwiki', tag: 'CMS', rule: '# DokuWiki\nlocation / {\n    index index.php doku.php;\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /doku.php?id=$1&$args last;\n    }\n}' },
  typecho: { icon: '🌱', nameKey: 'rw.typecho', tag: 'CMS', rule: '# Typecho 1.2\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /index.php$1 last;\n    }\n}' },
  pbootcms: { icon: '🏗️', nameKey: 'rw.pbootcms', tag: 'CMS', rule: '# PbootCMS 3.x\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?p=$1 last;\n    }\n}' },
  discuz: { icon: '💬', nameKey: 'rw.discuz', tag: 'Forum', rule: '# Discuz! X3.4 / X3.5\nlocation / {\n    rewrite ^(.*)-htm-(.*)$ $1.php?$2 last;\n    if (!-e $request_filename) {\n        rewrite ^(.*)$ /forum.php last;\n    }\n}' },
  phpbb: { icon: '📢', nameKey: 'rw.phpbb', tag: 'Forum', rule: '# phpBB 3.3\nlocation / {\n    try_files $uri $uri/ /app.php?$query_string;\n}' },
  opencart: { icon: '🛒', nameKey: 'rw.opencart', tag: 'Shop', rule: '# OpenCart 3/4\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.+)$ /index.php?_route_=$1 last;\n    }\n}' },
  spa: { icon: '🧩', nameKey: 'rw.spa', tag: 'SPA', rule: '# Single Page App (Vue / React / Angular)\nlocation / {\n    try_files $uri $uri/ /index.html;\n}' },
  yii1: { icon: '🟨', nameKey: 'rw.yii1', tag: 'PHP', rule: '# Yii 1.1\nlocation / {\n    if (!-e $request_filename) {\n        rewrite ^/(.*)$ /index.php?r=$1 last;\n    }\n}' },
  cakephp: { icon: '🍰', nameKey: 'rw.cakephp', tag: 'PHP', rule: '# CakePHP 4/5\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}' },
  custom: { icon: '✏️', nameKey: 'rw.custom', tag: 'Custom', rule: '# Custom rewrite rules\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}' },
}

export const REWRITE_KEYS = Object.keys(REWRITE_PRESETS)
