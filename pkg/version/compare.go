// 版本号比较：cmpVer 直译（缺失段按 0、非数字段按 0；返回值语义与 JS 排序比较器一致）
package version

import (
	"math/big"
	"strings"
)

// CmpVer 原型 cmpVer(a,b)：a 新于 b 返回负、a 旧于 b 返回正、相等返回 0（降序比较器）
func CmpVer(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		x := seg(pa, i)
		y := seg(pb, i)
		if x.Cmp(y) != 0 {
			return y.Cmp(x)
		}
	}
	return 0
}

// NaN||0 语义：非数字段计 0；big.Int 支撑超长数字段
func seg(parts []string, i int) *big.Int {
	v := new(big.Int)
	if i >= len(parts) {
		return v
	}
	if _, ok := v.SetString(parts[i], 10); !ok {
		return new(big.Int)
	}
	return v
}
