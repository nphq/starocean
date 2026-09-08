# StarOcean Implementation Plan

> ⚠️ **已归档**：早期实施计划，仅作历史参考，不再作为实施依据。
> 关键偏差：`cmd/` 拆分未采用（实为子命令+flag）；sqlc 未采用（手写 SQL）；
> SQLite 从"可选适配"变为**默认**；前端最终为 templ + htmx（中间曾用 SvelteKit SPA，2026-09 已移除）；
> Goja/Wasm 插件引擎、AI 助手均已删除（见 `docs/PLUGINS.md`）；Wazero/BOM/PP/MRP 未启动。
> **当前事实来源**：`docs/DESIGN.md §0 现状架构快照`。

---

**Goal（历史）:** Build a Go 1.26 + Gin + templ + HTMX 2.x ERP system for SMEs. Single binary, PostgreSQL backend (with optional SQLite support), Tailwind CSS 4, and dynamic sandboxed plugin architecture (Goja JS + Wazero Wasm).

---

## Phase 0: 项目骨架 (骨架 - M1)

### Task 1: 基础设施配置与多数据库底座 (Database & Config)
*   **Files:**
    *   Create: `internal/config/config.go`
    *   Create: `internal/db/postgres.go`
    *   Create: `internal/db/migrate.go`
    *   Create: `internal/db/migrations/001_init.up.sql`
*   **Target:** Setup configuration loading and double database driver compatibility hooks.

### Task 2: 命令行工具骨架 — `starocean serve`
*   **Files:**
    *   Create: `main.go`
    *   Create: `cmd/root.go`
    *   Create: `cmd/serve.go`
    *   Create: `cmd/migrate.go`
*   **Target:** Single binary CLI entry supporting seed and serve commands.

### Task 3: 现代化 Tailwind CSS 4 + HTMX 布局
*   **Files:**
    *   Create: `public/css/tailwind.css` (CSS-first configuration with @theme settings)
    *   Create: `public/js/htmx.min.js` (htmx vendor bundle)
    *   Create: `view/layout/base.templ` (Base layout with native dark mode toggling)

---

## Phase 1: 进销存核心业务闭环 (进销存 - M2)

### Task 4: 商品主数据 (Product CRUD)
*   **Files:** `internal/products/handler.go`, `view/inventory/product_list.templ`
*   **htmx flow:** Inline creation forms swapping rows on submission without full reload.

### Task 5: 客户与供应商管理 (SD/MM Base Data)
*   **Target:** Dynamic customer accounts, tax rules, payment terms and safety credit limits.

### Task 6: 销售订单生命周期管理 (Sales Orders)
*   **Workflow:** Draft ➔ Confirmed (triggers stock locking & reservation) ➔ Shipped ➔ Invoiced.

### Task 7: 采购订单管理 (Purchase Orders)
*   **Workflow:** Draft ➔ Confirmed ➔ Received (triggers inventory increment) ➔ Paid.

### Task 8: 库存流水追踪 (Stock Card)
*   **Target:** Log transactional inventory changes in `inventory_movements` wrapped in atomic PostgreSQL transactions.

---

## Phase 2: 财务往来账核销与预测 (财务 - M3)

### Task 9: 往来账明细分配表 (FICO Allocations)
*   **Files:**
    *   Create Migration: `internal/db/migrations/005_fico_allocations.up.sql`
    *   Create Service: `internal/finance/ledger.go` (Ageing analysis, allocate payments to invoices)

### Task 10: 多张发票多对多分配交互 (Multi-Invoice Payment Registration)
*   **Files:**
    *   Modify Handler: `internal/finance/handler.go`
    *   Create View: `view/finance/payment_allocation.templ` (Alpine.js-powered payment splitting UI)

### Task 11: 现金流预测看板与企业损益表 (Cashflow & Income Statement)
*   **Files:** `internal/finance/handler.go`, `view/finance/dashboard.templ`

---

## Phase 2.5: 插件化扩展引擎与沙箱治理 (插件化 - M3.5)

### Task 12: Goja JS 引擎池化与脚本预缓存 (Goja AST Cache & Pool)
*   **Files:**
    *   Create Engine: `internal/plugin/pool.go`
    *   Modify Loader: `internal/plugin/loader.go`
*   **Target:** Pre-compile dynamic JS scripts and pool VM runtimes via `sync.Pool` to avoid memory allocations in hot-paths.

### Task 13: 插件沙箱执行强熔断与隔离 (Sandbox Limits)
*   **Files:** `internal/plugin/sandbox.go`
*   **Target:** Enforce 50ms hard execution timeouts using context and channel interruptions to safeguard the main process.

### Task 14: UI Slots 动态注入组件化 (UI Hooks)
*   **Files:** `view/layout/base.templ`, `internal/plugin/ui.go`
*   **Target:** Declare UI hooks that dynamically render and swap layouts utilizing HTMX.

---

## Phase 3: PP 生产制造核心功能闭环 (生产 - M4)

### Task 15: 多级 BOM (物料清单) 树形结构
*   **Files:** `internal/products/bom.go`, `view/products/bom_form.templ`

### Task 16: 生产工单管理与自动配料发料 (Work Order & Backflushing)
*   **Files:** `internal/production/service.go`, `view/production/work_order.templ`
*   **Target:** Material check, manual issuance, and automatic stock deduction (backflushing) on completion.

### Task 17: ROP (再订货点) 智能采购建议 (ROP Engine)
*   **Files:** `internal/inventory/rop.go`, `view/inventory/rop_dashboard.templ`

---

## Phase 3.5: Wazero Wasm 插件虚拟机宿主 (高阶插件化 - M4.5)

### Task 18: Wazero 运行时集成 (Wasm Host)
*   **Files:** `internal/plugin/wasm.go`
*   **Target:** Dynamically load pre-compiled `.wasm` plugin rules in memory without CGO compilation dependencies.

---

## Phase 4: 极简分发部署与打印流支持 (可分发 - M5)

### Task 19: 响应式 Web 打印模板与 PDF 导出 (Print Layout)
*   **Files:** `view/layout/print.templ`
*   **Target:** Clean media styles for borderless receipt and invoice browser printouts.

### Task 20: 零依赖 SQLite 存储驱动可选适配 (CGO-free SQLite)
*   **Files:** `internal/db/sqlite.go`, `main.go`
*   **Target:** Support switches to SQLite for single-file portable offline operations utilizing CGO-free `modernc.org/sqlite`.

---

## 里程碑

| 里程碑 | 目标 | 验证场景 |
|---|---|---|
| **M1: 骨架** | Task 1-3 | `./starocean serve` ➔ 启动单二进制并渲染空仪表盘 |
| **M2: 进销存** | Task 4-8 | 销售/采购单据流转，库存异动流水实时写入，库存预警启动 |
| **M3: 财务核销** | Task 9-11 | 收付款多对多分配，生成欠款账龄分析及企业现金流/损益看板 |
| **M3.5: 插件引擎** | Task 12-14 | 脚本预缓存并实现 50ms 超时自动熔断，支持基于 HTMX 的 UI 插槽 |
| **M4: 生产制造** | Task 15-17 | 多级 BOM 树解析，生产工单下达与自动领料，ROP 智能采购建议 |
| **M4.5: Wasm 宿主** | Task 18 | 引入 Wazero，支持运行高性能沙箱 Wasm 插件组件 |
| **M5: 极致分发** | Task 19-20 | 适配 @media print 精美页面打印，支持 SQLite 零配置离线单机运行 |
