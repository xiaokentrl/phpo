#!/bin/sh
# deb/rpm postinstall：刷新桌面数据库与图标缓存（尽力而为，缺工具则跳过，不阻断安装）
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database /usr/share/applications >/dev/null 2>&1 || true
command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q /usr/share/icons/hicolor >/dev/null 2>&1 || true
exit 0
