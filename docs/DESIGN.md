# StarOcean Design Document

> **文档定位**：本文保留最初的产品/技术战略论证；**以「§0 现状架构快照」为实施事实来源**（2026-09 重写：全站回归 templ + htmx，SPA/插件引擎/AI 助手均已移除）。
> §0 之后的历史章节（§2.4/§2.5/§4 等）凡与快照冲突之处，以快照为准，保留原文仅作选型档案。

## 0. 现状架构快照（2026-09）

> 本节由实施反向生成，与代码同步维护；任何大版本改动请同步本节。

### 0.1 技术栈（现状）

| 层 | 现状选择 | 说明 |
|---|---|---|
| 后端 | Go 1.26 + Gin（**页面 HTML + JSON API 并存**） | `internal/web` 渲染 templ 页面（表单 POST+303 跳转、列表 htmx 局部刷）；`/api/*` JSON 保留，集成测试覆盖 |
| 前端 | **templ 服务端模板 + htmx 2.x + Tailwind CSS 4** | htmx 以 vendored 方式提交（`public/js/htmx.min.js`，`make htmx` 升级）；**零前端构建链**（SvelteKit/Bun/Vite 已删）；深色模式靠内联脚本 + localStorage |
| 数据库 | **SQLite 默认（单机零依赖）** / PostgreSQL 16 可选 | 同一份 SQL 通过驱动层翻译（`internal/db/sqlite*.go`：`::cast/ILIKE/NOW()/FOR UPDATE/gen_random_uuid/INTERVAL/jsonb合并` 等 + DDL 翻译）双库运行 |
| 迁移 | 内嵌迁移 23 版；PG 用 golang-migrate，SQLite 用 DDL 翻译执行 | 含 GL（023） |
| 打印 | templ（`/sales/:id/print` 等 6 类，与业务页同一模板体系） | `view/print` + `view/layout/print.templ` |
| 扩展 | **事件约定（`internal/hooks`）+ `properties` 扩展字段**（详见 `docs/PLUGINS.md`） | Goja JS 引擎、UI Slot、自定义字段 API 均已删除；无解释器、无动态加载 |
| 总账 | `internal/ledger`：会计科目/期间/凭证/账簿/结账 + **事件驱动自动过账**（单据确认/取消/付款/报销，与业务同事务 + 幂等唯一索引） | 迁移 023 |
| AI | **已删除**（`internal/assistant`、`assistant-index` CLI、`docs/ask.md` 均已移除，全仓零引用） | 如需 AI，另起独立服务调本系统 API |
| 会话 | HMAC Cookie（SameSite=Lax，Secure 跟随代理协议）+ 登录限流 | 单租户，最小 RBAC（admin 改单 / viewer 只看账，见 0.6） |

### 0.2 后端包结构（现状）

```
internal/
  db/          连接/迁移/两套 DDL 翻译与 SQL 重写(golden 测试锁定)
  models/      手写模型 + 状态机(domain.go)
  shared/      通用工具(JSON/金额/分页/全局搜索)
  hooks/       事件注册表（扩展约定的唯一载体，见 docs/PLUGINS.md）
  server/      gin 路由/静态资源/中间件（HTML 页面经 web 注册，JSON 经各模块 handler）
  web/         服务端渲染处理器（HTML 表单/列表/详情，与 JSON handler 共享业务函数）
  middleware/  会话/认证/限流
  orders/  products/  customers/  suppliers/  inventory/  finance/  核心进销存+应收应付
  ledger/      总账(凭证/结账/自动过账)   personnel/  attendance/  workreports/  partnernotes/
  picking/     拣货        print/      打印模板 handler
view/
  layout/      Base 主布局（侧栏/<details>手风琴导航）与打印布局
  component/   表格/表单/分页/徽标等 htmx 组件
  pages/       各业务模块页面模板（列表片段复用于 htmx 局部刷新）
  vmodel/      模板与 handler 共享的视图模型（防 import cycle）
  print/       6 类打印模板
public/
  css/         Tailwind 4（@source 扫描 view/，产物 output.css 随二进制 embed）
  js/          htmx 2.x（vendored，无构建）
```

### 0.3 CLI / 环境

- 子命令：`serve`（默认，自动迁移）/ `migrate` / `seed`
- 关键 env：`DATABASE_URL`（默认 `sqlite:starocean.db`）、`SECRET_KEY`（必填）、`PORT`、`COMPANY_NAME`、`DEBUG`

### 0.4 部署形态

- **单机推荐**：单二进制 + 一个 `.db` 文件（无任何外部服务）
- Docker：仅 `Dockerfile` 构建单二进制镜像（SQLite 落盘 `/data`）；无 compose（已删除，见 §0.7）；生产单机部署见 `docs/deploy-home-vm-tunnel.md`

### 0.5 测试

- Go：30 个集成/单元测试（HTTP API 全链路、HTML 页面冒烟、并发双击确认、支付 CAS、GL 过账幂等、翻译层 golden），`STAROCEAN_TEST_DSN` 一键切 SQLite/PG，`-race` 全绿
- lefthook：提交前 gofmt/vet/templ 生成物/Tailwind 产物一致性，推送前全量构建+测试

### 0.6 已知边界（现状）

- 单租户：`company_id` 全为 `default`；最小 RBAC（`users.role`：admin 可写，viewer 只读，写接口 403）——多角色/细粒度权限是 SaaS 化前置工作
- `quantity INT`（称重等小数场景靠 `pricing_type=weight` 打补丁）
- 搜索：SQLite 走 LIKE（无 FTS5），PG 走 trgm GIN
- 高级人事（岗位管理/薪资批量/期初录入）暂只保留 JSON API，页面按需补齐

### 0.7 与早期设计的偏差对照

| 早期文案 | 现状 | 处理 |
|---|---|---|
| SvelteKit SPA（打印保留 templ） | **已删除**：2026-09 全站回归 templ + htmx（`internal/web` + `view/`），零前端构建链 | 已迁移 |
| PostgreSQL 唯一依赖 | SQLite 默认 / PG 可选 | 翻译层双库 |
| sqlc 类型安全 | 手写参数化 SQL + golden 测试 | 放弃 sqlc |
| Wazero Wasm 插件 | 未实现 | roadmap 保留 |
| Goja JS 插件引擎 + UI Slot + 自定义字段 | **已删除**：降级为事件约定 + `properties` 扩展（`docs/PLUGINS.md`） | 已调整 |
| AI 助手（RAG + 工具调用） | **已删除**（代码/CLI/文档/路由全清） | 已调整 |
| docker-compose.yml + deploy.sh（PG 容器生产链） | **已删除**：SQLite 默认不再需要 compose；单机部署见 `docs/deploy-home-vm-tunnel.md` | 已调整 |
| `/plugins` 目录热加载 | 从未落地，无需处理 | — |

## 一、为什么做

### 1.1 市场现状与定位

| 方案 | 年费 | 技术栈 | SME 能用？ | 架构与定制能力 |
|---|---|---|---|---|
| **SAP Business One** | ¥20-50万 | ABAP + HANA | ❌ 太贵太重 | 经典臃肿，实施周期长达数月 |
| **Oracle NetSuite** | ¥30-80万 | SuiteScript | ❌ 太贵太重 | 云端封闭，定制费用昂贵 |
| **用友 U8+ / 金蝶 K/3** | ¥10-30万 | .NET + SQL Server | ❌ 部署复杂 | 绑定 Windows，架构陈旧 |
| **Odoo 社区版** | ¥0（自行部署） | Python + JS | ⚠️ Python 部署门槛 | 插件多但容易冲突，性能较差 |
| **ERPNext** | ¥0（自行部署） | Python + MariaDB | ⚠️ Python 部署门槛 | Frappe 框架重，启动慢，内存消耗大 |
| **StarOcean** | **¥0（开源自部署）** | **Go 1.26 + SQLite** | **✅ 10秒部署，极度轻量** | **进销存 + 往来 + 简易总账，单二进制** |

> [!IMPORTANT]
> **StarOcean 的战略定位：以极轻量级架构，提供 70% 功能的 SAP 替代方案。**
> 
> 我们不去做 100% 的 SAP。相反，我们剥离了 SAP 中 30% 仅适用于超大型跨国集团的超复杂业务逻辑（如多 GAAP 并行核算、全球内部交易对账、超复杂生产产能详细调度等），仅保留最能解决企业痛点的 **70% 核心黄金功能**。
> 
> 同时，为了防止功能增加导致系统重新变得臃肿，StarOcean 采用 **“极简核心 + 扩展约定”** 的设计哲学。核心层极其干净、高效，而所有特定行业属性、个性化流程的定制逻辑，全部走事件约定 + 扩展字段 + 外部集成（`docs/PLUGINS.md`），不接受运行时上传代码。

### 1.2 目标客户

| 客户画像 | 规模 | 核心需求与痛点 | 为什么选择 StarOcean |
|---|---|---|---|
| **小型制造企业** | 20-100 人 | 进销存 + 简单生产流转，Excel 管理易错 | 先用进销存与库存流水替代 Excel；生产制造（BOM/工单）暂不支持 |
| **商贸贸易公司** | 5-30 人 | 销售订单跟踪 + 客户账期管理 + 采购入库核销 | 5分钟搞定部署，流畅的 htmx 现代界面体验 |
| **项目型初创团队** | 10-50 人 | 成本核算 + 快速定制自己特有的业务规则 | 事件约定清晰，定制逻辑与核心解耦、升级不 break |

### 1.3 核心设计原则

1. **部署无感（Ultra-lightweight）**：`./starocean` 单二进制启动，PostgreSQL/SQLite 是唯一依赖，启动 <1 秒，日常内存占用仅约 50MB。
2. **能力诚实**：只承诺代码里真实存在的东西（见 §三），不拿 SAP 模块对标，不写“规划中”的功能当卖点。
3. **扩展约定（Docs + Conventions）**：绝不在核心代码中写“客制化（Customization）”代码。所有行业特有需求（如：服装鞋帽的尺码矩阵、医药行业的批号追踪、特定客户的折扣算法）全部走事件约定 + 扩展字段 + 外部集成（`docs/PLUGINS.md`），核心保持零依赖。
4. **体验第一**：基于 Tailwind CSS 4 提供媲美现代化 SaaS 的暗黑模式与交互，操作响应均在毫秒级。

---

## 二、技术决策

### 2.1 选型理由

| 层 | 选择 | 为什么 |
|---|---|---|
| 后端 | **Go 1.26 + Gin** | 单二进制分发、高性能、极致内存控制、原生静态编译。 |
| 数据库 | **SQLite 默认 / PostgreSQL 可选**（历史：仅 PG） | 单机零依赖优先；SQL 由驱动层翻译双库运行。 |
| 模板引擎 | **templ（全站页面 + 打印）** | Go 原生类型安全模板，编译时检查；页面模板在 `view/pages`，片段复用于 htmx 局部刷新。 |
| 前端交互 | **htmx 2.x（vendored，无构建）** | 列表搜索/分页只换 `#list` 区域；表单走原生 POST+303；危险操作走原生 `confirm()`。无前端框架、无 Node 构建链。 |
| CSS | **Tailwind CSS 4** | 现代 UI，内置强大的 Dark Mode 支持，基于 CSS-first 配置。 |
| 图标 | **Heroicons** | 内联 SVG。 |
| 迁移 | **golang-migrate（PG）+ DDL 翻译（SQLite）** | 23 版内嵌迁移；SQL 为手写参数化 + golden 对照测试。 |
| 扩展机制 | **事件约定 + `properties` 扩展字段（`docs/PLUGINS.md`）** | Goja JS 引擎已删除（见 §2.5 档案说明）。定制走 `internal/hooks` 事件与外部集成，不接受运行时上传代码。 |

### 2.2 技术栈架构图（现状）

```
                         ┌──────────────────────────────────────────────┐
                         │              浏览器 / 客户端                  │
                         │    ┌──────────────────┬─────────────────┐    │
                         │    │ htmx 2.x (局部刷新│  Tailwind 4 UI  │    │
                         │    │ #list / 原生表单) │  (+ 深色模式)   │    │
                         │    └──────────────────┴─────────────────┘    │
                         └──────────────────────┬───────────────────────┘
                                                │ HTTP: 整页 HTML / HTML 片段 /
                                                │       表单 POST(→303 跳转)
                                                ▼
                         ┌──────────────────────────────────────────────┐
                         │                 StarOcean 核心                 │
                         │    ┌────────────────────────────────────┐    │
                         │    │           Gin 路由 & 中间件        │    │
                         │    │   HTML 页面(internal/web) + JSON   │    │
                         │    └─────────────────┬──────────────────┘    │
                         │                      │                       │
                         │    ┌─────────────────▼──────────────────┐    │
                         │    │  templ 模板(view/layout+component  │    │
                         │    │  +pages) 服务端渲染                │    │
                         │    └─────────────────┬──────────────────┘    │
                         │                      │                       │
                         │    ┌─────────────────▼──────────────────┐    │
                         │    │   Event Hooks 事件注册表 (Memory)    │    │
                         │    │   (docs/PLUGINS.md 扩展约定)       │    │
                         │    └─────────────────┬──────────────────┘    │
                         └──────────────────────┼───────────────────────┘
                                                │ SQL(参数化, PG 风格)
                                                ▼
                         ┌──────────────────────────────────────────────┐
                         │                 数据持久层                   │
                         │    ┌────────────────────────────────────┐    │
                         │    │  SQLite 默认 / PostgreSQL 16 可选   │    │
                         │    │  (驱动层方言翻译, properties 扩展)  │    │
                         │    └────────────────────────────────────┘    │
                         └──────────────────────────────────────────────┘
                         注: 重型集成(WMS/MES/复杂计价)走独立服务调本系统
                         API,不进单二进制(见 docs/PLUGINS.md 约定三)。
```

### 2.3 与经典 ERP 架构对比

| 维度 | SAP S/4HANA | ERPNext | StarOcean |
|---|---|---|---|
| **核心语言** | ABAP / C++ | Python (Frappe) | **Go 1.26** |
| **部署分发** | 极重，需要专业团队与专用服务器 | bench 命令行或大型 Docker | **单二进制文件 `./starocean`** |
| **启动时间** | 数十分钟 | 30 - 60 秒 | **<1 秒** |
| **运行内存** | 至少 64GB 起 | 至少 1GB - 2GB | **~50MB** |
| **前端模式** | Fiori / Heavy Web | Vue 3 SPA | **templ 服务端渲染 + htmx 局部刷新（零构建）** |
| **客制化方式** | 复杂的 ABAP 开发与传输配置 | 自定义 DocType (底层表结构频繁变更) | **事件约定 + `properties` 扩展字段 + 外部集成（无脚本引擎）** |
| **数据库** | SAP HANA (内存数据库) | MariaDB / PostgreSQL | **SQLite 默认（零依赖）/ PostgreSQL 16 可选** |

### 2.4 扩展架构设计 (Docs + Conventions)

> 现状（2026-09）：Goja 引擎、UI Slot、自定义字段 API 已删除。本节只保留三条现行约定，
> 全文见 `docs/PLUGINS.md`；§2.5 的引擎选型对比保留为历史档案。

为了保证核心的高度轻量化，StarOcean 绝不包含非通用的客制化代码。系统通过以下三条约定实现扩展：

#### 1. 基于 `internal/hooks` 的事件机制
在业务生命周期（例如订单创建、状态确认、入库、付款）的黄金切面点，核心系统会发布事件。
前置事件（`*.creating/confirming/...`）可拦截拒绝并回填 `Patches`；后置事件（`*.created/confirmed/...`）
异步执行、panic 隔离。Payload 键名只增不改：
- `sales_order.confirmed` -> 自动向客户发送通知短信，或自动推送第三方物流系统。
- `stock.adjusted` -> 触发库存同步，同步至电商平台。

#### 2. `properties` 物理层扩展
核心单据表（`products`, `sales_orders`, `purchase_orders`, `customers`, `suppliers`）均内置
`properties` 列（PG 为 JSONB，SQLite 为 TEXT + JSON1 函数）。
- 行业属性（如包装规格、危险品标识、客户偏好标签）全部存这里，不加物理列。
- PG 侧按需加 GIN 索引；SQLite 侧走 LIKE/JSON1。

#### 3. 外部集成优先于内部脚本
通知类走独立 worker/webhook 中继；校验拦截类写成同进程 Go handler 随版本发布；
**不接受运行时上传代码**（动态注入曾是 XSS 与升级 break 的主要来源）。

### 2.5 动态脚本与插件引擎选型对比（历史档案，已废止）

> ⚠️ 本节为 2026-09 之前的选型论证，结论（选用 Goja）**已被推翻**：
> JS 插件引擎、UI Slot、自定义字段 API 已从代码中删除（见 §0.1/§0.7 与 `docs/PLUGINS.md`）。
> 下表保留为档案，不作为实施依据。

为了给 StarOcean 寻找最符合“轻量化部署、强隔离沙箱、极低实施难度”诉求的脚本与插件底座，我们对目前 Go 生态内的几种主流运行时扩展方案进行了深度对比评估：

| 方案 | 语言支持 | 依赖与跨平台 | 性能表现 | 沙箱隔离度 | 用户上手难度 | 适用场景 |
|---|---|---|---|---|---|---|
| **Goja (ECMAScript 5.1/6)** | JavaScript | **✅ 纯 Go (无 CGO)**<br>完美跨平台 | 中等 (纯解释执行，但启动极快) | **✅ 极高** (纯内存沙箱，默认无法访问任何系统 I/O) | **✅ 极低** (JS 生态极大，前/后端及实施人员均熟悉) | **核心推荐：** 动态表单校验、折扣计价规则、轻量生命周期 Hooks |
| **Gopher-Lua** | Lua 5.1 | **✅ 纯 Go (无 CGO)**<br>完美跨平台 | **🚀 极高** (解释器中性能最优，内存占用极低) | **✅ 极高** (天然沙箱隔离，API 暴露受限) | 中等 (需要学习 Lua 语法，非主流 web 语言) | 高频数据转换、超高性能的定制算法过滤 |
| **Wazero (WebAssembly)** | Wasm (C/Rust/Go/TS等编译产物) | **✅ 纯 Go (无 CGO)**<br>完美跨平台 | **🚀 极高** (JIT 编译，接近原生执行速度) | **🛡️ 硬件级强隔离** (绝对的安全沙箱) | 极高 (用户无法直接在网页端写代码，必须本地编译 Wasm 并上传) | 行业极客插件、计算密集型复杂算法扩展、不透明闭源算法插件 |
| **Yaegi (Go 解释器)** | Go (运行时解释执行) | **✅ 纯 Go (无 CGO)**<br>完美跨平台 | 较低 (由于编译 Go 抽象语法树较重) | ❌ 极低 (脚本能完全访问系统内存及反射，安全隐患高) | 极高 (必须熟练掌握 Go 语言底层) | 官方大型可选模块的热加载或动态中间件加载 |
| **V8go (Google V8 绑定)** | 现代 JavaScript | ❌ 需要 CGO (依赖庞大 C++ 库，跨平台极其困难) | **🚀 极高** (JIT 编译，顶级 JS 引擎性能) | **✅ 极高** (V8 沙箱隔离) | **✅ 极低** (主流 JS 语法) | 拥有大量并发计算、极致性能要求且不介意编译复杂度的超大企业级项目 |
| **HashiCorp go-plugin** | 任何语言 (多进程 RPC) | ❌ 依赖多进程管理 (部署包变大) | 中等 (存在跨进程/网络通信开销) | 较好 (进程隔离，但需要 OS 级容器权限控制) | 中等 (需要编写完整的独立应用程序) | 适用于超大型独立系统集成（如：智能立体仓库 WMS、大型智能工厂控制套件） |

#### 为什么 StarOcean 默认选用 Goja？
1. **零 CGO 束缚，坚守单二进制原则**：`v8go` 等方案虽然性能强悍，但强依赖 CGO 交叉编译，这会彻底摧毁 Go 单二进制包“10秒跨平台即下即用”的极致部署体验。而 Goja 作为纯 Go 实现，具备天然的免环境依赖与跨平台性。
2. **极高的安全沙箱上限**：ERP 系统的插件化常常需要向第三方或最终用户开放。Goja 运行在完全被控的 Go 内存沙箱中，除非 Go 核心层显式注入，否则 JS 脚本无法读取任何文件系统、环境变量或发起网络连接，这彻底避免了恶意插件导致的服务器被黑风险。
3. **最平缓的实施与定制曲线**：在 ERP 的实际实施中，客户的网管、IT 或外部独立外包人员最熟悉的语言必定是 JavaScript。相比于 Lua，使用 JS 作为脚本扩展语言，能够最大化降低企业的定制与维护成本。

*(注：后续针对有极致计算需求、需要使用 Rust 或 Go 编写的高性能第三方计算插件，StarOcean 会在插件引擎中增加基于 **Wazero** 的 Wasm 插件插槽，以此作为高阶扩展补充。)*

#### 附：Wazero 社区活跃度与生产实践评估

为了验证将 **Wazero** 作为 StarOcean 高阶插件技术路线的安全性和可持续性，我们对其开源社区和生产环境落地情况进行了全面评估：
1. **社区基建与商业背书**：Wazero GitHub 目前已拥有 **~6.1k Stars**，由知名的云原生服务网格（Istio/Envoy 生态）领军企业 **Tetrate** 专职团队进行赞助和全职维护。其规范更新极快，支持完整的 WebAssembly 1.0 和 2.0 标准规范，生命周期安全且不受单一爱好者维护影响。
2. **零 CGO 生产验证**：Wazero 最大的竞争优势是**零 CGO 依赖**。这意味着在执行 JIT（即时编译）提升性能的同时，不会破坏 Go 单二进制的交叉编译便利性，是目前 Go 社区运行 Wasm 唯一的生产级纯 Go 引擎。
3. **顶流工业级项目背书**：Wazero 已在大量顶流基础架构和云原生项目（CNCF 生态）中作为核心 Wasm 引擎在生产环境跑通：
   - **Redpanda**：现代流处理数据平台，使用 Wazero 在 Go 侧实时运行第三方的 Wasm 数据异动与转换处理器。
   - **Dapr**：CNCF 孵化的分布式应用运行时，使用 Wazero 来热加载 Wasm 中间件扩展。
   - **Trivy (Aqua Security)**：云原生安全扫描的行业标准，利用 Wazero 加载第三方的 Wasm 安全扫描规则插件。
   - **Coraza WAF (OWASP)**：现代 Go 语言 Web 应用防火墙，内置 Wazero 来安全隔离并高效解析 OWASP 核心防护规则集。
   - **Arcjet & RunReveal**：现代安全组件，在 Go 后端使用 Wazero 安全地沙箱隔离运行第三方的 bot 检测与数据过滤逻辑。

**评估结论**：Wazero 已经从一个早期的尝试性项目演变为 Go WebAssembly 运行时事实上的**工业级标准**。其活跃的开发者生态、强劲的商业背书，以及在 Redpanda/Dapr/Trivy 等顶流项目中的严苛磨炼，使其成为 StarOcean 插件化体系未来进阶（混合 JS/Wasm 双引擎）的最优战略保障。

---

## 三、产品能力地图（诚实版，2026-09）

> 本节替代早期的“SAP 70% 映射”营销写法：只写代码里真实存在的东西
> （✅ 已做 / ❌ 未做）。不要再拿 SAP SD/MM/WM/FICO 对标。

```
StarOcean 能力现状
├── 销售 ──── 客户档案、销售订单 (Draft→Confirmed→Shipped→Invoiced)、确认扣库存 ✅
├── 采购 ──── 供应商档案、采购订单 (Draft→Confirmed→Received→Paid)、收货加库存 ✅
├── 库存 ──── 单仓实时库存、批次/效期、出入库流水、安全库存预警 ✅
├── 财务 ──── 收付款登记、应收应付统计、报销流、发票录入/作废、对账确认 ✅
├── 总账 ──── 科目/凭证/试算/三大报表、期间结账、单据自动过账 ✅
└── 人事考勤 ─ 员工档案、薪资、合同、部门、考勤、工作报告、分拣 ✅（基础可用）
```

### 3.1 分模块现状与边界

#### 1. 销售
* ✅ 客户主数据（联系人/电话/信用额度字段）。
* ✅ 信用额度控制：`customers.balance` 在确认时记应收、取消/回款时冲回；建单时检查（欠款+新单≤额度），超限拒绝并有测试覆盖。
* ✅ 订单全生命周期 + 取消冲销库存；仅草稿/已取消单可删（审计保护）。
* ❌ 销售退货流程、多联系人/标签画像、国际税区计税。

#### 2. 采购
* ✅ 供应商档案、采购订单全生命周期 + 收货加库存。
* ❌ 采购申请、采购退货、供应商评级打分、发票三单匹配（发票模块为独立财务录入）。

#### 3. 财务与总账
* ✅ 应收应付统计、收付款登记、报销（申请→审批→付款）、发票录入/作废、对账确认。
* ✅ 简易总账：科目、凭证（借贷平衡校验）、过账、试算、资产负债表/利润表/现金流量表、期间结账、业务单据自动过账（同事务）。
* ❌ 成本中心、多账套、多币种汇兑、集团合并报表。

#### 4. 生产制造
* ❌ 未开始：无 BOM、无工作中心、无工单、无 MRP。
  上 PP 会重写事务模型，在现有状态机说清楚之前不启动。

#### 5. 库存
* ✅ 单仓实时库存、批次/效期、出入库流水、安全库存预警。
* ❌ 多仓库/库位、库存盘点（动碰/期末盘盈亏）、拣货路径规划。`warehouse_id` 字段已预留，逻辑恒为单仓。

---

## 四、技术架构与代码组织

### 4.1 代码目录结构 (现状，2026-09)

```
starocean/
├── main.go                 # 入口：serve / migrate / seed（单二进制）
├── internal/
│   ├── server/             # Gin 路由：/api JSON + HTML 页面注册 + 静态资源
│   ├── web/                # 服务端渲染处理器（表单/列表/详情，复用业务函数）
│   ├── hooks/              # 事件注册表（扩展约定的唯一载体，见 docs/PLUGINS.md）
│   ├── models/             # 手写强类型模型 + 状态机(domain.go)
│   ├── db/                 # 连接/迁移/方言翻译（postgres.go/sqlite*.go/migrations）
│   ├── shared/             # 通用工具（错误/金额/分页/全局搜索）
│   ├── middleware/         # 会话/认证/登录限流
│   ├── print/              # 打印页 handler（模板在 view/print）
│   │   /* 核心业务模块目录，高度自治 */
│   ├── customers/ suppliers/ products/   # 主数据
│   ├── orders/             # 销售/采购单据业务逻辑（状态机+库存+信用）
│   ├── inventory/          # 库存流水
│   ├── finance/            # 收付/报销/发票/对账
│   ├── ledger/             # 科目/凭证/账簿/结账/自动过账
│   └── personnel/ attendance/ workreports/ partnernotes/ picking/
├── view/
│   ├── layout/             # Base 主布局 + 打印布局
│   ├── component/          # 表格/表单/分页/徽标（htmx 片段复用）
│   ├── pages/              # 各模块页面模板
│   ├── vmodel/             # 模板与 handler 共享视图模型（防 import cycle）
│   └── print/              # 6 类打印模板
├── public/
│   ├── css/                # Tailwind 4（@source 扫描 view/，产物 embed）
│   └── js/                 # htmx 2.x（vendored，无构建）
└── docs/PLUGINS.md         # 扩展约定（事件表/扩展字段/外部集成优先）
```

### 4.2 典型扩展交互生命周期 (以“建单前拦截”为例)

```
用户点击"确认销售订单"
        │
        ▼
Gin 接收到 POST /sales/:id/confirm (internal/web)
        │
        ▼
开启事务 → orders.ConfirmSalesOrder（行锁 + 状态校验 + 扣库存）
        │
        ▼
【触发前置 Hook】shared.FireBefore(ctx, "sales_order.confirming", {order_id})
        │
        ▼
┌──────────────────────────────────────────────────────────────┐
│ hooks.Default: 同进程 Go handler（随版本发布，非动态脚本）    │
│ 例: VIP 客户自动折扣 / 黑名单拦截                             │
│ 返回 {Aborted:true, Reason} 即拒绝 → 事务回滚 → 详情页展示原因 │
└──────────────────────────────┬───────────────────────────────┘
                                │ 通过
                                ▼
ledger.OnBusinessEvent（同事务自动过账）→ tx.Commit
        │
        ▼
【触发后置 Hook】shared.FireAfter("sales_order.confirmed")（异步，panic 隔离）
        │
        ▼
303 跳转回详情页（htmx 请求则回 HX-Redirect）
```

### 4.3 数据库物理扩展设计示意 (properties 列)

```sql
-- 以订单表为例，内置 properties 存放行业扩展属性（PG 为 JSONB，SQLite 为 TEXT）
CREATE TABLE sales_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_no VARCHAR(20) NOT NULL UNIQUE,
    customer_id UUID REFERENCES customers(id),
    status VARCHAR(20) DEFAULT 'draft',
    total_amount DECIMAL(15,2) DEFAULT 0,

    -- 核心通用字段定义在物理列中...

    -- 核心扩展列（PG: JSONB；SQLite: TEXT + JSON1 函数）
    properties JSONB DEFAULT '{}',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- PG 侧按需建 GIN 索引（SQLite 侧走 LIKE/JSON1，见迁移文件）
CREATE INDEX idx_sales_orders_properties ON sales_orders USING gin (properties);
```

---

## 五、竞争定位与优势

### 5.1 四象限分析

```
                     功能丰富度 ▲
                                │
                                │   ● SAP S/4HANA (功能全面、重型、极昂贵)
                                │
       ERPNext ●                │
       Odoo ●                   │
                                │   ● SAP Business One (中型、实施长、成本高)
                                │
────────────────────────────────┼────────────────────────────────► 架构轻量化
                                │  
                                │   ● **StarOcean** (进销存+往来+简易总账、单二进制、事件约定扩展)
                                │
       iota-sdk ●               │
                                │
                                ▼
```

### 5.2 核心差异化特征表格

| 指标 | 传统重型 ERP (SAP S/4) | 现代开源 ERP (ERPNext) | StarOcean (轻量级 70% SAP) |
|---|---|---|---|
| **学习与上手成本** | 极高，需要数月培训 | 中等，需要学习其特有框架 | **极低，纯正 htmx 交互，界面直观** |
| **典型部署耗时** | 3 - 6 个月 | 1 - 2 个小时 | **10 秒 (运行单二进制文件)** |
| **软硬件总拥有成本** | 数十万元至数百万元 | 维护服务器及运维成本 | **仅需极低内存的单台服务器数据库** |
| **行业特异性支持** | 庞大的行业包（臃肿） | 自定义 DocType（易导致升级灾难）| **事件约定 + 扩展字段（无脚本引擎，安全升级）** |

---

## 六、项目实施风险与对策

### 6.1 扩展约定的版本兼容性
* **风险描述**：Hook Payload 键名一旦变更，随版本发布的 Go 扩展 handler 可能读取不到字段。
* **规避对策**：
  - Payload 键名只增不改，改名必须发版说明（见 `docs/PLUGINS.md` 约定一）。
  - 后置 Hook panic 隔离在 `hooks.Fire` 内，绝不影响主单据流程。

### 6.2 扩展字段的检索纪律
* **风险描述**：`properties` 无节制膨胀会导致查询走全表扫描。
* **规避对策**：
  - PG 侧对高频过滤键加 GIN 索引；SQLite 侧控制数据量，必要时转物理列（走正常迁移）。
  - 金额/库存/状态等核心字段永不进 `properties`。

### 6.3 财务核算的严密性不足
* **风险描述**：轻量化架构极易在简化财务账目时，由于逻辑不够严密，产生对不上账的问题。
* **规避对策**：
  - 核心交易流水（仓储异动、收付款明细）严格基于数据库强事务锁机制（PostgreSQL Read Committed / Serializable）。
  - 单据状态变更节点强校验：例如销售出库时，自动检查对应销售订单是否已被修改，确保业务链路的每一次转结在财务底层都是可稽核的。
