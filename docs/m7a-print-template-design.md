# M7a 打印模板 — 完整设计与实施方案

## Context

StarOcean 目前没有任何打印功能。销售订单、采购订单、发票、对账单、报销单等单据只能在屏幕上查看，无法生成专业的打印输出。

**目标：** 为 6 类单据提供浏览器原生打印支持，通过专用打印布局 + `@media print` CSS 实现，零新依赖。

---

## 需求清单

| # | 单据类型 | 用途 | 数据源 Model |
|---|---|---|---|
| 1 | 销售订单 / 送货单 | 客户确认、仓库拣货、随货同行 | `SalesOrder` + `SalesOrderItem` |
| 2 | 采购订单 | 供应商确认、到货核对 | `PurchaseOrder` + `PurchaseOrderItem` |
| 3 | 发票 | 税务凭证、客户归档 | `Invoice` |
| 4 | 对账单 | 往来账款确认 | `Reconciliation` + `ReconciliationItem` |
| 5 | 报销单 | 内部审批留档 | `Reimbursement` + `ReimbursementItem` |
| 6 | 客户对账单 | 客户欠款/余额汇总 | `Customer` + 聚合查询 |

---

## 文件变更清单

### 新建文件（8 个）

| 文件 | 说明 |
|---|---|
| `view/layout/print.templ` | 打印专用布局模板 |
| `view/print/sales_order.templ` | 销售订单打印 |
| `view/print/purchase_order.templ` | 采购订单打印 |
| `view/print/invoice.templ` | 发票打印 |
| `view/print/reconciliation.templ` | 对账单打印 |
| `view/print/reimbursement.templ` | 报销单打印 |
| `view/print/customer_statement.templ` | 客户对账单打印 |
| `internal/print/handler.go` | 打印页面 handler |

### 修改文件（7 个）

| 文件 | 变更内容 |
|---|---|
| `internal/server/router.go` | 添加 6 条打印路由 + import |
| `internal/config/config.go` | 添加 `CompanyName` 配置项 |
| `view/sales/sales_detail.templ` | 操作栏添加"打印"按钮 |
| `view/purchase/purchase_detail.templ` | 操作栏添加"打印"按钮 |
| `view/finance/invoice_detail.templ` | 操作栏添加"打印"按钮 |
| `view/finance/reconciliation_detail.templ` | 操作栏添加"打印"按钮 |
| `view/finance/reimbursement_detail.templ` | 操作栏添加"打印"按钮 |

---

## 详细设计

### 1. 打印布局 `view/layout/print.templ`

独立的打印布局，不继承 `Base`，无侧边栏、无导航、无 Alpine.js。

**结构：**
- `@media screen`：白底 A4 纸张预览（210mm 宽，居中，带阴影）
- `@media print`：隐藏操作按钮，`@page { size: A4; margin: 0 }`
- `no-print` CSS 类：屏幕可见，打印时 `display: none`

**参数：** `title string, companyName string`

**组成：**
1. 操作按钮栏（打印/关闭，`no-print`）
2. 页眉：公司名称 + 打印时间（JS 填充）
3. `{ children... }` 内容区
4. 页脚：固定在底部，公司名 + 页码

**打印样式组件（内联 style，避免依赖 Tailwind）：**

| 类名 | 用途 |
|---|---|
| `.print-header` | 页眉：公司名 + 打印时间，底部分割线 |
| `.print-title` | 单据标题，居中大字，letter-spacing |
| `.print-info` | 两列 grid 信息区（编号、日期、客户名等） |
| `.print-table` | 表格：1px 实线边框，th 灰底 |
| `.print-total` | 合计行：右对齐，大字加粗 |
| `.print-notes` | 备注：灰底圆角框 |
| `.print-signature` | 签章区：三列 grid（制单/审核/收货） |

### 2. 打印 Handler `internal/print/handler.go`

**Handler 结构：**

```go
type Handler struct {
    db          *sql.DB
    companyName string
}

func New(db *sql.DB, companyName string) *Handler
```

**6 个方法，每个方法复用现有数据查询 SQL：**

#### 2.1 SalesOrderPrint（复用 `orders/order_business.go:131` 的 `getSalesOrder` 查询）

```go
func (h *Handler) SalesOrderPrint(c *gin.Context)
```

数据查询（直接在 handler 中写，不引用 order_business 的私有函数）：
- 查 `sales_orders` JOIN `customers` 获取订单 + 客户名
- 查 `sales_order_items` JOIN `products` 获取明细
- 调用 `print.SalesOrderPrint(order, items, h.companyName).Render(ctx, c.Writer)`

SQL 参考（来自 `handler.go:169-176`）：
```sql
-- 订单头
SELECT id, order_no, COALESCE(customer_id, gen_random_uuid()),
       COALESCE(status, 'draft'), COALESCE(total_amount, 0), COALESCE(paid_amount, 0),
       COALESCE(order_date, '1970-01-01'), delivery_date,
       COALESCE(notes, ''), COALESCE(created_at, '1970-01-01'),
       COALESCE(company_id, 'default'), COALESCE(properties::text, '{}')
FROM sales_orders WHERE id = $1

-- 明细行
SELECT soi.id, COALESCE(soi.order_id, gen_random_uuid()),
       COALESCE(soi.product_id, gen_random_uuid()),
       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
       soi.quantity, COALESCE(soi.unit_price, 0), COALESCE(soi.amount, 0)
FROM sales_order_items soi LEFT JOIN products p ON soi.product_id = p.id
WHERE soi.order_id = $1
```

需额外查询客户联系人/电话（用于送货单）：
```sql
SELECT COALESCE(contact_person,''), COALESCE(phone,''), COALESCE(address,'')
FROM customers WHERE id = $1
```

#### 2.2 PurchaseOrderPrint（对称于 SalesOrderPrint）

```go
func (h *Handler) PurchaseOrderPrint(c *gin.Context)
```

SQL 参考（来自 `order_business.go:320-375`），查询 `purchase_orders` + `purchase_order_items`。
供应商联系人信息从 `suppliers` 表获取。

#### 2.3 InvoicePrint（复用 `finance/enhancement.go:298-315` 的查询）

```go
func (h *Handler) InvoicePrint(c *gin.Context)
```

SQL：
```sql
SELECT id, invoice_no, type, partner_type, partner_id, COALESCE(partner_name,''),
       amount, tax_rate, tax_amount, total_amount,
       invoice_date, COALESCE(invoice_code,''), invoice_status,
       COALESCE(reference_type,''), reference_id,
       COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
FROM invoices WHERE id = $1
```

#### 2.4 ReconciliationPrint（复用 `finance/enhancement.go:389-407`）

```go
func (h *Handler) ReconciliationPrint(c *gin.Context)
```

SQL — 对账单头 + 明细（`getReconciliationItems` 来自 `enhancement.go:599`）：
```sql
-- 头
SELECT ... FROM reconciliations WHERE id = $1
-- 订单明细
SELECT id, reconciliation_id, item_type, reference_no, amount, reference_date
FROM reconciliation_items WHERE reconciliation_id = $1 AND item_type = 'order'
-- 付款明细
SELECT ... WHERE reconciliation_id = $1 AND item_type = 'payment'
```

#### 2.5 ReimbursementPrint（复用 `finance/enhancement.go:494-519`）

```go
func (h *Handler) ReimbursementPrint(c *gin.Context)
```

SQL 参考 `getReimbursement`，查 `reimbursements` + `reimbursement_items`。

#### 2.6 CustomerStatement（新增查询）

```go
func (h *Handler) CustomerStatement(c *gin.Context)
```

需新建数据结构：
```go
type CustomerStatementData struct {
    Customer   models.Customer
    Orders     []models.SalesOrder
    Payments   []models.Payment
    TotalDue   decimal.Decimal
    TotalPaid  decimal.Decimal
    Balance    decimal.Decimal
}
```

SQL（聚合查询）：
```sql
-- 客户信息
SELECT id, code, name, contact_person, phone, email, address, credit_limit, balance
FROM customers WHERE id = $1

-- 未清销售订单
SELECT id, order_no, order_date, total_amount, paid_amount, status
FROM sales_orders WHERE customer_id = $1 AND status NOT IN ('cancelled','draft')
ORDER BY order_date

-- 关联付款
SELECT id, type, amount, partner_name, notes, payment_date, created_at
FROM payments WHERE partner_name = (SELECT name FROM customers WHERE id = $1)
ORDER BY payment_date
```

### 3. 打印模板设计

#### 3.1 销售订单 `view/print/sales_order.templ`

**参数：** `order models.SalesOrder, items []models.SalesOrderItem, customer models.Customer, companyName string`

**布局：**
```
┌─ 公司名称 ───────────────────────── 打印时间 ─┐
│                                                  │
│              销 售 订 单                          │
│                                                  │
│  单据编号: SO-xxx     订单日期: 2026-05-30       │
│  客    户: XX公司     交货日期: 2026-06-05       │
│  联 系 人: 张三       联系电话: 138xxxx          │
│  地    址: XXXXXX                                │
│                                                  │
│  序号 │ 商品名称  │ 数量 │ 单价   │ 金额         │
│  ─────┼──────────┼──────┼────────┼───────       │
│   1   │ 商品A     │  10  │ 100.00 │ 1000.00      │
│   2   │ 商品B     │   5  │ 200.00 │ 1000.00      │
│                                                  │
│                              合计: ¥2,000.00     │
│                                                  │
│  备注: ________________                           │
│                                                  │
│  制单:_______  审核:_______  收货:_______        │
└──────────────────────────────────────────────────┘
```

**签章区：** 三列 grid（制单/审核/收货），每列上方留空白手签区，下方画线+标签。

#### 3.2 采购订单 `view/print/purchase_order.templ`

与销售订单对称，差异：
- 标题：`采 购 订 单`
- 信息区：供应商名/联系人/电话/地址
- 签章区：采购/审核/验收

#### 3.3 发票 `view/print/invoice.templ`

**无签章区**。突出税额信息：

```
│  发票号码: FP-xxx     发票代码: xxx              │
│  发票类型: 销项       往来方: XX公司             │
│  开票日期: 2026-05-30                             │
│                                                  │
│  不含税金额: ¥1,000.00                           │
│  税    率: 13%                                    │
│  税    额: ¥130.00                                │
│  价税合计: ¥1,130.00                             │
```

#### 3.4 对账单 `view/print/reconciliation.templ`

**双表格结构：**

```
│  对账编号: DZ-xxx     往来方: XX公司             │
│  对账期间: 2026-05-01 ~ 2026-05-31               │
│                                                  │
│  ── 订单明细 ──                                  │
│  单据编号    │ 日期       │ 金额                  │
│  SO-001     │ 05-10      │ 5,000.00              │
│  SO-002     │ 05-15      │ 3,000.00              │
│  小计: ¥8,000.00                                 │
│                                                  │
│  ── 收款明细 ──                                  │
│  备注        │ 日期       │ 金额                  │
│  5月回款     │ 05-20      │ 6,000.00              │
│  小计: ¥6,000.00                                 │
│                                                  │
│  差异金额: ¥2,000.00                             │
│                                                  │
│  确认方签章:_________  我方签章:_________        │
```

签章区两列：确认方 / 我方。

#### 3.5 报销单 `view/print/reimbursement.templ`

```
│  报销编号: BX-xxx     申请人: 张三              │
│  部    门: 技术部     报销类别: 差旅             │
│  费用日期: 2026-05-28                             │
│                                                  │
│  类别    │ 金额      │ 说明                      │
│  交通    │ 200.00    │ 出租车                    │
│  餐饮    │ 150.00    │ 客户招待                  │
│                                                  │
│  合计: ¥350.00                                    │
│                                                  │
│  审批人: 李四         审批时间: 2026-05-29       │
│                                                  │
│  申请人:_________  部门主管:_________  财务:____ │
```

#### 3.6 客户对账单 `view/print/customer_statement.templ`

```
│  客户对账单                                       │
│  客    户: XX公司      编码: C001                │
│  联 系 人: 张三        电 话: 138xxxx            │
│                                                  │
│  ── 未清订单 ──                                  │
│  单据编号   │ 日期       │ 应收金额  │ 已收金额   │
│  SO-001    │ 05-10      │ 5000.00  │ 3000.00    │
│  SO-002    │ 05-15      │ 3000.00  │ 0.00       │
│                                                  │
│  ── 收款记录 ──                                  │
│  日期       │ 金额      │ 备注                    │
│  05-20     │ 6000.00   │ 银行转账                │
│                                                  │
│  应收合计: ¥8,000.00                              │
│  已收合计: ¥6,000.00                              │
│  未收余额: ¥2,000.00                              │
```

### 4. 详情页打印按钮集成

在 5 个详情页的操作区域添加打印按钮。按钮样式统一：

```templ
<a href={ templ.SafeURL(fmt.Sprintf("/sales/%s/print", order.ID)) }
   target="_blank"
   class="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium
          border border-warm-200 rounded-lg text-warm-600
          hover:bg-warm-50 transition-colors">
    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" d="M6.72 13.829c-.24.03-.48.062-.72.096m.72-.096a42.415 42.415 0 0 1 10.56 0m-10.56 0L6.34 18m10.94-4.171c.24.03.48.062.72.096m-.72-.096L17.66 18m0 0 .229 2.523a1.125 1.125 0 0 1-1.12 1.227H7.231c-.662 0-1.18-.568-1.12-1.227L6.34 18m11.318 0h1.091A2.25 2.25 0 0 0 21 15.75V9.456c0-1.081-.768-2.015-1.837-2.175a48.055 48.055 0 0 0-1.913-.247M6.34 18H5.25A2.25 2.25 0 0 1 3 15.75V9.456c0-1.081.768-2.015 1.837-2.175a48.041 48.041 0 0 1 1.913-.247m0 0a48.159 48.159 0 0 1 10.5 0m-10.5 0V5.625c0-.621.504-1.125 1.125-1.125h8.25c.621 0 1.125.504 1.125 1.125v2.034"/>
    </svg>
    打印
</a>
```

**放置位置：** 各详情页的操作栏（`flex flex-wrap gap-3` 区域）内，在现有操作按钮之前。

涉及文件及行号：
- `view/sales/sales_detail.templ:77` — `<div class="flex flex-wrap gap-3">` 内部
- `view/purchase/purchase_detail.templ` — 同上位置
- `view/finance/invoice_detail.templ` — 操作栏内
- `view/finance/reconciliation_detail.templ` — 操作栏内
- `view/finance/reimbursement_detail.templ` — 操作栏内

### 5. 路由注册 `internal/server/router.go`

在 `RegisterRoutes` 函数中，`auth` 组内添加：

```go
import "github.com/starocean/starocean/internal/print"

// 在 handler 初始化区域添加：
printH := print.New(db, cfg.CompanyName)  // 需传入 config

// 在 auth 组内添加路由：
auth.GET("/sales/:id/print", printH.SalesOrderPrint)
auth.GET("/purchases/:id/print", printH.PurchaseOrderPrint)
auth.GET("/finance/invoices/:id/print", printH.InvoicePrint)
auth.GET("/finance/reconciliations/:id/print", printH.ReconciliationPrint)
auth.GET("/finance/reimbursements/:id/print", printH.ReimbursementPrint)
auth.GET("/customers/:id/statement/print", printH.CustomerStatement)
```

**注意：** `RegisterRoutes` 当前签名是 `(r *gin.Engine, db *sql.DB, publicFS embed.FS)`，需要增加 `cfg *config.Config` 参数，或者直接从环境变量读取公司名。最简方案是在 `RegisterRoutes` 中加 `companyName string` 参数。

### 6. 配置项 `internal/config/config.go`

添加 `CompanyName` 字段：

```go
type Config struct {
    Port        string
    DatabaseURL string
    SecretKey   string
    Debug       bool
    CompanyName string  // 新增
}

// Load() 中添加：
CompanyName: getEnv("COMPANY_NAME", "StarOcean"),
```

---

## 实施步骤（按顺序）

| 步骤 | 内容 | 文件 |
|---|---|---|
| 1 | Config 添加 CompanyName | `internal/config/config.go` |
| 2 | 创建打印布局模板 | `view/layout/print.templ` |
| 3 | 创建打印 handler | `internal/print/handler.go` |
| 4 | 创建销售订单打印模板 | `view/print/sales_order.templ` |
| 5 | 创建采购订单打印模板 | `view/print/purchase_order.templ` |
| 6 | 创建发票打印模板 | `view/print/invoice.templ` |
| 7 | 创建对账单打印模板 | `view/print/reconciliation.templ` |
| 8 | 创建报销单打印模板 | `view/print/reimbursement.templ` |
| 9 | 创建客户对账单打印模板 | `view/print/customer_statement.templ` |
| 10 | 注册路由 + 修改 RegisterRoutes 签名 | `internal/server/router.go` |
| 11 | 5 个详情页添加打印按钮 | 5 个 detail.templ |
| 12 | `templ generate` + `go build` 验证 | 命令行 |

---

## 验证

1. `make build` 编译通过
2. `./starocean serve` 启动，访问销售订单详情页
3. 点击"打印"按钮，新窗口打开 A4 预览
4. 确认：公司页眉、单据标题、信息区、明细表格、合计、签章区均正确
5. `Cmd+P` 触发浏览器打印，确认操作按钮已隐藏
6. 逐个验证 6 类单据打印输出
