// dockerutil：镜像引用解析与就绪轮询
package dockerutil

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseImageRef(t *testing.T) {
	cases := []struct {
		in     string
		name   string
		tag    string
		digest string
	}{
		{"php:8.4-fpm", "php", "8.4-fpm", ""},
		{"nginx", "nginx", "", ""},
		{"registry.example.com:5000/team/app:1.2", "registry.example.com:5000/team/app", "1.2", ""},
		{"alpine@sha256:abc123", "alpine", "", "sha256:abc123"},
		{"mysql:8.4@sha256:def", "mysql", "8.4", "sha256:def"},
	}
	for _, c := range cases {
		got := ParseImageRef(c.in)
		if got.Name != c.name || got.Tag != c.tag || got.Digest != c.digest {
			t.Errorf("ParseImageRef(%q) = %+v，期望 name=%q tag=%q digest=%q", c.in, got, c.name, c.tag, c.digest)
		}
	}
}

func TestImageRefStringRoundTrip(t *testing.T) {
	if got := (ImageRef{Name: "redis", Tag: "8"}).String(); got != "redis:8" {
		t.Errorf("String 带 tag = %q", got)
	}
	if got := (ImageRef{Name: "nginx"}).String(); got != "nginx:latest" {
		t.Errorf("String 无 tag 应补 latest = %q", got)
	}
	if got := (ImageRef{Name: "php", Tag: "8.4-fpm"}).String(); got != "php:8.4-fpm" {
		t.Errorf("String 带点与后缀 = %q", got)
	}
}

func TestFormatRef(t *testing.T) {
	if got := FormatRef("php", ""); got != "php:latest" {
		t.Errorf("FormatRef 空 tag = %q", got)
	}
	if got := FormatRef("postgres", "17"); got != "postgres:17" {
		t.Errorf("FormatRef = %q", got)
	}
}

func TestWaitPollsUntilReady(t *testing.T) {
	calls := 0
	err := Wait(context.Background(), time.Second, time.Millisecond, func() (bool, error) {
		calls++
		return calls >= 3, nil
	})
	if err != nil {
		t.Fatalf("应轮询至成功: %v", err)
	}
	if calls < 3 {
		t.Errorf("至少调用 3 次，实为 %d", calls)
	}
}

func TestWaitTimeoutAndError(t *testing.T) {
	if err := Wait(context.Background(), 10*time.Millisecond, time.Millisecond, func() (bool, error) {
		return false, nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("超时应返回 DeadlineExceeded, got %v", err)
	}
	boom := errors.New("boom")
	if err := Wait(context.Background(), time.Second, time.Millisecond, func() (bool, error) {
		return false, boom
	}); !errors.Is(err, boom) {
		t.Errorf("cond 错误应立即上抛, got %v", err)
	}
}
