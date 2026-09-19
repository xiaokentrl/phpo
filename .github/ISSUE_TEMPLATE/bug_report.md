name: 问题反馈
description: 报告一个 bug 或异常行为
title: "[bug] "
labels: [bug]
body:
  - type: markdown
    attributes:
      value: |
        感谢反馈。请尽量给出可复现的步骤。phpo 是仅 GUI 的桌面应用（无 CLI）。
  - type: textarea
    id: describe
    attributes:
      label: 问题描述
      description: 发生了什么、期望是什么。
    validations:
      required: true
  - type: textarea
    id: repro
    attributes:
      label: 复现步骤
      placeholder: |
        1. 打开 …
        2. 点击 …
        3. 看到 …
    validations:
      required: true
  - type: textarea
    id: env
    attributes:
      label: 运行环境
      value: |
        - 操作系统：Windows / macOS / Linux（版本）
        - phpo 版本：
        - Docker 版本：
        - 涉及服务/版本（如 PHP 8.4、MySQL 8.4）：
    validations:
      required: true
  - type: textarea
    id: logs
    attributes:
      label: 日志抽屉输出
      description: 复制任务日志抽屉中的相关行（可含等效命令展示）。
      render: shell
  - type: checkboxes
    id: offline
    attributes:
      label: 离线缓存相关
      options:
        - label: 问题发生在弱网/内网/断网环境，或涉及镜像/扩展缓存命中
