// T604a · downloader 单测：httptest 成功落盘 + 进度累计、404 报错、取消清理 .part
package updater

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDownload_Success(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 200_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "200000")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dst := filepath.Join(t.TempDir(), "sub", "phpo.pkg")
	var lastDone, lastTotal int64
	err := HTTPDownloader{}.Download(context.Background(), srv.URL, dst, func(done, total int64) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("内容不符，长度 %d vs %d", len(got), len(payload))
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("最终进度应为 %d/%d，实得 %d/%d", len(payload), len(payload), lastDone, lastTotal)
	}
	// 成功后不得残留 .part
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("成功后仍残留 .part")
	}
}

func TestDownload_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dst := filepath.Join(t.TempDir(), "pkg")
	if err := (HTTPDownloader{}).Download(context.Background(), srv.URL, dst, nil); err == nil {
		t.Fatal("404 应报错")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("404 不应生成目标文件")
	}
}

func TestDownload_CancelledBefore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	dst := filepath.Join(t.TempDir(), "pkg")
	if err := (HTTPDownloader{}).Download(ctx, srv.URL, dst, nil); err == nil {
		t.Fatal("已取消的 ctx 应报错")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("取消不应生成目标文件")
	}
}

func TestDownload_CancelledMidStream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release // 阻塞，等待测试取消
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	dst := filepath.Join(t.TempDir(), "pkg")
	done := make(chan error, 1)
	go func() {
		done <- (HTTPDownloader{}).Download(ctx, srv.URL, dst, nil)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("流中取消应报错")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后下载未在超时内返回")
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("取消后不应残留 .part")
	}
}
