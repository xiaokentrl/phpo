// 离线缓存路径推导（§5.14.2）：缓存根目录持久 · 临时目录单任务
package config

import "path/filepath"

// OfflineImageTar PHPO_HOME 根下 ./offline/{kind}/{version}/image.tar（根由装机向导选定，见 §0.1.1）
func (e Env) OfflineImageTar(kind, version string) string {
	return filepath.ToSlash(e.OfflineRoot + "/" + kind + "/" + version + "/image.tar")
}

// OfflineExtImageTar 扩展固化镜像（phpo/php:{version}）的缓存 tar：
// ./offline/{kind}/{version}/image-extensions.tar——与基座镜像各占一份文件。
// 两者共用 image.tar 会让「应用扩展」把基座缓存整份覆盖掉，下次装 php 基座即 load 到扩展镜像。
func (e Env) OfflineExtImageTar(kind, version string) string {
	return filepath.ToSlash(e.OfflineRoot + "/" + kind + "/" + version + "/image-extensions.tar")
}

// OfflineManifestFile 每版本一份缓存清单
func (e Env) OfflineManifestFile(kind, version string) string {
	return filepath.ToSlash(e.OfflineRoot + "/" + kind + "/" + version + "/manifest.json")
}

// OfflineExtDir 扩展包缓存目录（apk / pecl 二选一）
func (e Env) OfflineExtDir(kind, version, extType string) string {
	return filepath.ToSlash(e.OfflineRoot + "/" + kind + "/" + version + "/" + extType)
}

// TempExtDir 临时目录（PHPO_HOME 根下）./{kind}/{version}/ext/（任务结束必清空）
func (e Env) TempExtDir(kind, version string) string {
	return filepath.ToSlash(e.RootFor(kind, version) + "/ext")
}

// TempExtTypeDir 临时目录内按类型分区：ext/apk 或 ext/pecl
func (e Env) TempExtTypeDir(kind, version, extType string) string {
	return filepath.ToSlash(e.TempExtDir(kind, version) + "/" + extType)
}

// 扩展类型枚举（php 服务专用）
const (
	ExtTypeAPK  = "apk"
	ExtTypePECL = "pecl"
)
