# 插件化：文档 + 约定

> 结论（2026-09 修订）：JS 插件引擎（Goja 沙箱）已移除。定制需求走
> **事件约定（`internal/hooks`）+ 外部集成**，核心保持零依赖、单二进制。

## 为什么降级

1. 真实定制到来之前，引擎是负债不是资产（VM 池 / Slot / 沙箱 12 个文件，0 个生产插件）。
2. ERP 定制的 90% 是“单据创建后通知外部系统 / 建单前校验拦截”，一个内存事件注册表足够。
3. 需要重型逻辑（WMS、MES、复杂计价）时，正确做法是独立服务调本系统 API，而不是往单二进制里塞解释器。

## 约定一：事件（internal/hooks）

业务在状态流转前后发布事件。命名：`<领域>.<动作>`，动作为 `creating/confirming/...`（前置，可拦截）与 `created/confirmed/...`（后置，异步）：

| 事件 | 时机 | Payload |
|---|---|---|
| `sales_order.creating` / `sales_order.created` | 销售单创建前后 | `customer_id, notes, properties` / `order_id` |
| `sales_order.confirming` / `sales_order.confirmed` | 确认扣库存前后 | `order_id` |
| `sales_order.shipping` / `sales_order.shipped` | 发货前后 | `order_id` |
| `sales_order.cancelling` / `sales_order.cancelled` | 取消前后 | `order_id` |
| `sales_order.deleting` / `sales_order.deleted` | 删除前后 | `order_id` |
| `purchase_order.*` | 同上（`receiving/received`, `paying/paid`） | `order_id` |
| `product.creating/created/updating/updated/deleting/deleted` | 商品 CRUD | `id, name, properties` |
| `customer.*` / `supplier.*` | 档案 CRUD | `id, name, properties` |
| `stock.adjusting` / `stock.adjusted` | 库存调整 | `product_id, type, quantity` |
| `payment.creating` / `reimbursement.*` | 收付 / 报销 | 见 handler |

- 前置：`hooks.Default.FireBefore(ctx, Event{Name, Payload})`，返回 `Result{Aborted, Reason, Patches}`；
  插件返回 `Aborted=true` 即拒绝本次操作（reason 会展示给用户）。
- 后置：`FireAfter` 异步执行，panic 会被隔离，不影响主流程。

Payload 键名是兼容契约：**只增不改**，改名必须发版说明。

## 约定二：扩展字段（properties JSONB）

`products / sales_orders / purchase_orders / customers / suppliers` 均有 `properties`
列。行业属性一律放这里，不加物理列：

```json
{ "vip_level": "gold", "fabric_batch": "B-2026-09" }
```

查询用 JSON 运算符并按需加 GIN 索引（见迁移文件）。

## 约定三：外部集成优先于内部脚本

- 通知类（短信/企微/物流）：订阅后置事件，起独立 worker / webhook 中继，不要进主进程。
- 校验拦截类：实现为同一进程内的 Go handler 并注册到 `hooks.Default`
 （`OnBeforeKeyed`），随版本发布，不接受运行时上传代码。
- 需要复用 UI：在对应 templ 页面按约定锚点加链接即可，不设动态 Slot 机
  制（动态注入曾是 XSS 与升级 break 的主要来源）。

## 何时重新引入引擎

同时满足：① ≥3 个付费客户要互不干扰的自定义规则；② 有专人负责插件安全审计。
届时从 git 历史恢复 `internal/plugins`（Goja），而不是重写。
