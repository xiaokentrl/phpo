// root-set 规则单测（需求 1/2/7/8）：自定义缓存根 / 备份根 / 每服务版本数据目录
// 口径：只拦路径穿越（硬红线 3），其余一律放行（§0.2 #15/#16）；置空即回落默认，不属错误
package preflight

import (
	"testing"

	"phpo/pkg/errs"
)

func TestRootSet_AcceptsAnySafePath(t *testing.T) {
	w := readyWorld()
	for _, v := range []string{"/mnt/cache", "~/cache", `D:\phpo\offline`, "/带 空格/中文", ""} {
		for _, field := range []string{"offline_root", "backup_root", "data_dir"} {
			res := Run(ActRootSet, Ctx{Field: field, Kind: "mysql", Version: "8.4", NewValue: v}, w)
			if !res.Ok {
				t.Errorf("%s=%q 应放行，得 %+v", field, v, res.Errors)
			}
		}
	}
}

func TestRootSet_RejectsTraversal(t *testing.T) {
	w := readyWorld()
	for _, v := range []string{"/a/../b", "../b", "/a/.."} {
		res := Run(ActRootSet, Ctx{Field: "offline_root", NewValue: v}, w)
		if res.Ok || firstErr(res) != errs.PathTraversal {
			t.Errorf("%q 应被路径安全拒绝，得 %+v", v, res.Errors)
		}
	}
}

// 数据目录是挂载宿主路径，改完要重建容器才进得去运行中的容器：警告不阻止（§0.2 #16）
func TestRootSet_RunningServiceWarnsNotBlocks(t *testing.T) {
	w := readyWorld() // mysql 8.4 已装未运行
	if res := Run(ActRootSet, Ctx{Field: "data_dir", Kind: "mysql", Version: "8.4", NewValue: "/disk2/mysql84"}, w); !res.Ok || len(res.Warnings) != 0 {
		t.Fatalf("未运行的服务改数据目录应无警告通过，得 %+v / %+v", res.Errors, res.Warnings)
	}
	w.Snap.Running["mysql"] = []string{"8.4"}
	res := Run(ActRootSet, Ctx{Field: "data_dir", Kind: "mysql", Version: "8.4", NewValue: "/disk2/mysql84"}, w)
	if !res.Ok {
		t.Fatalf("在跑服务改数据目录只警告不阻止，得 %+v", res.Errors)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("在跑服务应给出「重建生效」警告，得 %+v", res.Warnings)
	}
}

// root-set 落 config.yaml：两根未就绪即拒绝，否则会在用户数据目录凭空建出 config.yaml（违反首启零落盘）
func TestRootSet_NeedsHome(t *testing.T) {
	w := readyWorld()
	w.Snap.DirReady["PHPO_HOME"] = false
	res := Run(ActRootSet, Ctx{Field: "offline_root", NewValue: "/mnt/cache"}, w)
	if res.Ok || firstErr(res) != errs.HomeNotReady {
		t.Fatalf("HOME 未就绪应报 homeNotReady，得 %+v", res.Errors)
	}
}
