// 60 行清单的形状契约：行数·分组·档位·危险项名单·前端文案覆盖，全部按 ①–㉚ 的裁决锁死
package model

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// 每个分组应有多少行：一旦有人往表里塞第 61 行或漏一行，这条先红
var wantGroupCount = map[string]int{
	"container": 9, "image": 7, "volume": 5, "network": 7, "build": 4, "log": 5,
	"plugin": 3, "swarm": 6, "compose": 2, "system": 8, "other": 4,
}

func TestCleanRowTable_Shape(t *testing.T) {
	rows := AllCleanRowMetas()
	if len(rows) != 60 {
		t.Fatalf("清单必须 60 行，实得 %d", len(rows))
	}
	if len(AllCleanGroups) != 11 {
		t.Fatalf("必须 11 个分组，实得 %d", len(AllCleanGroups))
	}

	seen := map[string]bool{}
	groups := map[string]int{}
	phpo := 0
	linuxOnly := 0
	byTier := map[Tier]int{}
	critical := 0

	for i, m := range rows {
		if m.Key == "" {
			t.Fatalf("第 %d 行无 key", i)
		}
		if seen[m.Key] {
			t.Fatalf("key 重复: %s", m.Key)
		}
		seen[m.Key] = true
		if !contains(AllCleanGroups, m.Group) {
			t.Errorf("%s 的分组 %q 不在 AllCleanGroups 里", m.Key, m.Group)
		}
		groups[m.Group]++
		if m.IsPhpo {
			phpo++
		}
		if m.LinuxOnly {
			linuxOnly++
		}
		switch m.Tier {
		case TierSDK, TierElev, TierInfo:
			byTier[m.Tier]++
		default:
			t.Errorf("%s 档位非法: %d（只有 1/2/3 三档）", m.Key, m.Tier)
		}
		switch m.Risk {
		case RiskLow, RiskMedium, RiskHigh, RiskCritical:
			if m.Risk == RiskCritical {
				critical++
				if m.Key != "system.root" {
					t.Errorf("%s 不得标 critical：整份清单只有「清空 /var/lib/docker」这一档", m.Key)
				}
			}
		default:
			t.Errorf("%s 风险档非法: %q", m.Key, m.Risk)
		}
	}

	if critical != 1 {
		t.Errorf("critical 必须恰好 1 行，实得 %d", critical)
	}
	// ⑩ 的范围卡要把 phpo 自己的 36 行锁起来，这个数字变了卡片就答错题
	if phpo != 36 || len(rows)-phpo != 24 {
		t.Errorf("phpo 自有行必须 36／非 phpo 24，实得 %d／%d", phpo, len(rows)-phpo)
	}
	if linuxOnly != 23 {
		t.Errorf("只在 Linux 上够得着的行必须 23，实得 %d", linuxOnly)
	}
	// 三档分布 **25／19／16**（不是随手点的数）：
	//   - 需授权那一档只留「扫得出具体对象、且动得了宿主文件」的行；
	//     build.buildx／volume.bind／log.build／other.events／other.cache 这五行数不出可勾的对象
	//     （Docker 的清单里没有它们，或它们的占用已经算进别的行），给按钮等于界面在撒谎，故落到只读那一档。
	if byTier[TierSDK] != 25 || byTier[TierElev] != 19 || byTier[TierInfo] != 16 {
		t.Errorf("三档分布必须 25／19／16，实得 %d／%d／%d", byTier[TierSDK], byTier[TierElev], byTier[TierInfo])
	}
	for g, want := range wantGroupCount {
		if groups[g] != want {
			t.Errorf("分组 %s 应 %d 行，实得 %d", g, want, groups[g])
		}
	}
}

// ⑭：危险项就是这 4 行，一份名单、不给第二处判据（⑲ ㉖ ㉚ 都读它）
func TestDangerousCleanRows_ExactlyFour(t *testing.T) {
	if len(DangerousCleanRowKeys) != 4 {
		t.Fatalf("危险项必须恰好 4 行，实得 %d: %v", len(DangerousCleanRowKeys), DangerousCleanRowKeys)
	}
	want := []string{"network.veth", "network.netns", "network.cni", "system.group"}
	for _, k := range want {
		if !IsDangerousCleanRow(k) {
			t.Errorf("%s 必须在危险项名单内", k)
		}
		m, ok := CleanRowMetaOf(k)
		if !ok {
			t.Fatalf("%s 不在 60 行清单里", k)
		}
		// ⑫：这 4 行照给按钮，只是点开关要单独确认，所以必须是可删档
		if !m.Deletable() || m.Tier != TierElev {
			t.Errorf("%s 必须可删且为需授权档（TierElev），实得 tier=%d deletable=%v", k, m.Tier, m.Deletable())
		}
	}
	for _, k := range []string{"log.journald", "container.cgroup", "network.iptables", "system.root"} {
		if IsDangerousCleanRow(k) {
			t.Errorf("%s 不该进危险项名单（⑭ 只收 4 行）", k)
		}
	}
	if IsDangerousCleanRow("container.all") {
		t.Error("普通行判成了危险项")
	}
}

// ③：只读说明那一档永远不给按钮；给按钮的两档必须给
func TestTierInfo_HasNoDeleteButton(t *testing.T) {
	info, elev, sdk := 0, 0, 0
	for _, m := range AllCleanRowMetas() {
		switch m.Tier {
		case TierInfo:
			info++
			if m.Deletable() {
				t.Errorf("%s 是只读档却可删", m.Key)
			}
		case TierElev:
			elev++
			if !m.Deletable() {
				t.Errorf("%s 需授权但没给按钮（⑫）", m.Key)
			}
		case TierSDK:
			sdk++
			if !m.Deletable() {
				t.Errorf("%s 能直接删却没给按钮", m.Key)
			}
		}
	}
	if info != 16 || elev != 19 || sdk != 25 {
		t.Errorf("档位计数 16/19/25 对不上，实得 %d/%d/%d", info, elev, sdk)
	}
}

func TestCleanRowMetaOf_UnknownKey(t *testing.T) {
	if _, ok := CleanRowMetaOf("network.veth"); !ok {
		t.Fatal("已登记的 key 查不到")
	}
	m, ok := CleanRowMetaOf("container.al") // 手抖写错
	if ok || m.Key != "" {
		t.Errorf("名单外的 key 必须返回 false 且给空元数据，实得 ok=%v %+v", ok, m)
	}
}

// NewCleanRow 是档位与危险标记的唯一装配点：前端不再推第二套判据
func TestNewCleanRow_CarriesAffordances(t *testing.T) {
	row := NewCleanRow("network.veth", RowOK, 3, 0, false)
	if row.Group != "network" || row.Tier != TierElev || !row.Deletable || !row.Dangerous || !row.IsPhpo {
		t.Errorf("veth 行属性错: %+v", row)
	}
	if row.Risk != RiskHigh {
		t.Errorf("veth 风险徽标应为 high: %+v", row)
	}
	safe := NewCleanRow("build.cache", RowOK, 12, 1024, true)
	if safe.Dangerous || !safe.Deletable || safe.IsPhpo {
		t.Errorf("构建缓存行不该带危险/自有标记: %+v", safe)
	}
	info := NewCleanRow("system.root", RowOK, 0, 0, false)
	if info.Deletable {
		t.Errorf("只读档不得给按钮: %+v", info)
	}
	// 取不到数字时不报 0：HasBytes=false 让界面写「—」
	if got := NewCleanRow("system.config", RowNoPerm, 0, 0, false); got.Status != RowNoPerm || got.HasBytes {
		t.Errorf("无权读取行的形状错: %+v", got)
	}
	// 名单外的 key 退化为裸行，不炸、也不编造档位
	bare := NewCleanRow("nope.nope", RowUnavailable, 0, 0, false)
	if bare.Key != "nope.nope" || bare.Deletable || bare.Tier != 0 || bare.Status != RowUnavailable {
		t.Errorf("未知 key 应退化为裸行: %+v", bare)
	}
}

// 快照里每一行的线上形状：camelCase 键 + 缺席字段不外泄
func TestCleanRowJSONKeys(t *testing.T) {
	b, err := json.Marshal(NewCleanRow("image.dangling", RowOK, 2, 100, true))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"key"`, `"group"`, `"tier"`, `"deletable"`, `"dangerous"`, `"isPhpo"`,
		`"risk"`, `"status"`, `"count"`, `"bytes"`, `"hasBytes"`, `"scannedAt"`} {
		if !strings.Contains(string(b), k+":") {
			t.Errorf("线上键 %s 缺席: %s", k, b)
		}
	}
	if strings.Contains(string(b), `"message"`) {
		t.Errorf("无说明文字时不得发 message 键: %s", b)
	}
}

func TestCleanScanReportJSONKeys(t *testing.T) {
	r := CleanScanReport{Rows: []CleanRow{NewCleanRow("other.events", RowOK, 1, 0, false)},
		TotalCount: 1, ScannedAt: time.Now(), Warnings: []string{"w"}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"rows":`, `"totalCount":`, `"totalBytes":`, `"scannedAt":`, `"deepScanned":`, `"warnings":`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("报告键 %s 缺席: %s", k, b)
		}
	}
}

// 预览载荷：一次性凭据 + 具体对象清单（㉗ 项级勾选靠的就是 Targets）
func TestCleanPreviewJSONKeys(t *testing.T) {
	p := CleanPreview{Token: "tok", ExpiresAt: time.Now(), Rows: []string{"volume.unused"},
		Targets: []CleanTarget{{Row: "volume.unused", Kind: "volume", ID: "abc", Name: "phpo-mysql-8.0-data",
			Size: 2048, InUse: false, Foreign: true, NeedsRoot: true}},
		Warnings: []string{"w"}, ConsentRequired: true,
		Installed: []CleanInstalled{{Kind: "mysql", Version: "8.0"}}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range []string{`"token":`, `"expiresAt":`, `"rows":`, `"targets":`, `"warnings":`,
		`"consentRequired":`, `"installed":`, `"needsRoot":`, `"foreign":`, `"inUse":`} {
		if !strings.Contains(s, k) {
			t.Errorf("预览键 %s 缺席: %s", k, s)
		}
	}
}

// 执行结果：Skipped 记「预览时还在、动手时已不在」（⑧），不是整单失败
func TestCleanExecuteReportJSONKeys(t *testing.T) {
	r := CleanExecuteReport{TaskID: "t1", Status: TaskSuccess,
		Items:   []CleanedItem{{Type: ResVolume, Name: "v1", OK: true}},
		Removed: 1, Failed: 0, Skipped: 2, FreedBytes: 4096,
		Trashed: []string{"v1"}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range []string{`"taskId":`, `"status":`, `"items":`, `"removed":`, `"failed":`,
		`"skipped":`, `"freedBytes":`, `"trashed":`} {
		if !strings.Contains(s, k) {
			t.Errorf("结果键 %s 缺席: %s", k, s)
		}
	}
}

// ⑱：每一行的名字与三步人话说明都在前端 locales，后端只发 key
func TestCleanRows_HaveLocaleCopy(t *testing.T) {
	for _, file := range []string{
		"../../frontend/src/locales/zh-CN.ts",
		"../../frontend/src/locales/en-US.ts",
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", file, err)
		}
		text := string(src)
		for _, m := range AllCleanRowMetas() {
			for _, suffix := range []string{".label", ".desc"} {
				key := `"clean.` + m.Key + suffix + `":`
				if !strings.Contains(text, key) {
					t.Errorf("%s 缺文案键 %s（60 行 × 名称 + 说明，中英两侧都要有）", file, key)
				}
			}
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
