package errs

import "testing"

// TestCodeCountMatchesTable 锁死「声明的条目数 ≡ 表里实际条数」。
// 只看 CodeCount 会骗人：加一条文案而忘了改它，界面遇到那个错误码就取不到文本，
// 而总纲 §0.3 引用的又正是这个数字，所以这里一次对账三件事。
func TestCodeCountMatchesTable(t *testing.T) {
	got := AllCodes()
	if len(got) != CodeCount {
		t.Errorf("AllCodes() 实际 %d 条，CodeCount 声明 %d 条", len(got), CodeCount)
	}
	if CodeCount != 28 {
		t.Errorf("CodeCount = %d，AGENTS.md §0.3 冻结口径为 28（原型 28 条 − lastPhp + 生产新增 FileMissing）", CodeCount)
	}
	seen := map[string]bool{}
	for i, c := range got {
		if c == "" {
			t.Fatalf("第 %d 条文案为空", i+1)
		}
		if seen[c] {
			t.Fatalf("第 %d 条文案与前文重复: %q", i+1, c)
		}
		seen[c] = true
	}
}
