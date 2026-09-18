// SemVer 工具：总纲选型 Masterminds/semver v3 的薄封装（本项目版本号不做字符集限制，此处仅辅助展示/排序）
package version

import semver "github.com/Masterminds/semver/v3"

// IsSemVer 判定是否可解析为 SemVer（不合法不等于不合法版本号，仅影响展示分组）
func IsSemVer(s string) bool {
	_, err := semver.NewVersion(s)
	return err == nil
}

// Less 仅在双方均为合法 SemVer 时比较，否则返回 false（调用方回落 CmpVer）
func Less(a, b string) bool {
	va, err := semver.NewVersion(a)
	if err != nil {
		return false
	}
	vb, err := semver.NewVersion(b)
	if err != nil {
		return false
	}
	return va.LessThan(vb)
}
