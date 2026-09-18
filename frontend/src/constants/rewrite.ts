// 重写规则预设：逐字迁移自原型 REWRITE_PRESETS（§0.3：REWRITE_PRESETS 数 = 9）
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
  custom: { icon: '✏️', nameKey: 'rw.custom', tag: 'Custom', rule: '# Custom rewrite rules\nlocation / {\n    try_files $uri $uri/ /index.php?$query_string;\n}' },
}

export const REWRITE_KEYS = Object.keys(REWRITE_PRESETS)
