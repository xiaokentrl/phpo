// cmpVer 语义测试：与原型逐分支一致（降序比较器、缺失段=0、非数字段=0）
package version

import "testing"

func TestCmpVer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"8.4", "8.3", -1}, // a 更新 → 负（降序比较器）
		{"8.3", "8.4", 1},
		{"8.4", "8.4", 0},
		{"8.4.1", "8.4", -1}, // 缺失段按 0：8.4.1 > 8.4
		{"8", "8.0", 0},
		{"10.0", "9.9", -1}, // 数值比较而非字典序
		{"abc", "1.0", 1},   // 非数字段按 0：abc≈0.0 < 1.0
		{"1.2.3.4.5", "1.2.3", -1},
		{"999999999999999999999999", "999999999999999999999998", -1}, // 大数段
	}
	for _, c := range cases {
		if got := CmpVer(c.a, c.b); got != c.want {
			t.Errorf("CmpVer(%q,%q) = %d, 期望 %d", c.a, c.b, got, c.want)
		}
	}
}
