package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	wantDisplayName = "phpo"
	wantSlug        = "phpo"
	wantAppID       = "io.github.xiaokentrl.phpo"
	wantDesktopFile = "io.github.xiaokentrl.phpo.desktop"
)

func TestGoldenValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"DisplayName", DisplayName, wantDisplayName},
		{"Slug", Slug, wantSlug},
		{"AppID", AppID, wantAppID},
		{"DesktopID", DesktopID, wantAppID},
		{"DesktopFile", DesktopFile, wantDesktopFile},
		{"IconName", IconName, wantSlug},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s 实得 %q，金标准是 %q", c.name, c.got, c.want)
		}
	}
}

func TestDesktopIdentityIsDerivedFromAppID(t *testing.T) {
	if DisplayName != Slug {
		t.Fatal("产品显示名必须由唯一 Slug 派生")
	}
	if DesktopID != AppID || DesktopFile != AppID+".desktop" {
		t.Fatal("桌面项 ID、文件名和 GTK 应用 ID 必须来自同一个 AppID")
	}
	if IconName != Slug {
		t.Fatal("图标名必须来自产品 Slug")
	}
}

func TestDockIdentityWiring(t *testing.T) {
	root := repoRoot(t)
	mainSource := readFile(t, filepath.Join(root, "main.go"))
	for _, want := range []string{
		"Name:        identity.DisplayName,",
		"ApplicationID: identity.AppID,",
		"ProgramName: identity.DesktopID,",
		"UniqueID:       identity.AppID,",
	} {
		if !strings.Contains(mainSource, want) {
			t.Errorf("main.go 未使用统一身份值 %q", want)
		}
	}

	desktopPath := filepath.Join(root, "build", "linux", DesktopFile)
	if filepath.Base(desktopPath) != DesktopFile {
		t.Fatalf("桌面项文件名 %q 与 DesktopFile=%q 不一致", filepath.Base(desktopPath), DesktopFile)
	}
	desktop := readFile(t, desktopPath)
	for _, want := range []string{
		"Name=" + DisplayName,
		"Exec=" + Slug + " %u",
		"Icon=" + IconName,
		"StartupNotify=false",
		"StartupWMClass=" + DesktopID,
	} {
		if !strings.Contains(desktop, want+"\n") {
			t.Errorf("%s 缺少身份字段 %q", DesktopFile, want)
		}
	}

	nfpm := readFile(t, filepath.Join(root, "build", "linux", "nfpm.yaml"))
	for _, want := range []string{
		"src: build/linux/" + DesktopFile,
		"dst: /usr/share/applications/" + DesktopFile,
	} {
		if !strings.Contains(nfpm, want) {
			t.Errorf("nfpm.yaml 未使用桌面项身份 %q", want)
		}
	}

	appImage := readFile(t, filepath.Join(root, "scripts", "make-appimage.sh"))
	if !strings.Contains(appImage, `DESKTOP_SRC="build/linux/`+DesktopFile+`"`) ||
		!strings.Contains(appImage, `DESKTOP_FILE="$(basename "$DESKTOP_SRC")"`) ||
		!strings.Contains(appImage, `"$APPDIR/$DESKTOP_FILE"`) {
		t.Error("AppImage 必须从桌面项源文件派生其 Desktop ID，不能另造一份")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到 %s：%v", path, err)
	}
	return string(data)
}
