# phpo 顶层 Makefile：与 Taskfile 对齐的常用入口
.PHONY: dev build test vet bindings clean

dev:
	task dev

bindings:
	task bindings

build:
	task build

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf build/bin frontend/dist
	git checkout -- frontend/dist 2>/dev/null || true
