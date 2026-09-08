# StarOcean 功能设计文档：行业通用能力增强

> 版本: 1.0 | 日期: 2026-06-03 | 状态: 待评审

---

## 一、背景与目标

### 现状

StarOcean 已完成 M1-M3（骨架、进销存、财务核销）及 M3.5 插件引擎。通过对标行业领先 ERP「蔬东坡」，识别出以下**行业通用能力**缺失——这些功能不限于生鲜行业，而是制造业、商贸流通、餐饮供应链等绝大多数 SME ERP 场景的共同需求。

### 对标差距摘要

| 能力 | StarOcean 现状 | 蔬东坡能力 | 差距 |
|------|-------------|-----------|------|
| 阶梯定价 | 无，仅商品固定售价 | 按客户等级/区域/量阶梯定价 | **缺失** |
| 非标品称重 | 无，数量仅支持整数 | 实称实重，按实际重量结算 | **缺失** |
| 保质期/批次 | 无 | 临期预警、FIFO、效期追溯 | **缺失** |
| 分拣/出库任务 | 无 | 按客户/路线生成分拣单 | **缺失** |
| 客户分级 | 无分级，无专属定价 | VIP/普通/临时，独立价格表 | **缺失** |
| 供应商评估 | 无 | 准时率/品质/价格综合评分 | **缺失** |

### 设计原则

1. **通用优先**：只纳入跨行业通用需求，生鲜特异功能（AI 预测、智能排线、农残检测）由插件承载
2. **极简核心**：每个功能用最少的表和代码实现 80% 场景
3. **渐进增强**：优先级 P0 → P1 → P2，每个优先级可独立交付

---

## 二、优先级总览

| 优先级 | 功能模块 | 核心价值 | 预估工作量 |
|-------|---------|---------|-----------|
| **P0** | A. 阶梯定价 + 客户专属价格 | 不同客户不同价，所有行业核心需求 | 3 天 |
| **P0** | B. 非标品 + 称重管理 | 按重量计价商品，订单量 ≠ 结算量 | 2 天 |
| **P0** | C. 保质期 + 批次管理 | 临期预警、FIFO、效期追溯 | 3 天 |
| **P1** | D. 分拣/出库任务 | 按客户汇总生成分拣单，出库作业闭环 | 3 天 |
| **P1** | E. 客户分级 + 供应商评估 | 客户等级体系、供应商综合评分 | 2 天 |

---

## 三、功能 A：阶梯定价 + 客户专属价格

### 3.1 业务场景

- **阶梯定价**：购买 1-99 个单价 ¥10，100-499 个单价 ¥9，500+ 单价 ¥8
- **客户专属价格**：VIP 客户 A 对商品 X 的价格是 ¥9（覆盖默认阶梯）
- **优先级**：客户专属价 > 阶梯价 > 默认售价

### 3.2 数据模型

```sql
-- 商品阶梯价格表
CREATE TABLE product_price_tiers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    min_quantity INT NOT NULL DEFAULT 1,     -- 起始数量
    max_quantity INT,                         -- 截止数量（NULL = 无上限）
    unit_price DECIMAL(15,2) NOT NULL,       -- 该阶梯的单价
    priority INT NOT NULL DEFAULT 100,       -- 多条规则时的优先级
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_price_tiers_product ON product_price_tiers(product_id);

-- 客户专属价格表（覆盖默认价和阶梯价）
CREATE TABLE customer_product_prices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    unit_price DECIMAL(15,2) NOT NULL,        -- 固定专属价
    effective_from DATE,                       -- 生效日期
    effective_to DATE,                         -- 失效日期（NULL = 永久有效）
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    UNIQUE(customer_id, product_id)
);

CREATE INDEX idx_customer_price_lookup ON customer_product_prices(customer_id, product_id);
```

### 3.3 价格解析逻辑（Go 函数）

```
输入: customerID, productID, quantity
输出: unitPrice

步骤:
1. 查 customer_product_prices 表 → 如果有客户专属价 → 返回
2. 查 product_price_tiers 表 → 找到 min_quantity <= quantity 的最低价 → 返回
3. 返回 products.sale_price（默认售价）
```

### 3.4 UI 变更

- **商品详情页**：新增「阶梯价格」Tab，展示/编辑阶梯价
- **客户详情页**：新增「专属价格」Tab，展示/编辑客户专属价
- **销售订单编辑页**：选商品时自动调用价格引擎，显示最终价格
- **商品新建/编辑页**：添加「添加阶梯价」按钮

### 3.5 影响范围

| 文件 | 改动 |
|------|------|
| `internal/db/migrations/017_price_tiers.up.sql` | 新增 2 张表 |
| `internal/models/models.go` | 新增 PriceTier, CustomerProductPrice struct |
| `internal/orders/handler.go` | 销售订单创建/编辑时调用价格引擎 |
| `internal/products/handler.go` | 阶梯价格 CRUD API |
| `internal/customers/handler.go` | 客户专属价格 CRUD API |
| `view/products/product_detail.templ` | 新增阶梯价格 Tab |
| `view/customers/customer_detail.templ` | 新增专属价格 Tab |

---

## 四、功能 B：非标品 + 称重管理

### 4.1 业务场景

- **标准品**：按件数计价（如饮料、罐头），quantity = 整数
- **非标品**：按重量计价（如蔬菜、肉类、水产），下单 5kg，实称 4.8kg，按 4.8kg 结算
- 订单 item 需要记录：下单量（order_qty）、实际量（actual_qty）、单价（unit_price）、实际金额（actual_amount）

### 4.2 数据模型

```sql
-- 商品增加计量类型字段
ALTER TABLE products ADD COLUMN pricing_type VARCHAR(10) NOT NULL DEFAULT 'standard';
-- 'standard' = 按件计价, 'weight' = 按重计价
-- unit 字段在 weight 模式下表示重量单位（kg/g/斤）

-- 销售订单明细增加称重字段
ALTER TABLE sales_order_items ADD COLUMN actual_quantity DECIMAL(15,3);
ALTER TABLE sales_order_items ADD COLUMN actual_amount DECIMAL(15,2);
ALTER TABLE sales_order_items ADD COLUMN pricing_type VARCHAR(10) NOT NULL DEFAULT 'standard';

-- 采购订单明细增加称重字段
ALTER TABLE purchase_order_items ADD COLUMN actual_quantity DECIMAL(15,3);
ALTER TABLE purchase_order_items ADD COLUMN actual_amount DECIMAL(15,2);
ALTER TABLE purchase_order_items ADD COLUMN pricing_type VARCHAR(10) NOT NULL DEFAULT 'standard';
```

### 4.3 结算逻辑

```
标准品:
  amount = quantity × unit_price
  actual_amount = amount（无变化）

非标品（称重）:
  下单阶段: amount = order_quantity × unit_price（预估）
  称重阶段: actual_amount = actual_quantity × unit_price（实称）
  最终结算金额 = actual_amount（如果已称重），否则 = amount
```

### 4.4 UI 变更

- **商品编辑页**：新增「计价方式」下拉（标准品/称重品）
- **销售订单明细**：非标品行显示「实称重量」输入框，录入后自动计算实称金额
- **采购订单明细**：同上
- 订单金额汇总：总额取 `SUM(COALESCE(actual_amount, amount))`

### 4.5 影响范围

| 文件 | 改动 |
|------|------|
| `018_weight_pricing.up.sql` | ALTER TABLE 加字段 |
| `internal/models/models.go` | Product 增加 PricingType；OrderItem 增加 ActualQuantity/ActualAmount |
| `internal/orders/handler.go` | 创建/编辑订单时处理 pricing_type，称重录入接口 |
| `internal/products/handler.go` | 商品 CRUD 支持 pricing_type |
| `view/orders/sales_form.templ` | 非标品行显示称重输入 |
| `view/orders/sales_detail.templ` | 展示实称数据 |

---

## 五、功能 C：保质期 + 批次管理

### 5.1 业务场景

- 入库时记录生产日期/批次号，系统自动计算过期日期
- 临期预警：距过期 N 天内商品高亮提醒
- FIFO 先进先出：出库时优先发出早期批次
- 批次追溯：通过批次号可查到从入库到出库的全链路

### 5.2 数据模型

```sql
-- 商品增加保质期天数
ALTER TABLE products ADD COLUMN shelf_life_days INT;  -- NULL = 无保质期限制

-- 库存批次表（新增）
CREATE TABLE inventory_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id),
    warehouse_id VARCHAR(50) NOT NULL DEFAULT 'default',  -- 预留多仓
    batch_no VARCHAR(50) NOT NULL,                         -- 批次号
    production_date DATE,                                   -- 生产日期
    expiry_date DATE,                                       -- 过期日期（自动计算）
    quantity DECIMAL(15,3) NOT NULL DEFAULT 0,             -- 当前批次库存量
    original_quantity DECIMAL(15,3) NOT NULL DEFAULT 0,    -- 入库量
    unit_cost DECIMAL(15,2),                                -- 入库成本
    status VARCHAR(20) NOT NULL DEFAULT 'normal',          -- normal/expired/damaged
    reference_type VARCHAR(50),                             -- 入库来源类型
    reference_id UUID,                                      -- 入库来源ID
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_batches_product ON inventory_batches(product_id, warehouse_id);
CREATE INDEX idx_batches_expiry ON inventory_batches(expiry_date) WHERE status = 'normal';

-- 库存变动记录增加批次关联
ALTER TABLE inventory_movements ADD COLUMN batch_id UUID REFERENCES inventory_batches(id);
```

### 5.3 核心逻辑

```
入库（采购收货/生产入库）:
  1. 创建 inventory_batches 记录
  2. expiry_date = production_date + products.shelf_life_days（如果有）
  3. 批次数量 +original_quantity
  4. 触发 inventory_movement

出库（销售出库/领料）:
  1. 按 FIFO 排序：SELECT ... ORDER BY production_date ASC NULLS LAST
  2. 从最旧批次扣减，批次不足则跨批次扣减
  3. 触发 inventory_movement

临期预警:
  查询: expiry_date BETWEEN NOW() AND NOW() + N天
  在 Dashboard 和库存页高亮显示
```

### 5.4 UI 变更

- **商品编辑页**：新增「保质期（天）」字段
- **采购入库**：新增「批次号」「生产日期」输入，自动计算过期日期
- **库存页**：新增「批次视图」Tab，按批次展示库存；临期商品红色标注
- **Dashboard**：临期预警卡片

### 5.5 影响范围

| 文件 | 改动 |
|------|------|
| `019_batch_shelf_life.up.sql` | ALTER + 新表 |
| `internal/models/models.go` | Product 增加 ShelfLifeDays；新增 InventoryBatch struct |
| `internal/inventory/handler.go` | 批次 CRUD、FIFO 出库逻辑 |
| `internal/orders/handler.go` | 销售出库/采购入库时关联批次 |
| `internal/dashboard/handler.go` | 临期预警数据 |
| `view/inventory/inventory.templ` | 批次视图 Tab |

---

## 六、功能 D：分拣/出库任务

### 6.1 业务场景

- 销售订单确认后，系统自动按商品汇总生成分拣任务
- 仓库人员按分拣单拣货，确认后自动完成出库
- 支持按客户分拣（每个客户一张分拣单）或按商品分拣（每个商品汇总所有客户需求）

### 6.2 数据模型

```sql
-- 分拣/出库任务单
CREATE TABLE picking_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    picking_no VARCHAR(20) NOT NULL UNIQUE,        -- PK-20260603-001
    type VARCHAR(20) NOT NULL DEFAULT 'by_customer', -- by_customer / by_product
    status VARCHAR(20) NOT NULL DEFAULT 'pending',   -- pending/in_progress/completed/cancelled
    order_date DATE NOT NULL DEFAULT CURRENT_DATE,
    assigned_to VARCHAR(100),                       -- 分拣人员
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

-- 分拣明细
CREATE TABLE picking_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    picking_id UUID NOT NULL REFERENCES picking_orders(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    product_name VARCHAR(200) NOT NULL,
    product_code VARCHAR(20) NOT NULL,
    required_quantity DECIMAL(15,3) NOT NULL,      -- 需求数量
    picked_quantity DECIMAL(15,3) NOT NULL DEFAULT 0, -- 实际拣货数量
    source_order_id UUID,                           -- 来源销售订单ID
    source_customer_id UUID,                        -- 来源客户ID（by_customer 模式）
    source_customer_name VARCHAR(200),              -- 客户名称
    status VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending/picked/shortage
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_picking_orders_date ON picking_orders(order_date);
CREATE INDEX idx_picking_items_picking ON picking_items(picking_id);
CREATE INDEX idx_picking_items_product ON picking_items(product_id);
```

### 6.3 核心流程

```
生成分拣单:
  1. 筛选 status='confirmed' 的销售订单
  2. 按商品汇总所有订单明细 → 生成 by_product 类型分拣单
     或按客户汇总 → 生成 by_customer 类型分拣单
  3. 批次预分配（FIFO）

分拣执行:
  1. 仓库人员查看分拣单，逐条确认拣货数量
  2. 实拣 ≠ 需求 → 标记 shortage
  3. 全部完成 → 分拣单状态变 completed

自动出库:
  1. 分拣单 completed 后，自动触发库存扣减（关联批次）
  2. 对应销售订单状态流转为 shipped
```

### 6.4 UI 变更

- **侧边栏新增**：「分拣管理」导航项
- **分拣列表页**：按日期展示分拣单，状态筛选
- **分拣详情页**：分拣明细表格，支持逐行填写实拣数量
- **一键生成分拣单**：从销售订单列表页触发
- **Dashboard**：待分拣订单数量卡片

---

## 七、功能 E：客户分级 + 供应商评估

### 7.1 数据模型

```sql
-- 客户等级
ALTER TABLE customers ADD COLUMN tier VARCHAR(20) NOT NULL DEFAULT 'normal';
-- 'vip' / 'normal' / 'temporary'

-- 客户增加销售员绑定
ALTER TABLE customers ADD COLUMN sales_person VARCHAR(100);

-- 供应商评估指标
ALTER TABLE suppliers ADD COLUMN rating DECIMAL(3,1) DEFAULT 0;  -- 综合评分 0-10
ALTER TABLE suppliers ADD COLUMN on_time_rate DECIMAL(5,2) DEFAULT 0;  -- 准时交货率%
ALTER TABLE suppliers ADD COLUMN quality_rate DECIMAL(5,2) DEFAULT 0;  -- 质量合格率%
ALTER TABLE suppliers ADD COLUMN license_info TEXT;  -- 资质证照信息（JSONB 可通过 properties 扩展）
```

### 7.2 评分逻辑

```
供应商综合评分:
  - 准时交货率 (weight=0.4): 已按时收货的采购单 / 总采购单 × 10
  - 质量合格率 (weight=0.3): 无退货/投诉的采购单 / 总采购单 × 10
  - 价格竞争力 (weight=0.3): 基于历史采购价与市场均价对比 × 10

  评分 = 准时率×0.4 + 合格率×0.3 + 价格分×0.3

  定期（每月）自动计算更新
```

### 7.3 UI 变更

- **客户列表页**：显示等级标签（VIP 金色/普通灰色/临时蓝色），可筛选
- **客户编辑页**：新增「等级」「销售员」字段
- **供应商详情页**：新增「评估信息」Tab，展示评分和趋势

---

## 八、实施顺序

```
Phase 1 (P0): 阶梯定价 + 非标品称重 + 保质期批次
  1. 017_price_tiers 迁移 + 定价引擎 + UI
  2. 018_weight_pricing 迁移 + 称重逻辑 + UI
  3. 019_batch_shelf_life 迁移 + 批次管理 + FIFO + 临期预警

Phase 2 (P1): 分拣任务 + 客户分级 + 供应商评估
  4. 020_picking_orders 迁移 + 分拣流程 + UI
  5. ALTER customers/suppliers + 评分逻辑 + UI
```

每个功能可独立交付，互不依赖。

---

## 九、核心 vs 插件边界

以下为本次设计中**明确不纳入核心**、应通过插件实现的功能：

| 功能 | 归属 | 理由 |
|------|------|------|
| AI 采购预测 | 插件 | 依赖外部数据源，非通用 |
| 智能排线/路线优化 | 插件 | 算法复杂，仅配送场景需要 |
| 微信小程序商城 | 插件 | 需微信生态接入，非核心 ERP |
| 农残检测/食安溯源 | 插件 | 仅食品行业需要 |
| 金蝶/用友对接 | 插件 | 外部系统集成 |
| 智能硬件（分拣秤/PDA） | 插件 | 硬件协议差异大 |
| AI 语音/图像下单 | 插件 | 依赖外部 AI 服务 |

---

## 十、验证方案

1. **阶梯定价**：创建 3 级阶梯价的商品，用不同数量的销售订单验证价格匹配
2. **客户专属价**：为客户 A 设置商品 X 的专属价，验证下单时覆盖阶梯价
3. **非标品称重**：创建称重品商品，下单 5kg，实称 4.8kg，验证结算金额
4. **保质期批次**：入库带生产日期的商品，验证临期预警、FIFO 出库顺序
5. **分拣任务**：确认 3 笔销售订单，一键生成分拣单，完成分拣后验证库存扣减和订单状态流转
6. **客户分级**：设置客户为 VIP，验证等级标签显示
7. **供应商评估**：完成若干采购收货后，验证评分自动计算
