// 生命周期钩子语义测试：启动顺序执行且遇错中断；关闭逆序执行且不中断
package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestStartupHooksRunInOrderAndStopOnError(t *testing.T) {
	l := NewLifecycle()
	var order []string
	boom := errors.New("boom")
	l.AddStartupHook("a", func(context.Context) error { order = append(order, "a"); return nil })
	l.AddStartupHook("b", func(context.Context) error { order = append(order, "b"); return boom })
	l.AddStartupHook("c", func(context.Context) error { order = append(order, "c"); return nil })

	err := l.OnStartup(context.Background())
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("应传播钩子错误, got %v", err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(order, want) {
		t.Errorf("启动顺序 = %v, 期望 %v（b 失败后 c 不得执行）", order, want)
	}
	if want := "启动钩子 b 失败: boom"; err.Error() != want {
		t.Errorf("错误文案 = %q, 期望 %q", err.Error(), want)
	}
}

func TestShutdownHooksRunReverseAndNeverAbort(t *testing.T) {
	l := NewLifecycle()
	var order []string
	l.AddShutdownHook("x", func(context.Context) error { order = append(order, "x"); return nil })
	l.AddShutdownHook("y", func(context.Context) error { order = append(order, "y"); return errors.New("ignored") })
	l.AddShutdownHook("z", func(context.Context) error { order = append(order, "z"); return nil })

	l.OnShutdown(context.Background())
	if want := []string{"z", "y", "x"}; !reflect.DeepEqual(order, want) {
		t.Errorf("关闭顺序 = %v, 期望 %v 且 y 的错误被忽略", order, want)
	}
}
