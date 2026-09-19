// T605b · 孤儿扫描纯判定：预置「容器/卷/网络/镜像」矩阵，断言四类命中与隔离边界。
package engine

import (
	"testing"

	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

func names(rs []model.DockerResource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestDetectOrphans_Matrix(t *testing.T) {
	installed := map[string]bool{
		dockerutil.ContainerName("php", "8.4"):   true,
		dockerutil.ContainerName("mysql", "8.0"): true,
	}
	in := OrphanInput{
		Installed: installed,
		Containers: []ContainerRecord{
			{Name: "phpo-php-8.4", Image: "phpo/php:8.4", Running: true, Volumes: []string{"phpo-php-8.4-conf"}},
			{Name: "phpo-mysql-8.0", Image: "mysql:8.0", Running: true, Volumes: []string{"phpo-mysql-8.0-data"}},
			{Name: "phpo-redis-7", Image: "redis:7", Running: false, Volumes: []string{"phpo-redis-7-data"}}, // 孤儿容器
		},
		Volumes: []VolumeRecord{
			{Name: "phpo-php-8.4-conf", Size: 100},   // 被在用容器引用 → 非孤儿
			{Name: "phpo-mysql-8.0-data", Size: 200}, // 被在用容器引用 → 非孤儿
			{Name: "phpo-redis-7-data", Size: 300},   // 仅孤儿容器引用 → 孤儿
			{Name: "user-keepme", Size: 999},         // 外部卷 → 隔离，绝不报
		},
		Networks: []NetworkRecord{
			{Name: "phpo-network", Attached: 2},  // 共享常驻 → 非孤儿
			{Name: "phpo-leftover", Attached: 0}, // phpo-* 无连接 → 孤儿
			{Name: "bridge", Attached: 0},        // 外部网络 → 隔离，不报
		},
		Images: []ImageRecord{
			{ID: "sha:aaa", Refs: []string{"phpo/php:8.4"}, Size: 10}, // 被在用容器引用 → 非孤儿
			{ID: "sha:bbb", Refs: []string{"phpo/php:7.4"}, Size: 20}, // phpo 前缀无人引用 → 孤儿
			{ID: "sha:ccc", Refs: []string{"nginx:alpine"}, Size: 30}, // 官方镜像非 phpo 命名空间 → 不报
			{ID: "sha:ddd", Refs: []string{"<none>"}, Size: 40},       // 悬空非 phpo → 不报（隔离）
		},
	}

	rep := DetectOrphans(in)

	if got := names(rep.Containers); !has(got, "phpo-redis-7") || len(got) != 1 {
		t.Fatalf("孤儿容器应仅 phpo-redis-7，实得 %v", got)
	}
	if got := names(rep.Volumes); !has(got, "phpo-redis-7-data") || len(got) != 1 {
		t.Fatalf("孤儿卷应仅 phpo-redis-7-data，实得 %v", got)
	}
	if got := names(rep.Networks); !has(got, "phpo-leftover") || len(got) != 1 {
		t.Fatalf("孤儿网络应仅 phpo-leftover，实得 %v", got)
	}
	if got := names(rep.Images); !has(got, "phpo/php:7.4") || len(got) != 1 {
		t.Fatalf("孤儿镜像应仅 phpo/php:7.4，实得 %v", got)
	}
	if rep.Total() != 4 {
		t.Fatalf("四类孤儿总数应为 4，实得 %d", rep.Total())
	}
}

func TestDetectOrphans_ExternalIsolation(t *testing.T) {
	// 全是外部资源、无 phpo 命名空间 → 四类皆空
	in := OrphanInput{
		Installed:  map[string]bool{},
		Containers: []ContainerRecord{{Name: "some-else", Image: "redis:7"}},
		Volumes:    []VolumeRecord{{Name: "not-mine"}},
		Networks:   []NetworkRecord{{Name: "host-only", Attached: 0}},
		Images:     []ImageRecord{{ID: "x", Refs: []string{"mysql:8"}}},
	}
	rep := DetectOrphans(in)
	if rep.Total() != 0 {
		t.Fatalf("外部资源不得计入 phpo 孤儿，实得 %+v", rep)
	}
}

func TestDetectOrphans_NoInstalledKeepsAllPhpoVolumes(t *testing.T) {
	// 无任何已安装容器：所有 phpo 卷均无人引用 → 全报孤儿（默认保留与否交清理模式裁决）
	in := OrphanInput{
		Installed: map[string]bool{},
		Volumes:   []VolumeRecord{{Name: "phpo-mysql-8.0-data"}, {Name: "phpo-redis-7-data"}},
	}
	rep := DetectOrphans(in)
	if len(rep.Volumes) != 2 {
		t.Fatalf("期望 2 个孤儿卷，实得 %v", names(rep.Volumes))
	}
}
