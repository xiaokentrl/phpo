// doctor 环境诊断视图模型（§5.7）：15 项检查逐条结果 + 汇总计数，OverviewView 消费
package model

// DoctorStatus 单项诊断结论（对应 §5.7 通过/警告/失败三态）
type DoctorStatus string

const (
	DoctorOK   DoctorStatus = "ok"   // 通过
	DoctorWarn DoctorStatus = "warn" // 警告（可继续，最小限制原则）
	DoctorErr  DoctorStatus = "err"  // 失败（可能阻断，附建议）
)

// DoctorCheck 单项诊断：ID 稳定供前端定位，Fix 非空表示支持一键修复
type DoctorCheck struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Status DoctorStatus `json:"status"`
	Detail string       `json:"detail"`
	Hint   string       `json:"hint"`
	Fix    string       `json:"fix"` // "" 无 / "calibrate" 状态校准 / "clear_temp" 清空临时目录残留
}

// DoctorReport 一次诊断全量结果 + 三态计数
type DoctorReport struct {
	Checks   []DoctorCheck `json:"checks"`
	OK       int           `json:"ok"`
	Warnings int           `json:"warnings"`
	Errors   int           `json:"errors"`
}
