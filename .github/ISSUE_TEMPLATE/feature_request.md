name: 功能请求
description: 提出新功能或改进建议
title: "[feature] "
labels: [enhancement]
body:
  - type: markdown
    attributes:
      value: |
        项目遵循「最小限制原则」：除 AGENTS.md §3.3 的 8 条硬红线外，限制一律放开或降级为警告。
  - type: textarea
    id: problem
    attributes:
      label: 想解决的问题 / 使用场景
      description: 对应哪类用户画像（外包开发者 / 团队负责人 / 学习者 / 高级用户 / CI / 内网离线）。
    validations:
      required: true
  - type: textarea
    id: proposal
    attributes:
      label: 期望的做法
    validations:
      required: true
  - type: dropdown
    id: area
    attributes:
      label: 涉及模块
      multiple: true
      options:
        - 站点 / vhost
        - PHP 版本切换
        - 数据服务（MySQL / PG / Redis）
        - 离线缓存
        - Docker 清洁 / 回收站
        - 备份 / 恢复
        - 应用升级
        - 前端界面 / i18n / 主题
        - 打包发布
  - type: checkboxes
    id: redline
    attributes:
      label: 硬红线自查
      options:
        - label: 本请求不要求放宽 §3.3 的 8 条硬红线
