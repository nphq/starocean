#!/bin/bash

# StarOcean Test Runner — Go 全链路（含 -race 集成测试）
#
# 用法:
#   ./scripts/run_tests.sh                 # 默认: 临时 SQLite 库，零外部依赖
#   STAROCEAN_TEST_DSN=postgres://... ./scripts/run_tests.sh   # 切 PostgreSQL
#
# 步骤:
#   1. go vet ./...
#   2. go test -race -count=1 ./...  （STAROCEAN_TEST_DSN 指定后端；默认 sqlite 临时库）
#   3. golangci-lint（可选，安装了才跑；要求 0 问题）
# 提示: 集成测试需要数据库；默认创建临时 SQLite 文件并在结束后删除。

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}==> StarOcean test runner${NC}"

# ---------------------------------------------------------------------------
# 1. 集成测试数据库: 默认临时 SQLite（零依赖）；STAROCEAN_TEST_DSN 可覆盖
# ---------------------------------------------------------------------------
BASE_DB="/tmp/starocean_test_$$"
TEST_DSN="${STAROCEAN_TEST_DSN:-sqlite:${BASE_DB}.db}"
export STAROCEAN_TEST_DSN="$TEST_DSN"
if [[ "$TEST_DSN" == sqlite:* ]]; then
    # 测试包会按 DSN 派生 *_db.db / *_ledger.db 独立文件，一并清理
    trap 'rm -f ${BASE_DB}*' EXIT
fi

# ---------------------------------------------------------------------------
# 2. Go: vet + race 集成测试
# ---------------------------------------------------------------------------
echo -e "${BLUE}==> go vet ./...${NC}"
go vet ./...

echo -e "${BLUE}==> go test -race -count=1 ./...${NC}"
echo -e "${YELLOW}    DSN: $TEST_DSN ${NC}"
if go test -race -count=1 ./...; then
    echo -e "${GREEN}    Go 测试通过（集成测试缺库即失败，杜绝静默跳过假绿；仅性能基线测试需 starocean_perf 库，缺失时自动跳过）${NC}"
else
    echo -e "${RED}    Go 测试失败${NC}"
    exit 1
fi

# ---------------------------------------------------------------------------
# 3. golangci-lint（可选，安装了才跑；严格模式: 0 问题）
# ---------------------------------------------------------------------------
if command -v golangci-lint >/dev/null 2>&1; then
    echo -e "${BLUE}==> golangci-lint run ./... (严格模式: 0 问题)${NC}"
    if golangci-lint run ./...; then
        echo -e "${GREEN}    lint 通过（0 问题）${NC}"
    else
        echo -e "${RED}    lint 未通过${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}==> 未安装 golangci-lint，跳过（可选）${NC}"
fi

echo -e "${GREEN}==> 全部通过！${NC}"
