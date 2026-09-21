// nginx 执行器的容器名必须在每次调用时现取：nginx 版本由用户开放输入（§1.6），装配期写死容器名会让
// 装了非默认版本的机器把 nginx -t / reload exec 到一个不存在的容器上（硬红线 2 失效）。
package vhost

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// runSpy 记录每次执行的 name+args
type runSpy struct {
	calls [][]string
	out   []byte
	err   error
}

func (s *runSpy) run(_ context.Context, name string, arg ...string) ([]byte, error) {
	s.calls = append(s.calls, append([]string{name}, arg...))
	return s.out, s.err
}

func TestNginxTValidatorResolvesContainerPerCall(t *testing.T) {
	ctx := context.Background()
	names := []string{"phpo-nginx-1.25", "phpo-nginx-alpine"}
	i := 0
	spy := &runSpy{}
	v := NewNginxTValidator(func() (string, error) {
		n := names[i]
		i++
		return n, nil
	})
	v.run = spy.run

	for range names {
		if err := v.ValidateFile(ctx, "/x/a.conf"); err != nil {
			t.Fatalf("第 %d 次校验应通过: %v", i, err)
		}
	}
	got := spy.calls
	if len(got) != 2 {
		t.Fatalf("应执行 2 次，实得 %d", len(got))
	}
	for n, c := range got {
		want := []string{"docker", "exec", names[n], "nginx", "-t"}
		if strings.Join(c, " ") != strings.Join(want, " ") {
			t.Errorf("第 %d 次命令 = %v，期望 %v", n+1, c, want)
		}
	}
}

func TestNginxTValidatorPropagatesResolveError(t *testing.T) {
	resolveErr := errors.New("nginx 未安装")
	ran := false
	v := NewNginxTValidator(func() (string, error) { return "", resolveErr })
	v.run = func(context.Context, string, ...string) ([]byte, error) { ran = true; return nil, nil }

	if err := v.ValidateFile(context.Background(), "/x/a.conf"); !errors.Is(err, resolveErr) {
		t.Fatalf("解析失败应原样上抛，实得 %v", err)
	}
	if ran {
		t.Error("容器名未解析出来时不得执行命令")
	}
}

func TestNginxReloaderResolvesContainerPerCall(t *testing.T) {
	ctx := context.Background()
	names := []string{"phpo-nginx-8", "phpo-nginx-1.25"}
	i := 0
	spy := &runSpy{}
	r := NewNginxReloader(func() (string, error) {
		n := names[i]
		i++
		return n, nil
	})
	r.run = spy.run

	for range names {
		if err := r.Reload(ctx); err != nil {
			t.Fatalf("第 %d 次重载应通过: %v", i, err)
		}
	}
	got := spy.calls
	if len(got) != 2 {
		t.Fatalf("应执行 2 次，实得 %d", len(got))
	}
	for n, c := range got {
		want := []string{"docker", "exec", names[n], "nginx", "-s", "reload"}
		if strings.Join(c, " ") != strings.Join(want, " ") {
			t.Errorf("第 %d 次命令 = %v，期望 %v", n+1, c, want)
		}
	}
}

func TestNginxReloaderPropagatesResolveError(t *testing.T) {
	resolveErr := errors.New("nginx 未安装")
	ran := false
	r := NewNginxReloader(func() (string, error) { return "", resolveErr })
	r.run = func(context.Context, string, ...string) ([]byte, error) { ran = true; return nil, nil }

	if err := r.Reload(context.Background()); !errors.Is(err, resolveErr) {
		t.Fatalf("解析失败应原样上抛，实得 %v", err)
	}
	if ran {
		t.Error("容器名未解析出来时不得执行命令")
	}
}
