// #214 · 全量清单的纯派生判定：预置 DockerInventory，断言「未使用」四类的口径与「取不到就说取不到」。
//
// 这条测试存在的理由只有一个：界面上一句「没有未使用镜像」可以是两种事实——
// 真扫过了、一个都没有；或者压根没扫到。第二种若被画成 0，用户就会以为可以放心删，
// 而后端下一步真去删的时候用的正是这份判定。故每条派生都要有一半用例专门锁「失败即空、不编数字」。
package engine

import (
	"testing"
)

// broken 造一份「某一类取不到」的清单账本，键与 ScanDockerInventory 里的类别名一致。
func broken(cats ...string) map[string]string {
	m := map[string]string{}
	for _, c := range cats {
		m[c] = "docker daemon 没答上话"
	}
	return m
}

func imageIDs(imgs []InvImage) []string {
	out := make([]string, len(imgs))
	for i, im := range imgs {
		out[i] = im.ID
	}
	return out
}

func hasID(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// groupOf 按名字找一组。界面上每组是一行，拿索引取组等于赌排序，故一律按名字找。
func groupOf(gs []InvGroup, name string) *InvGroup {
	for i := range gs {
		if gs[i].Name == name {
			return &gs[i]
		}
	}
	return nil
}

func TestDanglingImages_OnlyUnreferenced(t *testing.T) {
	inv := DockerInventory{
		Images: []InvImage{
			{ID: "sha:tagged", Refs: []string{"nginx:1.25"}},
			{ID: "sha:free", Refs: nil},
			{ID: "sha:empty", Refs: []string{}},
		},
	}
	got := imageIDs(inv.DanglingImages())
	if len(got) != 2 || !hasID(got, "sha:free") || !hasID(got, "sha:empty") {
		t.Fatalf("悬空镜像应为两份无标签的，实得 %v", got)
	}
}

// 取不到镜像清单时不得返回「一份也没有」——那会被界面读成 0 份，并被删除链路当成安全依据。
func TestDanglingImages_FailedYieldsNothing(t *testing.T) {
	inv := DockerInventory{
		Images:   []InvImage{{ID: "sha:free"}},
		Failures: broken(CatImages),
	}
	if got := inv.DanglingImages(); got != nil {
		t.Fatalf("镜像类别失败时不得给出任何判定，实得 %v", imageIDs(got))
	}
}

func TestUnusedImages_ParentChainKept(t *testing.T) {
	inv := DockerInventory{
		Containers: []InvContainer{
			{Name: "phpo-php-8.4", Image: "php:8.4-fpm", ImageID: "sha:base"},
			{Name: "keeper", Image: "by-digest-only", ImageID: "sha:child"},
		},
		Images: []InvImage{
			{ID: "sha:base", Refs: []string{"php:8.4-fpm"}},
			// 中间层无标签，但因为下面是容器在用的 sha:child，父链必须保住它。
			{ID: "sha:mid", ParentID: "sha:base"},
			{ID: "sha:child", ParentID: "sha:mid", Digests: []string{"by-digest-only"}},
			{ID: "sha:idle", Refs: []string{"redis:7"}},
			{ID: "sha:dangling", Refs: nil},
		},
	}
	got := imageIDs(inv.UnusedImages())
	if len(got) != 1 || got[0] != "sha:idle" {
		t.Fatalf("未使用镜像只应剩 sha:idle（有标签、无人用、非父链），实得 %v", got)
	}
}

// 容器清单失败即「不知道谁在用」，此时断言任何镜像未被使用都是拿猜测去删东西。
func TestUnusedImages_ContainerFailureBlocks(t *testing.T) {
	inv := DockerInventory{
		Images:     []InvImage{{ID: "sha:idle", Refs: []string{"redis:7"}}},
		Containers: []InvContainer{{Name: "ghost", ImageID: "sha:idle"}},
		Failures:   broken(CatContainers),
	}
	if got := inv.UnusedImages(); got != nil {
		t.Fatalf("容器类别失败时不得判任何镜像未使用，实得 %v", imageIDs(got))
	}
}

func TestUnusedImages_BothCategoriesNeeded(t *testing.T) {
	inv := DockerInventory{
		Containers: []InvContainer{{Name: "a", ImageID: "sha:x"}},
		Failures:   broken(CatImages),
	}
	if got := inv.UnusedImages(); got != nil {
		t.Fatalf("镜像类别失败时不得有判定，实得 %v", imageIDs(got))
	}
}

func TestUnusedVolumes_NeedsRefCountEvidence(t *testing.T) {
	inv := DockerInventory{
		Containers: []InvContainer{{Name: "phpo-mysql-8.0", NamedVolumes: []string{"phpo-mysql-8.0-data"}}},
		Volumes: []InvVolume{
			{Name: "phpo-mysql-8.0-data", HasRef: true, RefCount: 1}, // 容器在用 → 不判未使用
			{Name: "by-refcount", HasRef: true, RefCount: 0},         // 卷自己说没人挂 → 未使用
			{Name: "no-usagedata"},                                   // VolumeList 不保证带 UsageData → 不判
			{Name: "listed-by-container", HasRef: true, RefCount: 3},
		},
	}
	var got []string
	for _, v := range inv.UnusedVolumes() {
		got = append(got, v.Name)
	}
	if len(got) != 1 || got[0] != "by-refcount" {
		t.Fatalf("未使用卷只应有取到引用计数且为 0 的那份，实得 %v", got)
	}
}

// 容器失败时只剩卷自身的引用计数这一条证据：有计数为 0 的仍可判，无计数的仍不可判。
func TestUnusedVolumes_WithoutContainersStillGated(t *testing.T) {
	inv := DockerInventory{
		Volumes: []InvVolume{
			{Name: "zero", HasRef: true, RefCount: 0},
			{Name: "unknown"},
		},
		Failures: broken(CatContainers),
	}
	var got []string
	for _, v := range inv.UnusedVolumes() {
		got = append(got, v.Name)
	}
	if len(got) != 1 || got[0] != "zero" {
		t.Fatalf("容器失败时仍须只认有引用计数的卷，实得 %v", got)
	}
}

func TestUnusedVolumes_FailedYieldsNothing(t *testing.T) {
	inv := DockerInventory{
		Volumes:  []InvVolume{{Name: "zero", HasRef: true, RefCount: 0}},
		Failures: broken(CatVolumes),
	}
	if got := inv.UnusedVolumes(); got != nil {
		t.Fatalf("卷类别失败时不得有判定，实得 %d 份", len(got))
	}
}

func TestUnusedNetworks_SkipsBuiltinsAndAttached(t *testing.T) {
	inv := DockerInventory{
		Networks: []InvNetwork{
			{Name: "bridge"}, {Name: "host"}, {Name: "none"},
			{Name: "ingress", Ingress: true},
			{Name: "phpo-network", Attached: 4},
			{Name: "orphan-net"},
		},
	}
	var got []string
	for _, n := range inv.UnusedNetworks() {
		got = append(got, n.Name)
	}
	if len(got) != 1 || got[0] != "orphan-net" {
		t.Fatalf("只有既非内置、又无连接、也非 ingress 的网络才算未使用，实得 %v", got)
	}
}

func TestUnusedNetworks_FailedYieldsNothing(t *testing.T) {
	inv := DockerInventory{
		Networks: []InvNetwork{{Name: "orphan-net"}},
		Failures: broken(CatNetworks),
	}
	if got := inv.UnusedNetworks(); got != nil {
		t.Fatalf("网络类别失败时不得有判定，实得 %d 份", len(got))
	}
}

func TestGroups_ByComposeLabel(t *testing.T) {
	inv := DockerInventory{
		Containers: []InvContainer{
			{Name: "app-web-1", Labels: map[string]string{ComposeProjectLabel(): "shop"}, SizeRw: 10},
			{Name: "app-db-1", Labels: map[string]string{ComposeProjectLabel(): "shop"}, SizeRw: 5},
			{Name: "other", Labels: map[string]string{ComposeProjectLabel(): "blog"}, SizeRw: 1},
			{Name: "unlabeled"},
		},
		Images: []InvImage{
			{ID: "sha:i1", Refs: []string{"shop/app:latest"}, Labels: map[string]string{ComposeProjectLabel(): "shop"}, Size: 100},
			{ID: "sha:i2", Labels: map[string]string{ComposeProjectLabel(): "shop"}, Size: 7}, // 无标签则退回 ID
		},
		Volumes: []InvVolume{
			{Name: "shop_data", Labels: map[string]string{ComposeProjectLabel(): "shop"}, HasSize: true, Size: 50},
			{Name: "shop_nosize", Labels: map[string]string{ComposeProjectLabel(): "shop"}}, // 取不到体积就不累加
		},
		Networks: []InvNetwork{{Name: "shop_default", Labels: map[string]string{ComposeProjectLabel(): "shop"}}},
		Services: []InvNamed{{ID: "svc1", Name: "shop_web", Labels: map[string]string{SwarmStackLabel(): "shop"}}},
	}

	gs := inv.Groups(ComposeProjectLabel())
	if len(gs) != 2 {
		t.Fatalf("按 compose 标签应归出 shop / blog 两组（stack 标签不串台、无标签的不进组），实得 %d 组", len(gs))
	}
	if gs[0].Name != "blog" || len(gs[0].Containers) != 1 {
		t.Fatalf("组应按名字排序且各归各的成员：%+v", gs[0])
	}
	g := groupOf(gs, "shop")
	if g == nil {
		t.Fatalf("应归出 shop 一组：%+v", gs)
	}
	if len(g.Containers) != 2 || len(g.Images) != 2 || len(g.Volumes) != 2 || len(g.Networks) != 1 {
		t.Fatalf("分组明细不对：%+v", g)
	}
	if g.Containers[0] != "app-db-1" || g.Containers[1] != "app-web-1" {
		t.Fatalf("组内成员应稳定按名字排序，实得 %v", g.Containers)
	}
	if !hasID(g.Images, "sha:i2") || !hasID(g.Images, "shop/app:latest") {
		t.Fatalf("镜像组内引用应取标签、无标签退回 ID，实得 %v", g.Images)
	}
	if want := int64(10 + 5 + 100 + 7 + 50); g.Bytes != want {
		t.Fatalf("组体积应累加可取到的那些，期望 %d 实得 %d", want, g.Bytes)
	}
	// swarm stack 是另一把标签，不得与 compose 混为一组。
	sg := inv.Groups(SwarmStackLabel())
	if len(sg) != 1 || sg[0].Name != "shop" || len(sg[0].Services) != 1 || len(sg[0].Containers) != 0 {
		t.Fatalf("按 stack 标签只应归出服务那一维：%+v", sg)
	}
}

// 识别不了的类别跳过，其余照常归组；全失败则给出空列表，界面上写「未发现」而不是 0 字节。
func TestGroups_SkipsFailedCategories(t *testing.T) {
	inv := DockerInventory{
		Containers: []InvContainer{{Name: "app", Labels: map[string]string{ComposeProjectLabel(): "shop"}, SizeRw: 9}},
		Images:     []InvImage{{ID: "sha:i", Labels: map[string]string{ComposeProjectLabel(): "shop"}, Size: 1 << 30}},
		Failures:   broken(CatImages),
	}
	gs := inv.Groups(ComposeProjectLabel())
	if len(gs) != 1 || gs[0].Bytes != 9 || len(gs[0].Images) != 0 {
		t.Fatalf("失败的镜像类不得进组、也不得贡献体积：%+v", gs)
	}

	empty := DockerInventory{Failures: broken(CatContainers, CatImages, CatVolumes, CatNetworks, CatSwarmServices)}
	if got := empty.Groups(ComposeProjectLabel()); len(got) != 0 {
		t.Fatalf("五类全取不到时应回空列表（不是编造出来的组）：%+v", got)
	}
}

func TestNamedHelpers(t *testing.T) {
	if got := firstContainerName([]string{"/phpo-php-8.4"}); got != "phpo-php-8.4" {
		t.Fatalf("容器显示名应去掉开头的斜杠，实得 %q", got)
	}
	if got := firstContainerName(nil); got != "" {
		t.Fatalf("无名容器应回空串而不是 panic，实得 %q", got)
	}
	if got := imageSortKey(InvImage{ID: "sha:x"}); got != "~sha:x" {
		t.Fatalf("无标签镜像的排序键要有独立前缀，实得 %q", got)
	}
	if got := imageSortKey(InvImage{ID: "sha:x", Refs: []string{"a:1", "b:2"}}); got != "a:1" {
		t.Fatalf("有标签时按第一个标签排序，实得 %q", got)
	}
}
