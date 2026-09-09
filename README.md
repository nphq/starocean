# StarOcean

<p align="center">
  <img src="public/icon.png" width="120" alt="StarOcean logo">
</p>

[![CI](https://github.com/nphq/starocean/actions/workflows/ci.yml/badge.svg)](https://github.com/nphq/starocean/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**单二进制、零依赖的轻量开源 ERP：进销存、应收应付、简易总账，开箱即用。**

StarOcean 专为中小企业（SME）设计：一个二进制文件 + 一个 SQLite 文件即完成部署，
覆盖商贸与小制造最常用的单据流转与账目闭环。不承诺替代 SAP——只把“做账不出错”
这件事做扎实。

## 特性

- **🚀 极简部署** — 单二进制文件运行 `./starocean serve`，10 秒完成部署，日常运行内存仅约 50MB。
- **💼 进销存闭环** — 客户/供应商档案、销售订单（草稿→确认扣库存→发货→开票）、采购订单（草稿→确认→收货加库存→付款）、实时库存流水、安全库存预警。
- **💰 往来与总账** — 收付款登记、应收应付统计、费用报销流、发票录入与作废、对账确认、会计科目/凭证/试算/三大报表、期间结账。
- **🔌 扩展约定** — 核心零臃肿，通过 Event Hooks 事件约定 + `properties` 扩展字段承接定制，无解释器、无动态加载（见 `docs/PLUGINS.md`）。
- **🌓 现代化 UI/UX** — 服务端渲染（templ + Tailwind CSS 4），无前端构建链，原生深色模式，列表搜索/分页局部刷新。

> 明确未做：生产制造（BOM/工单/MRP）、多仓库/库位、采购退货、发票三单匹配、
> 成本中心、库存盘点、多币种。详见 `docs/DESIGN.md §0.6`。

## 技术栈

| 层 | 选择 |
|---|---|
| 后端 | Go 1.26 + Gin（页面 HTML + JSON API 并存） |
| 数据库 | **Turso 文件库（SQLite 兼容引擎，纯 Go、无 CGO，单机默认）** |
| SQL | 手写参数化查询（database/sql + tursogo，无 ORM） |
| 前端 | templ 服务端模板 + Tailwind CSS 4 |
| JS 运行时 | 无框架，仅少量内联脚本（深色模式/确认框） |
| 迁移 | 内嵌单一基线 `schema.sql`（Turso 方言，幂等，可重复执行） |

## 快速开始

### 本地开发 (一键运行)

我们提供了一个脚本来自动处理数据库启动、模板生成和应用运行：

```bash
chmod +x scripts/dev.sh
./scripts/dev.sh
```

该脚本将自动执行以下操作：
1. 生成 `templ` 模板与 Tailwind CSS。
2. 启动应用，监听 `http://localhost:8080`（演示账号: `admin` / `3dQAKbZHqP6P`）。

默认 Turso 文件库单机运行（SQLite 兼容，零外部依赖）。

### Docker 镜像（可选）

```bash
docker build -t starocean .
docker run -d -p 8080:8080 \
  -e SECRET_KEY="$(openssl rand -base64 32)" \
  -v starocean-data:/data \
  starocean
# 访问 http://localhost:8080（演示账号: admin / 3dQAKbZHqP6P，首次启动如需演示数据请先 seed）
```

> 生产环境 `SECRET_KEY` 必须覆盖随机值；SQLite 数据落在 `/data/starocean.db`（已挂 volume 持久化）。

### 手动运行（单机 SQLite，零依赖，推荐）

```bash
make build

# 迁移 + 演示数据（默认数据库文件: ./starocean.db，也可在 serve 时自动执行/写入）
# 注意：seed 默认拒绝演示口令——本地评估加 -demo-password，生产设置 ADMIN_PASSWORD
./starocean seed -demo-password

# 启动（无需任何外部服务）
./starocean serve -secret "更换为你的随机密钥"
# 或
DATABASE_URL="sqlite:data/starocean.db" ./starocean serve -secret "..."
# Turso DSN 前缀亦可：DATABASE_URL="turso:data/starocean.db"
```

> 升级注意：旧版本创建的 `.db` 文件含 STORED 生成列，Turso 引擎无法解析
> 其 schema，请重建库（备份数据 → 新版 `migrate` + `seed`，或按表导出导入）。
> 新版 `schema.sql` 已把计算列写成普通列 + 触发器。

访问 http://localhost:8080，演示账号 `admin` / `3dQAKbZHqP6P`（由 `-seed` 写入）。

## CLI 命令

```bash
./starocean serve    # 启动 Web 服务 (默认 :8080，启动时自动执行数据库迁移)
./starocean migrate  # 仅执行数据库迁移后退出
./starocean seed     # 写入演示数据后退出 (可加 -products N / -clear；需 ADMIN_PASSWORD 或 -demo-password，否则拒绝执行)
```

> 数据库连接串：默认 `sqlite:starocean.db`（Turso 引擎承载，单机零依赖），
> 亦可用 `turso:<path>` 前缀（可透传 `?experimental=` 等 Turso DSN 参数）。

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DATABASE_URL` | `sqlite:starocean.db` | 数据库连接（`sqlite:<path>` 或 `turso:<path>`） |
| `PORT` | `8080` | 监听端口 |
| `SECRET_KEY` | （必填，启动时校验） | Session 加密密钥，请使用随机值 |
| `COMPANY_NAME` | `StarOcean` | 公司/品牌名（用于页面标题与打印单据抬头） |
| `DEBUG` | `false` | 调试模式 |


## 模块

### 销售管理
- 销售订单：draft → confirmed → shipped → invoiced
- 确认时自动扣减库存（事务保证一致性）
- 订单明细在详情页逐行添加（服务端表单，无 JS 框架）

### 采购管理
- 采购订单：draft → confirmed → received → paid
- 收货时自动增加库存

### 库存管理
- 商品主数据（SKU、分类、单位、售价、成本价）
- 实时库存 + 安全库存预警
- 库存流水（入库/出库记录）

### 财务管理
- 应收应付总览
- 收付款登记
- 现金流一览（本月收入/支出/净额）

## 开发

```bash
make dev       # 生成 templ + CSS 后启动 Go（SQLite）
make build     # 构建二进制
make test      # Go 全量测试（默认临时 SQLite，零依赖）
make hooks     # 安装 Git hooks (lefthook)：提交前 gofmt/vet/生成物一致性，推送前全量构建与测试
```

## 参与贡献与安全

- 想贡献代码？先读 [`CONTRIBUTING.md`](CONTRIBUTING.md)（核心保持精简的约定 + 提交规范）。
- 发现安全漏洞？**不要开公开 Issue**，见 [`SECURITY.md`](SECURITY.md)。
- 生产 seed 请设置 `ADMIN_PASSWORD`，演示密码仅供本地评估。

## 许可

MIT — 详见 [LICENSE](LICENSE)
