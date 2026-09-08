#!/bin/bash

# StarOcean Local Development Script (SQLite，零外部依赖)
# 生成 templ 模板/Tailwind 并运行应用。

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}==> StarOcean development (SQLite, no dependencies)${NC}"

echo -e "${BLUE}==> Generating templates (templ)...${NC}"
if command -v templ &> /dev/null; then
    templ generate
else
    echo -e "${YELLOW}Warning: 'templ' not found. Installing...${NC}"
    go install github.com/a-h/templ/cmd/templ@v0.3.1020
    templ generate
fi

echo -e "${BLUE}==> Generating Tailwind CSS...${NC}"
if command -v npx &> /dev/null; then
    npx @tailwindcss/cli -i ./public/css/tailwind.css -o ./public/css/output.css
else
    echo -e "${YELLOW}Warning: 'npx' not found. Skipping CSS generation.${NC}"
fi

echo -e "${GREEN}==> Running StarOcean at http://localhost:8080${NC}"
echo -e "${YELLOW}Press Ctrl+C to stop${NC}"

# 默认 SQLite 单机运行；PostgreSQL 可通过 DATABASE_URL 覆盖：
#   DATABASE_URL="postgres://starocean:starocean@localhost:5432/starocean?sslmode=disable" ./scripts/dev.sh
go run . serve -secret "dev-secret-key-32-chars!!" -seed -demo-password
