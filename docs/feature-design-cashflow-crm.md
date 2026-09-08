# 功能设计文档：现金流预测 & 轻量 CRM 跟进备注

> 版本: 1.0 | 日期: 2026-05-24 | 状态: 待评审

---

## 目录

1. [背景与目标](#1-背景与目标)
2. [功能一：现金流预测增强](#2-功能一现金流预测增强)
3. [功能二：客户/供应商跟进备注](#3-功能二客户供应商跟进备注)
4. [文件变更清单](#4-文件变更清单)
5. [开发计划与优先级](#5-开发计划与优先级)

---

## 1. 背景与目标

### 现状

当前 StarOcean 已具备：
- **仪表盘**：本月销售额、待处理订单、库存预警、活跃客户、12个月趋势图
- **财务页面**（`/finance`）：应收/应付余额、本月收入/支出/净现金流（仅数字卡片）
- **客户/供应商模块**：完整 CRUD，详情页仅展示静态字段，无互动记录

### 痛点

| 痛点 | 影响 |
|------|------|
| 现金流只有当月快照，无法看到趋势 | 管理者无法预判资金缺口 |
| 无应收账龄分布，不知道哪些钱拖得久 | 坏账风险不可见 |
| 无现金流预测能力 | 资金规划凭感觉 |
| 客户/供应商无跟进记录 | 沟通历史丢失，协作断层 |

### 目标

1. 将现金流从「当月快照」升级为「6个月回溯 + 3个月预测」的完整视图
2. 为客户/供应商增加轻量跟进备注系统，形成沟通时间线

---

## 2. 功能一：现金流预测增强

### 2.1 功能概览

在 Dashboard 和 Finance 两个页面增强现金流展示能力：

- **Dashboard**：新增「现金流趋势」图表卡片（6个月实际 + 3个月预测）
- **Finance**：现金流区域升级为月度趋势迷你图 + 应收账龄分布

### 2.2 数据源分析

现有数据完全满足需求，无需新建表：

```
payments 表        → 实际现金流入/流出（按 payment_date）
sales_orders 表    → 应收未收 = total_amount - paid_amount（按 order_date）
purchase_orders 表 → 应付未付 = total_amount - paid_amount（按 order_date）
```

### 2.3 预测算法

采用**简单移动平均 + 未完成订单修正**，不引入复杂模型：

**收入预测（未来月份 M）：**
```
predicted_income_M =
  AVG(前3个月实际收入) × 0.6
  + 未完成销售订单中 delivery_date 落在 M 的 (total_amount - paid_amount) 合计 × 0.4
```

- 权重 0.6 给历史均值（稳健），0.4 给合同确定性（乐观调整）
- 新建企业无历史数据时，纯靠未完成订单推算（权重自动退化为 1.0）

**支出预测（未来月份 M）：**
```
predicted_expense_M =
  AVG(前3个月实际支出) × 0.6
  + 未完成采购订单中 delivery_date 落在 M 的 (total_amount - paid_amount) 合计 × 0.4
```

**净现金流预测：**
```
predicted_net_M = predicted_income_M - predicted_expense_M
```

### 2.4 API 设计

#### `GET /api/finance/cashflow/trend`

返回近6个月实际 + 未来3个月预测的现金流数据。

**请求参数：** 无

**响应结构：**
```json
{
  "months": [
    {
      "label": "2026-01",
      "type": "actual",
      "income": 125000.00,
      "expense": 87000.00,
      "net": 38000.00
    },
    {
      "label": "2026-02",
      "type": "actual",
      "income": 110000.00,
      "expense": 92000.00,
      "net": 18000.00
    }
  ],
  "summary": {
    "total_receivable": 250000.00,
    "total_payable": 180000.00,
    "aging": {
      "0_30": { "receivable": 150000.00, "payable": 100000.00 },
      "31_60": { "receivable": 60000.00, "payable": 50000.00 },
      "61_90": { "receivable": 25000.00, "payable": 20000.00 },
      "90_plus": { "receivable": 15000.00, "payable": 10000.00 }
    }
  }
}
```

**`type` 字段说明：**
- `"actual"` — 历史实际值，来自 payments 表
- `"forecast"` — 预测值，来自移动平均 + 订单推算

#### `GET /api/finance/receivable/aging`

应收账龄分布详情（可选独立接口，也可合并到上面）。

**响应结构：**
```json
{
  "aging": [
    { "range": "0-30天", "count": 12, "amount": 150000.00 },
    { "range": "31-60天", "count": 5, "amount": 60000.00 },
    { "range": "61-90天", "count": 2, "amount": 25000.00 },
    { "range": "90天以上", "count": 1, "amount": 15000.00 }
  ]
}
```

### 2.5 SQL 查询设计

#### 实际现金流（近6个月）

```sql
SELECT
    TO_CHAR(d.month, 'YYYY-MM') AS label,
    COALESCE(income.total, 0) AS income,
    COALESCE(expense.total, 0) AS expense
FROM generate_series(
    DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '5 months',
    DATE_TRUNC('month', CURRENT_DATE),
    '1 month'
) AS d(month)
LEFT JOIN LATERAL (
    SELECT SUM(CAST(amount AS numeric)) AS total
    FROM payments
    WHERE type = '收入'
      AND payment_date >= d.month
      AND payment_date < d.month + INTERVAL '1 month'
) income ON true
LEFT JOIN LATERAL (
    SELECT SUM(CAST(total_amount AS numeric)) AS total
    FROM purchase_orders
    WHERE status != 'cancelled'
      AND order_date >= d.month
      AND order_date < d.month + INTERVAL '1 month'
) expense ON true
ORDER BY d.month;
```

#### 预测现金流（未来3个月）

```sql
-- 历史均值部分
WITH hist AS (
    SELECT
        AVG(income) AS avg_income,
        AVG(expense) AS avg_expense
    FROM (
        SELECT
            COALESCE((SELECT SUM(CAST(amount AS numeric)) FROM payments
                      WHERE type = '收入' AND payment_date >= DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '5 months' + (n || ' month')::interval
                      AND payment_date < DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '4 months' + (n || ' month')::interval), 0) AS income,
            COALESCE((SELECT SUM(CAST(total_amount AS numeric)) FROM purchase_orders
                      WHERE status != 'cancelled'
                      AND order_date >= DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '5 months' + (n || ' month')::interval
                      AND order_date < DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '4 months' + (n || ' month')::interval), 0) AS expense
        FROM generate_series(0, 2) AS n
    ) h
),
-- 未完成订单预期部分
future AS (
    SELECT
        TO_CHAR(d.month, 'YYYY-MM') AS label,
        COALESCE(so.expected, 0) AS sales_expected,
        COALESCE(po.expected, 0) AS purchase_expected
    FROM generate_series(
        DATE_TRUNC('month', CURRENT_DATE) + INTERVAL '1 month',
        DATE_TRUNC('month', CURRENT_DATE) + INTERVAL '3 months',
        '1 month'
    ) AS d(month)
    LEFT JOIN LATERAL (
        SELECT SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)) AS expected
        FROM sales_orders
        WHERE status NOT IN ('cancelled', 'invoiced')
          AND delivery_date >= d.month
          AND delivery_date < d.month + INTERVAL '1 month'
    ) so ON true
    LEFT JOIN LATERAL (
        SELECT SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)) AS expected
        FROM purchase_orders
        WHERE status NOT IN ('cancelled', 'received')
          AND delivery_date >= d.month
          AND delivery_date < d.month + INTERVAL '1 month'
    ) po ON true
)
SELECT
    f.label,
    (h.avg_income * 0.6 + f.sales_expected * 0.4) AS predicted_income,
    (h.avg_expense * 0.6 + f.purchase_expected * 0.4) AS predicted_expense
FROM future f
CROSS JOIN hist h
ORDER BY f.label;
```

#### 应收账龄分布

```sql
SELECT
    CASE
        WHEN age_days BETWEEN 0 AND 30 THEN '0-30天'
        WHEN age_days BETWEEN 31 AND 60 THEN '31-60天'
        WHEN age_days BETWEEN 61 AND 90 THEN '61-90天'
        ELSE '90天以上'
    END AS age_range,
    COUNT(*) AS count,
    SUM(CAST(total_amount AS numeric) - COALESCE(paid_amount, 0)) AS amount
FROM (
    SELECT
        total_amount,
        paid_amount,
        CURRENT_DATE - COALESCE(order_date, CURRENT_DATE) AS age_days
    FROM sales_orders
    WHERE status NOT IN ('cancelled')
      AND CAST(total_amount AS numeric) - COALESCE(paid_amount, 0) > 0
) unpaid
GROUP BY age_range
ORDER BY MIN(age_days);
```

### 2.6 前端设计

#### Dashboard 新增卡片

位置：在现有「近12个月销售与采购趋势」图表下方，作为新卡片。

```
┌─────────────────────────────────────────────┐
│  现金流趋势 (近6月实际 + 3月预测)           │
│                                             │
│  ¥120K ┤  ┌─┐                              │
│  ¥100K ┤  │ │   ┌─┐                         │
│   ¥80K ┤──┘ └───┘ └──┐  ▓▓ ← 预测区间     │
│   ¥60K ┤              └────▓▓▓              │
│   ¥40K ┤                                     │
│   ¥20K ┤                                     │
│     ¥0 ┼──┬──┬──┬──┬──┬──┬──┬──┬──          │
│         01 02 03 04 05 06 07 08 09           │
│  ── 实际  ── 预测                            │
└─────────────────────────────────────────────┘
```

- 实际值：实线
- 预测值：虚线 + 半透明填充
- X 轴：月份标签
- Y 轴：金额
- Tooltip：显示收入、支出、净额

#### Finance 页面现金流区域升级

将现有的3个数字卡片替换为：

```
┌──────────────────────────────────────────────────┐
│  现金流概览                                       │
│                                                   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐          │
│  │ 本月收入  │ │ 本月支出  │ │ 净现金流  │          │
│  │ ¥125,000 │ │ ¥87,000  │ │ ¥38,000  │          │
│  │ ↑12.5%   │ │ ↓3.2%    │ │ ↑28.7%   │          │
│  └──────────┘ └──────────┘ └──────────┘          │
│                                                   │
│  ┌────────────────────────────────────────┐       │
│  │  月度现金流趋势 (迷你图)                │       │
│  │  紧凑版折线图，高80px                   │       │
│  └────────────────────────────────────────┘       │
│                                                   │
│  应收账龄分布                                     │
│  ┌────────────────────────────────────────┐       │
│  │  0-30天  ████████████████  ¥150,000    │       │
│  │  31-60天 ██████          ¥60,000      │       │
│  │  61-90天 ███             ¥25,000      │       │
│  │  90天+   ██              ¥15,000      │       │
│  └────────────────────────────────────────┘       │
└──────────────────────────────────────────────────┘
```

**环比变化计算：**
```
较上月变化 = (本月值 - 上月值) / NULLIF(上月值, 0) × 100%
```
正值为绿色 ↑，负值为红色 ↓。

### 2.7 后端改动

**`internal/finance/handler.go`：**

- 新增 `CashflowTrendAPI` handler
- 新增 `getCashflowTrend(ctx, db)` 函数：返回实际+预测数据
- 新增 `getReceivableAging(ctx, db)` 函数：返回账龄分布
- 新增 `getMonthOverMonth(ctx, db)` 函数：计算环比变化

**`view/dashboard/dashboard.templ`：**

- 新增现金流趋势图表卡片
- 复用 Chart.js，新增 `cashflowChart()` Alpine.js 组件
- 预测区间使用虚线 `borderDash: [5, 5]`

**`view/finance/finance.templ`：**

- 现金流区域从3个数字卡片升级为：数字卡片 + 迷你趋势图 + 账龄分布
- 数字卡片增加环比箭头

**`internal/server/router.go`：**

- 新增路由：`GET /api/finance/cashflow/trend`

---

## 3. 功能二：客户/供应商跟进备注

### 3.1 功能概览

为客户和供应商增加统一的跟进备注系统，形成沟通时间线，支持：
- 在客户/供应商详情页查看和添加跟进记录
- 按类型分类（电话/拜访/邮件/其他）
- 设定下次跟进日期，到期待提醒
- 仪表盘展示待跟进条目

### 3.2 数据库设计

#### 新建 `partner_notes` 表

```sql
CREATE TABLE partner_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_type VARCHAR(10) NOT NULL,       -- 'customer' | 'supplier'
    partner_id UUID NOT NULL,                -- 关联客户或供应商 ID
    note_type VARCHAR(20) NOT NULL DEFAULT '其他',
    content TEXT NOT NULL,
    next_follow_up DATE,                     -- 下次跟进日期（可选）
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_partner_notes_partner ON partner_notes(partner_type, partner_id);
CREATE INDEX idx_partner_notes_follow_up ON partner_notes(next_follow_up) WHERE next_follow_up IS NOT NULL;
CREATE INDEX idx_partner_notes_created ON partner_notes(created_at DESC);
CREATE INDEX idx_partner_notes_company ON partner_notes(company_id);
```

**字段说明：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | UUID | 自动 | 主键 |
| `partner_type` | VARCHAR(10) | 是 | 固定值 `customer` 或 `supplier` |
| `partner_id` | UUID | 是 | 指向 `customers.id` 或 `suppliers.id`，无外键约束（轻量设计） |
| `note_type` | VARCHAR(20) | 是 | 联系方式：`电话` / `拜访` / `邮件` / `微信` / `其他` |
| `content` | TEXT | 是 | 跟进内容，建议限制 2000 字符 |
| `next_follow_up` | DATE | 否 | 下次跟进日期，用于提醒 |
| `created_at` | TIMESTAMPTZ | 自动 | 创建时间 |
| `company_id` | VARCHAR(50) | 自动 | 多租户隔离 |

**设计决策：**

- 不设外键约束：partner_notes 作为轻量附加层，不与客户/供应商表强耦合
- 统一一张表而非分表：减少代码重复，查询模式一致
- `next_follow_up` 可为 NULL：不是每条备注都需要设定跟进

### 3.3 Model 定义

在 `internal/models/models.go` 新增：

```go
type PartnerNote struct {
    ID            uuid.UUID
    PartnerType   string        // "customer" | "supplier"
    PartnerID     uuid.UUID
    PartnerName   string        // 冗余显示用
    NoteType      string        // "电话" | "拜访" | "邮件" | "微信" | "其他"
    Content       string
    NextFollowUp  time.Time     // 零值表示未设定
    CreatedAt     time.Time
    CompanyID     string
}
```

### 3.4 API 设计

#### `POST /api/partner-notes`

创建跟进备注。

**请求体（form-data）：**
```
partner_type = "customer"
partner_id   = "uuid"
note_type    = "电话"
content      = "客户确认下周二下单，金额约5万"
next_follow_up = "2026-06-01"  (可选)
```

**响应：** htmx 片段（时间线新条目的 HTML），插入到详情页时间线顶部。

#### `GET /api/partner-notes/:partnerType/:partnerId`

获取某客户/供应商的全部跟进备注（按时间倒序）。

**响应：** htmx 片段（时间线列表 HTML），用于页面首次加载。

#### `DELETE /api/partner-notes/:id`

删除某条跟进备注。

**响应：** 204 No Content，前端移除对应 DOM 节点。

### 3.5 后端设计

#### 新建模块 `internal/partnernotes/handler.go`

```
internal/partnernotes/
└── handler.go
```

**Handler 结构和方法：**

```go
type Handler struct {
    db *sql.DB
}

func New(db *sql.DB) *Handler

// 获取跟进备注列表（返回 htmx 片段）
func (h *Handler) ListByPartner(c *gin.Context)

// 创建跟进备注（返回 htmx 片段）
func (h *Handler) Create(c *gin.Context)

// 删除跟进备注
func (h *Handler) Delete(c *gin.Context)

// 获取待跟进数量（用于 Dashboard）
func (h *Handler) PendingCount(ctx context.Context, companyID string) (int64, error)
```

**路由注册（`router.go`）：**

```go
noteH := partnernotes.New(db)

// 跟进备注 API
auth.POST("/api/partner-notes", noteH.Create)
auth.GET("/api/partner-notes/:partnerType/:partnerId", noteH.ListByPartner)
auth.DELETE("/api/partner-notes/:id", noteH.Delete)
```

### 3.6 前端设计

#### 客户/供应商详情页 — 跟进记录时间线

在现有「基本信息」卡片下方新增「跟进记录」卡片：

```
┌──────────────────────────────────────────────┐
│  跟进记录                          [+ 添加]  │
│                                               │
│  ● 2026-05-24  电话    下次跟进: 06-01       │
│  │  客户确认下周二下单，金额约5万，            │
│  │  需要提前备货。                            │
│  │                              [删除]       │
│  │                                           │
│  ● 2026-05-18  拜访                         │
│  │  上门拜访，了解了新产线需求，               │
│  │  对方有意向增加采购量。                    │
│  │                              [删除]       │
│  │                                           │
│  ● 2026-05-10  邮件    ⚠️ 已逾期             │
│  │  发送报价单，等待回复。                    │
│  │                              [删除]       │
│  │                                           │
│  └─ (更多按钮，如有 > 10 条)                  │
└──────────────────────────────────────────────┘
```

**视觉规范：**

- 时间线左侧竖线：`border-left: 2px solid warm-200`，节点为圆点
- 备注类型使用彩色徽章：
  - 电话 → 蓝色 `bg-blue-50 text-blue-700`
  - 拜访 → 紫色 `bg-purple-50 text-purple-700`
  - 邮件 → 琥珀色 `bg-amber-50 text-amber-700`
  - 微信 → 绿色 `bg-emerald-50 text-emerald-700`
  - 其他 → 灰色 `bg-warm-50 text-warm-600`
- `next_follow_up` 已逾期 → 红色标签 `⚠️ 已逾期 N 天`
- `next_follow_up` 未来3天内到期 → 黄色标签 `3天内需跟进`

#### 添加备注交互

点击「+ 添加」按钮后，在时间线顶部内联展开表单（非弹窗）：

```
┌──────────────────────────────────────────────┐
│  新增跟进记录                                  │
│                                               │
│  联系方式: [电话 ▾]  下次跟进: [____日期____]  │
│                                               │
│  ┌────────────────────────────────────────┐   │
│  │ 请输入跟进内容...                       │   │
│  │                                        │   │
│  └────────────────────────────────────────┘   │
│                                               │
│  [取消]                        [保存]         │
└──────────────────────────────────────────────┘
```

- 使用 htmx 提交：`hx-post="/api/partner-notes"` `hx-target="#timeline-list"` `hx-swap="afterbegin"`
- 保存成功后自动收起表单，新备注出现在时间线顶部
- 失败时在表单内显示错误提示

#### Dashboard 待跟进提醒卡片

在 Dashboard 统计卡片区域新增一个卡片（可选，P2 优先级）：

```
┌─────────────────────┐
│  待跟进              │
│  5                   │
│  2 条已逾期          │
│  ─────────────────   │
│  张三 (客户) 逾期3天 │
│  李四 (供应商) 明日  │
└─────────────────────┘
```

### 3.7 通用时间线组件

新建 `view/components/note_timeline.templ` 作为可复用组件：

```go
// 客户和供应商详情页共享的时间线渲染组件
templ NoteTimeline(partnerType string, partnerID uuid.UUID, notes []models.PartnerNote)

// 单条备注条目
templ NoteItem(note models.PartnerNote)

// 添加备注的内联表单
templ NoteForm(partnerType string, partnerID uuid.UUID)

// 空状态
templ NoteEmpty()
```

### 3.8 DB 迁移文件

**`012_partner_notes.up.sql`**
**`012_partner_notes.down.sql`**

---

## 4. 文件变更清单

### 功能一：现金流预测

| 操作 | 文件 | 说明 |
|------|------|------|
| 修改 | `internal/finance/handler.go` | 新增 `CashflowTrendAPI`、预测查询、账龄查询、环比计算 |
| 修改 | `view/dashboard/dashboard.templ` | 新增现金流趋势图表卡片 + Chart.js 配置 |
| 修改 | `view/finance/finance.templ` | 现金流区域升级（迷你图 + 账龄分布 + 环比） |
| 修改 | `internal/server/router.go` | 新增 `/api/finance/cashflow/trend` 路由 |

### 功能二：跟进备注

| 操作 | 文件 | 说明 |
|------|------|------|
| 新建 | `internal/db/migrations/012_partner_notes.up.sql` | 建表 + 索引 |
| 新建 | `internal/db/migrations/012_partner_notes.down.sql` | 回滚 |
| 修改 | `internal/models/models.go` | 新增 `PartnerNote` 结构体 |
| 新建 | `internal/partnernotes/handler.go` | CRUD handler + 待跟进查询 |
| 新建 | `view/components/note_timeline.templ` | 时间线通用组件 |
| 修改 | `view/customers/customer_detail.templ` | 引入时间线组件 |
| 修改 | `view/suppliers/supplier_detail.templ` | 引入时间线组件 |
| 修改 | `internal/server/router.go` | 注册备注路由 |

---

## 5. 开发计划与优先级

### 阶段划分

```
Phase 1 (现金流预测)          Phase 2 (跟进备注)
├── 后端 API                  ├── DB 迁移
│   ├── 趋势查询 API           ├── Model 定义
│   ├── 账龄分布 API           └── 后端 CRUD
│   └── 环比计算           ├── 时间线组件
├── Dashboard 图表卡片         │   ├── 列表渲染
└── Finance 页面升级           │   ├── 添加表单
                               │   └── 删除交互
                               ├── 客户详情页集成
                               └── 供应商详情页集成
```

### 里程碑

| 里程碑 | 内容 | 预估 |
|--------|------|------|
| M1 | 现金流趋势 API + Dashboard 图表 | 后端 + 前端各1个文件 |
| M2 | Finance 页面账龄分布 + 环比 | 1个前端文件 |
| M3 | partner_notes 建表 + CRUD API | 1个迁移 + 1个handler |
| M4 | 时间线组件 + 详情页集成 | 1个组件 + 2个页面 |
| M5 | Dashboard 待跟进卡片（可选） | 1个卡片 |

### 不做的事情

- 不做复杂预测模型（ARIMA、机器学习等）—— 数据量不足以支撑
- 不做独立的 CRM 模块（线索、商机、漏斗）—— 超出轻量定位
- 不做备注附件上传 —— 增加存储复杂度
- 不做备注 @提及 / 通知 —— 当前为单用户系统
- 不给 partner_notes 加外键 —— 避免与客户/供应商的删除操作产生约束冲突

---

## 附录 A：注意事项

1. **company_id 隔离**：所有新查询必须带上 `company_id` 条件，通过 middleware 注入
2. **空数据保护**：预测算法在无历史数据时应优雅降级（返回零值或仅显示实际数据）
3. **Chart.js 复用**：现金流图表复用 Dashboard 已引入的 `chart.min.js`，不新增依赖
4. **htmx 交互模式**：备注添加/删除遵循现有 htmx 模式（片段交换），不引入额外 JS 框架
5. **删除确认**：备注删除需 `confirm()` 确认，与现有删除按钮行为一致
