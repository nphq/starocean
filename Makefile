.PHONY: build templ css htmx run stop dev docker test lint clean seed hooks

build: templ css
	go build -o starocean .

GOPATH_BIN := $(shell go env GOPATH)/bin
PORT ?= 8080

$(GOPATH_BIN)/templ:
	go install github.com/a-h/templ/cmd/templ@v0.3.1020

templ: $(GOPATH_BIN)/templ
	PATH=$(GOPATH_BIN):$$PATH templ generate

$(GOPATH_BIN)/lefthook:
	go install github.com/evilmartians/lefthook@latest

hooks: $(GOPATH_BIN)/lefthook
	PATH=$(GOPATH_BIN):$$PATH lefthook install

css: node_modules
	npx @tailwindcss/cli -i ./public/css/tailwind.css -o ./public/css/output.css --minify

node_modules: package.json
	npm install

# htmx 以 vendored 方式提交在 public/js/htmx.min.js，无需构建；
# 仅在升级版本时执行（当前版本见文件头注释）。
htmx:
	npm pack htmx.org@2.0.10 && tar -xzf htmx.org-2.0.10.tgz package/dist/htmx.min.js \
		&& mv package/dist/htmx.min.js public/js/htmx.min.js && rm -rf package htmx.org-2.0.10.tgz

run: build
	SECRET_KEY=dev-secret-key-32-chars!! ./starocean serve -seed -demo-password -port $(PORT)

stop:
	@pids=$$(lsof -nP -tiTCP:$(PORT) -sTCP:LISTEN 2>/dev/null || true); \
	if [ -n "$$pids" ]; then \
		echo "stopping :$(PORT) ($$pids)"; \
		kill $$pids 2>/dev/null || true; \
		i=0; \
		while [ $$i -lt 20 ] && lsof -nP -tiTCP:$(PORT) -sTCP:LISTEN >/dev/null 2>&1; do \
			i=$$((i+1)); \
			if [ $$i -eq 8 ]; then kill -9 $$pids 2>/dev/null || true; fi; \
			sleep 0.25; \
		done; \
		if lsof -nP -tiTCP:$(PORT) -sTCP:LISTEN >/dev/null 2>&1; then \
			echo "error: :$(PORT) still in use"; exit 1; \
		fi; \
	fi

dev: templ css stop
	go run . serve -secret "dev-secret-key-32-chars!!" -seed -demo-password -port $(PORT)

docker:
	docker build -t starocean .

# 集成测试默认用单次运行的 SQLite 临时库（test helpers 缺库曾静默 Skip，导致假绿）；
# 各测试包自动追加后缀（如 _ledger.db）避免并行冲突，跑完自动删除。可用
#   make test STAROCEAN_TEST_DSN=sqlite:<path>
# 指定其他库。
test:
	@if [ -n "$(STAROCEAN_TEST_DSN)" ]; then \
		STAROCEAN_TEST_DSN="$(STAROCEAN_TEST_DSN)" go test -count=1 ./...; \
	else \
		TMP=$$(mktemp /tmp/starocean-test-XXXXXX); \
		rm -f "$$TMP"; \
		trap 'rm -f "$$TMP"*' EXIT INT TERM; \
		STAROCEAN_TEST_DSN="sqlite:$$TMP.db" go test -count=1 ./...; \
	fi

# 严格 lint（发现任何问题即非零退出）。依赖 golangci-lint v2：
#   brew install golangci-lint  或
#   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
lint:
	golangci-lint run --timeout=5m ./...

clean:
	rm -f starocean
	rm -f public/css/output.css

seed: build
	./starocean seed -demo-password -products 100000 -secret dev-secret-key-32-chars!!
