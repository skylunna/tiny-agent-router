# === 全局配置 ===
APP_NAME := tiny-agent-router
GO := go
GOFLAGS := -ldflags="-s -w"
RUST_DIR := cache-service
RUST_TARGET := x86_64-unknown-linux-musl

# === 默认目标 ===
.PHONY: help
help: ## 📚 显示帮助信息
	@echo "🛠️  $(APP_NAME) - Makefile Commands"
	@echo ""
	@echo "🔹 Go 相关:"
	@echo "  make build          - 编译 Go 二进制到 bin/"
	@echo "  make run            - 本地运行 Go 服务（加载 .env）"
	@echo "  make test           - 运行 Go 单元测试"
	@echo "  make clean          - 清理 Go 构建产物"
	@echo ""
	@echo "🦀 Rust 相关:"
	@echo "  make rust-build     - 编译 Rust cache-service（release）"
	@echo "  make rust-run       - 本地运行 Rust 服务（:50051）"
	@echo "  make rust-clean     - 清理 Rust 构建产物"
	@echo "  make rust-check     - 运行 fmt + clippy 检查"
	@echo ""
	@echo "🚀 开发模式:"
	@echo "  make dev            - 并行启动 Go + Rust（开发调试）"
	@echo "  make observe        - 启动 Prometheus+Grafana 可观测栈"
	@echo "  make clean-observe  - 停止可观测栈"
	@echo ""
	@echo "🐳 构建部署:"
	@echo "  make docker-build   - 构建 Go 镜像（Step 4.3 将支持 Rust）"
	@echo "  make docker-compose - 一键启动全部服务（路由+缓存+监控）"
	@echo ""
	@echo "🧹 清理:"
	@echo "  make clean-all      - 清理 Go + Rust + Docker 产物"

# === Go 相关命令 ===
.PHONY: build run test clean docker-build

build: ## 🔨 编译 Go 二进制
	@echo "🔨 Building $(APP_NAME)..."
	$(GO) build $(GOFLAGS) -o bin/$(APP_NAME) ./cmd/router
	@echo "✅ Binary ready: bin/$(APP_NAME)"

run: ## ▶️ 本地运行（自动加载 .env）
	@echo "🚀 Starting $(APP_NAME) on :7722..."
	@export $$(cat .env 2>/dev/null | grep -v '^#' | xargs) && \
	$(GO) run ./cmd/router

test: ## 🧪 运行单元测试 + 覆盖率
	@echo "🧪 Running tests..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	@echo "📊 Coverage report: coverage.out"

clean: ## 🧹 清理 Go 构建产物
	@echo "🧹 Cleaning Go artifacts..."
	rm -rf bin/ coverage.out

docker-build: ## 🐳 构建 Docker 镜像（Go only, Step 4.3 将集成 Rust）
	@echo "🐳 Building Docker image..."
	docker build -t $(APP_NAME):latest .

# === Rust 相关命令 ===
.PHONY: rust-build rust-run rust-clean rust-check

rust-build: ## 🦀 编译 Rust cache-service（release 模式）
	@echo "🦀 Building semantic-cache (release)..."
	cd $(RUST_DIR) && cargo build --release --target $(RUST_TARGET)
	@echo "✅ Rust binary: $(RUST_DIR)/target/$(RUST_TARGET)/release/semantic-cache"

rust-run: ## ▶️ 本地运行 Rust 服务
	@echo "🦀 Starting semantic-cache on :50051..."
	cd $(RUST_DIR) && cargo run

rust-clean: ## 🧹 清理 Rust 构建产物
	@echo "🧹 Cleaning Rust artifacts..."
	cd $(RUST_DIR) && cargo clean

rust-check: ## 🔍 运行 Rust 格式检查 + clippy
	@echo "🔍 Running Rust checks..."
	cd $(RUST_DIR) && cargo fmt -- --check
	cd $(RUST_DIR) && cargo clippy -- -D warnings -D clippy::pedantic
	@echo "✅ Rust checks passed"

# === 开发模式 ===
.PHONY: dev

dev: ## 🚀 并行启动 Go + Rust（开发调试专用）
	@echo "🚀 Starting dev mode: $(APP_NAME) + semantic-cache"
	@echo "   → Go router: http://localhost:7722"
	@echo "   → Rust cache: grpc://localhost:50051"
	@echo "   → Press Ctrl+C to stop all"
	@trap 'kill 0' SIGINT SIGTERM; \
	$(MAKE) rust-run > /tmp/rust.log 2>&1 & \
	RUST_PID=$$!; \
	echo "   [$$RUST_PID] Rust service started"; \
	sleep 2; \
	if kill -0 $$RUST_PID 2>/dev/null; then \
		$(MAKE) run > /tmp/go.log 2>&1; \
	else \
		echo "❌ Rust service failed to start, check /tmp/rust.log"; \
		exit 1; \
	fi

# === 可观测性栈（Step 3） ===
.PHONY: observe clean-observe

observe: ## 📊 启动 Prometheus + Grafana 可观测栈
	@echo "📊 Starting observability stack..."
	@docker-compose -f docker-compose.observability.yml up -d
	@echo "✅ Services ready:"
	@echo "   → tiny-agent-router: http://localhost:7722"
	@echo "   → Prometheus: http://localhost:9090"
	@echo "   → Grafana: http://localhost:3000 (admin/admin)"
	@echo "   → Metrics endpoint: http://localhost:7722/metrics"

clean-observe: ## 🛑 停止可观测栈
	@echo "🛑 Stopping observability stack..."
	docker-compose -f docker-compose.observability.yml down -v

# === 一键启动全部（开发/演示） ===
.PHONY: docker-compose

docker-compose: ## 🐳 一键启动路由+缓存+监控（需先 build）
	@echo "🐳 Starting full stack via docker-compose..."
	@docker-compose -f docker-compose.observability.yml up -d
	@echo "✅ Full stack ready. Check logs: docker-compose logs -f"

# === 清理所有 ===
.PHONY: clean-all

clean-all: clean rust-clean ## 🧹 清理 Go + Rust + 临时文件
	@echo "🧹 Cleaning all artifacts..."
	rm -rf /tmp/rust.log /tmp/go.log
	@echo "✅ Clean complete"