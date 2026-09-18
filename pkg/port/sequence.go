// 端口顺延链：从 desired+1 向上到 maxPort，再回绕 minPort..desired-1，取第一个空闲端口
// 站点端口以全范围 (1,65535) 调用；全占用时返回 nil（现实几乎不可能）
package port

// Used 端口占用表：port → 占用者描述（如 "mysql 8.4" / "site demo.test"）
type Used map[int]string

// FindNextAvailable 先 desired+1 → maxPort 升序，再 minPort → desired-1；无空位返回 nil
func FindNextAvailable(desired, maxPort, minPort int, used Used) *int {
	for p := desired + 1; p <= maxPort; p++ {
		if _, ok := used[p]; !ok {
			return &p
		}
	}
	for p := minPort; p < desired; p++ {
		if _, ok := used[p]; !ok {
			return &p
		}
	}
	return nil
}
