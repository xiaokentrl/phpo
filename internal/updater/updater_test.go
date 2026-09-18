// updater 骨架单测：httptest 发布清单 / 版本比较 / update:available / SHA256+Ed25519 双校验 / 调度
package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"phpo/internal/model"
)

type capEmitter struct {
	mu     sync.Mutex
	events []capEvent
}
type capEvent struct {
	Name    string
	Payload any
}

func (c *capEmitter) Emit(n string, p any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capEvent{n, p})
}
func (c *capEmitter) Capture() []capEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capEvent, len(c.events))
	copy(out, c.events)
	return out
}

func hasEvent(cap *capEmitter, name string) bool {
	for _, e := range cap.Capture() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// —— 版本比较 ——

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.2.0", "1.1.0", true},
		{"1.0.0", "1.0.0", false},
		{"0.9.9", "1.0.0", false},
		{"2.0", "1.99", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%s,%s)=%v 期望 %v", c.latest, c.current, got, c.want)
		}
	}
}

// —— 检查器：发现新版本发事件 ——

func serveRelease(t *testing.T, r Release) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r)
	}))
}

func TestCheckerAvailable(t *testing.T) {
	srv := serveRelease(t, Release{Version: "2.0.0", Changelog: "新", Size: 100, SHA256: "abc"})
	defer srv.Close()
	cap := &capEmitter{}
	c := NewChecker("1.0.0", HTTPSource{URL: srv.URL}, cap)
	rel, has, err := c.Check(context.Background())
	if err != nil || !has || rel.Version != "2.0.0" {
		t.Fatalf("应发现新版本，得 has=%v err=%v", has, err)
	}
	if !hasEvent(cap, "update:available") {
		t.Fatal("应发 update:available")
	}
	var payload model.UpdateAvailable
	for _, e := range cap.Capture() {
		if e.Name == "update:available" {
			payload = e.Payload.(model.UpdateAvailable)
		}
	}
	if payload.Version != "2.0.0" || payload.Size != 100 {
		t.Fatalf("事件载荷错误: %+v", payload)
	}
}

func TestCheckerUpToDate(t *testing.T) {
	srv := serveRelease(t, Release{Version: "1.0.0"})
	defer srv.Close()
	cap := &capEmitter{}
	c := NewChecker("1.0.0", HTTPSource{URL: srv.URL}, cap)
	if _, has, err := c.Check(context.Background()); err != nil || has {
		t.Fatalf("已最新不应有更新，得 has=%v err=%v", has, err)
	}
	if hasEvent(cap, "update:available") {
		t.Fatal("无更新时不应发 available")
	}
}

func TestCheckerBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewChecker("1.0.0", HTTPSource{URL: srv.URL}, &capEmitter{})
	if _, _, err := c.Check(context.Background()); err == nil {
		t.Fatal("非 200 应返回错误")
	}
}

// —— 双校验：SHA256 + Ed25519 ——

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVerifyPackageOK(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("phpo-installer-bytes")
	path := writeFile(t, dir, "setup.bin", data)
	sha, err := SHA256File(path)
	if err != nil {
		t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(sha)))
	if err := VerifyPackage(path, sha, pub, sig); err != nil {
		t.Fatalf("双校验应通过，得 %v", err)
	}
}

func TestVerifyPackageChecksumFail(t *testing.T) {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(nil)
	path := writeFile(t, dir, "a.bin", []byte("good"))
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("whatever")))
	// 期望 sha 与实际不符
	if err := VerifyPackage(path, "deadbeef", pub, sig); !errors.Is(err, ErrChecksum) {
		t.Fatalf("校验值不符应报 ErrChecksum，得 %v", err)
	}
}

func TestVerifyPackageSignatureFail(t *testing.T) {
	dir := t.TempDir()
	pub, _, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	data := []byte("payload")
	path := writeFile(t, dir, "b.bin", data)
	sha, _ := SHA256File(path)
	badSig := base64.StdEncoding.EncodeToString(ed25519.Sign(otherPriv, []byte(sha)))
	if err := VerifyPackage(path, sha, pub, badSig); !errors.Is(err, ErrSignature) {
		t.Fatalf("签名不符应报 ErrSignature，得 %v", err)
	}
}

func TestVerifyPackageNoKey(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "c.bin", []byte("x"))
	sha, _ := SHA256File(path)
	if err := VerifyPackage(path, sha, nil, ""); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("缺公钥应拒绝，得 %v", err)
	}
}

// —— 调度器：立即检查一次 ——

type countingSource struct{ n int }

func (c *countingSource) FetchLatest(context.Context) (*Release, error) {
	c.n++
	return &Release{Version: "0.0.1"}, nil
}

func TestSchedulerRunsOnce(t *testing.T) {
	src := &countingSource{}
	c := NewChecker("9.9.9", src, &capEmitter{})
	s := NewScheduler(c, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	if src.n < 1 {
		t.Fatalf("启动应立即检查，得检查 %d 次", src.n)
	}
}
