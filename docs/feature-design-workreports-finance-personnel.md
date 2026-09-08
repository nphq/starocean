# StarOcean 功能设计文档：工作日报 + 财务管理增强 + 人员薪资管理

## Context

StarOcean 目前已实现：销售订单、采购管理、商品/库存、客户/供应商管理、收付流水（含现金流预测和应收应付账龄）、考勤打卡（企业微信/钉钉导入）、Dashboard、CRM跟进记录。

本设计补充三个缺失模块：
1. **工作报告** — 支持日报+周报，自动汇总
2. **财务管理增强** — 费用报销、发票管理、对账管理
3. **人员与薪资管理** — 组织架构、员工档案、合同管理、薪资核算

---

## 模块一：工作报告（日报/周报）

### 数据模型

**work_reports 表** — 统一存储日报和周报

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| type | VARCHAR(10) | `daily` / `weekly` |
| report_date | DATE NOT NULL | 报告日期 |
| employee_code | VARCHAR(50) | 员工编码 |
| employee_name | VARCHAR(200) | 员工姓名 |
| department | VARCHAR(200) | 部门 |
| work_done | TEXT | 今日工作内容 |
| tomorrow_plan | TEXT | 明日计划 |
| issues | TEXT | 遇到的问题 |
| status | VARCHAR(20) | `draft` / `submitted` |
| company_id / created_at / updated_at | 标准字段 | |

**weekly_report_links 表** — 周报关联的日报

| 字段 | 类型 | 说明 |
|------|------|------|
| weekly_report_id | UUID FK | 关联周报 |
| daily_report_id | UUID FK | 关联日报 |

**report_templates 表** — 日报/周报模板，快速填入常用内容

| 字段 | 类型 | 说明 |
|------|------|------|
| name | VARCHAR(100) | 模板名称 |
| type | VARCHAR(10) | daily/weekly |
| work_done / tomorrow_plan / issues | TEXT | 预填内容 |
| is_default | BOOLEAN | 是否默认模板 |

### 页面与功能

| 路由 | 功能 | 说明 |
|------|------|------|
| `/workreports` | 报告列表 | 日报/周报 Tab 切换，日期筛选，员工筛选，统计卡片 |
| `/workreports/new` | 新建报告 | 表单：类型切换(日报/周报)，可选从模板预填，工作内容/计划/问题三个 textarea |
| `/workreports/:id` | 报告详情 | 完整内容展示，状态标签 |
| `/workreports/:id/edit` | 编辑报告 | |
| `/workreports/weekly/generate` | 生成周报 | 选择周范围，自动聚合该周日报内容，预览后生成周报 |
| `/workreports/templates` | 模板管理 | 增删改查模板 |

### 导航

放在侧边栏「业务管理」分组，考勤打卡下方，图标为文档图标。

---

## 模块二：财务管理增强

### 2.1 费用报销

**reimbursements 表**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| reimbursement_no | VARCHAR(20) UNIQUE | 自动编号 BX-20260525-001 |
| applicant_name | VARCHAR(200) | 申请人 |
| department | VARCHAR(200) | 部门 |
| amount | DECIMAL(15,2) | 报销总额 |
| category | VARCHAR(50) | 费用类别（差旅/办公/招待/其他） |
| description | TEXT | 说明 |
| status | VARCHAR(20) | `draft` → `pending_approval` → `approved` / `rejected` → `paid` |
| approver_name | VARCHAR(200) | 审批人 |
| approved_at | TIMESTAMPTZ | 审批时间 |
| rejected_reason | TEXT | 驳回原因 |
| payment_id | UUID FK→payments | 关联付款记录 |
| expense_date | DATE | 费用发生日期 |

**reimbursement_items 表** — 报销明细行

| 字段 | 类型 | 说明 |
|------|------|------|
| reimbursement_id | UUID FK | |
| category | VARCHAR(50) | 明细类别 |
| amount | DECIMAL(15,2) | 明细金额 |
| description | TEXT | 说明 |

### 页面与功能

| 路由 | 功能 |
|------|------|
| `/finance/reimbursements` | 报销列表，状态 Tab 筛选（全部/待审批/已通过/已驳回/已付款） |
| `/finance/reimbursements/new` | 新建报销单，支持动态添加/删除明细行（htmx），总额自动计算 |
| `/finance/reimbursements/:id` | 报销详情，审批操作按钮 |
| `/finance/reimbursements/:id/submit` | 提交审批 |
| `/finance/reimbursements/:id/approve` | 审批通过 |
| `/finance/reimbursements/:id/reject` | 驳回 |
| `/finance/reimbursements/:id/pay` | 付款（自动创建 payment 记录） |

### 2.2 发票管理

**invoices 表**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| invoice_no | VARCHAR(50) UNIQUE | 发票号码 |
| type | VARCHAR(10) | `input`（进项）/ `output`（销项） |
| partner_type | VARCHAR(10) | `customer` / `supplier` |
| partner_id | UUID | 关联客户/供应商 |
| partner_name | VARCHAR(200) | |
| amount | DECIMAL(15,2) | 不含税金额 |
| tax_rate | DECIMAL(5,2) | 税率 |
| tax_amount | DECIMAL(15,2) | 税额 |
| total_amount | GENERATED | 金额+税额（自动计算列） |
| invoice_date | DATE | 开票日期 |
| invoice_code | VARCHAR(50) | 发票代码 |
| invoice_status | VARCHAR(20) | `normal` / `void` / `red` |
| reference_type | VARCHAR(30) | 关联订单类型（sales_order/purchase_order） |
| reference_id | UUID | 关联订单 ID |

### 页面与功能

| 路由 | 功能 |
|------|------|
| `/finance/invoices` | 发票列表，进项/销项 Tab，统计卡片（进项总额/销项总额/税额合计） |
| `/finance/invoices/new` | 新建发票，关联订单下拉选择，税率自动计算税额 |
| `/finance/invoices/:id` | 发票详情，显示关联订单信息 |
| `/finance/invoices/:id/void` | 作废发票 |

### 2.3 对账管理

**reconciliations 表**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| reconciliation_no | VARCHAR(20) UNIQUE | 自动编号 DZ-202605-001 |
| partner_type | VARCHAR(10) | customer / supplier |
| partner_id | UUID | |
| partner_name | VARCHAR(200) | |
| period_start / period_end | DATE | 对账周期 |
| order_total | DECIMAL(15,2) | 订单合计（自动计算） |
| payment_total | DECIMAL(15,2) | 付款合计（自动计算） |
| discrepancy | DECIMAL(15,2) | 差异额 |
| status | VARCHAR(20) | `draft` / `confirmed` |

**reconciliation_items 表** — 对账明细

| 字段 | 类型 | 说明 |
|------|------|------|
| item_type | VARCHAR(20) | `order` / `payment` |
| reference_no | VARCHAR(50) | 订单号/付款参考号 |
| amount | DECIMAL(15,2) | 金额 |
| reference_date | DATE | 日期 |

### 页面与功能

| 路由 | 功能 |
|------|------|
| `/finance/reconciliations` | 对账单列表 |
| `/finance/reconciliations/new` | 新建对账：选择客户/供应商+日期范围，自动拉取订单和付款明细预览 |
| `/finance/reconciliations/:id` | 对账详情：订单明细组 + 付款明细组 + 差异高亮 |

### 导航

侧边栏「库存与财务」分组新增三个导航项：费用报销、发票管理、对账管理（与收付流水并列）。

---

## 模块三：人员与薪资管理

### 3.1 组织架构

**departments 表** — 部门（树形结构）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| name | VARCHAR(100) | 部门名称 |
| code | VARCHAR(20) UNIQUE | 部门编码 |
| parent_id | UUID FK→departments | 上级部门（支持多级树） |
| manager_name | VARCHAR(200) | 负责人 |
| sort_order | INT | 排序 |

**positions 表** — 岗位

| 字段 | 类型 | 说明 |
|------|------|------|
| name | VARCHAR(100) | 岗位名称 |
| department_id | UUID FK | 所属部门 |
| base_salary | DECIMAL(15,2) | 岗位默认底薪 |

### 页面与功能

| 路由 | 功能 |
|------|------|
| `/personnel/departments` | 部门树形展示（Alpine.js 展开/折叠），显示每个部门人数 |
| `/personnel/departments/new` | 新建部门（上级部门下拉选择） |
| `/personnel/positions` | 岗位列表，按部门分组展示 |
| `/personnel/positions/new` | 新建岗位（关联部门，设默认底薪） |

### 3.2 员工档案

**employees 表**

| 字段 | 类型 | 说明 |
|------|------|------|
| id | UUID PK | |
| code | VARCHAR(50) UNIQUE | 员工编码（自动生成） |
| name | VARCHAR(200) | 姓名 |
| department_id | UUID FK | 部门 |
| position_id | UUID FK | 岗位 |
| phone / email | | 联系方式 |
| id_number | VARCHAR(18) | 身份证号 |
| hire_date | DATE | 入职日期 |
| separation_date | DATE | 离职日期 |
| status | VARCHAR(20) | `active` / `probation` / `inactive` |
| emergency_contact / emergency_phone | | 紧急联系人 |
| bank_name / bank_account | | 银行卡信息 |
| properties | JSONB | 扩展字段 |

**contracts 表** — 劳动合同

| 字段 | 类型 | 说明 |
|------|------|------|
| employee_id | UUID FK | |
| contract_no | VARCHAR(50) UNIQUE | 合同编号 |
| contract_type | VARCHAR(30) | 固定期限/无固定期限/劳务 |
| start_date / end_date | DATE | |
| salary | DECIMAL(15,2) | 合同薪资 |
| status | VARCHAR(20) | `active` / `expired` / `terminated` |

### 页面与功能

| 路由 | 功能 |
|------|------|
| `/personnel/employees` | 员工列表，部门筛选 + 状态 Tab |
| `/personnel/employees/new` | 新建员工（部门/岗位下拉，自动生成编码） |
| `/personnel/employees/:id` | 员工详情（基本信息 + 合同列表 + 薪资历史） |
| `/personnel/employees/:id/deactivate` | 办理离职 |
| `/personnel/contracts` | 合同列表，到期提醒（30天内到期标黄） |
| `/personnel/contracts/new` | 新建合同（关联员工） |

### 3.3 薪资管理

**salary_components 表** — 月度薪资明细

| 字段 | 类型 | 说明 |
|------|------|------|
| employee_id | UUID FK | |
| month | DATE | 月份（YYYY-MM-01） |
| base_salary | DECIMAL(15,2) | 底薪（从岗位默认值带入） |
| overtime_pay | DECIMAL(15,2) | 加班费 |
| bonus | DECIMAL(15,2) | 绩效奖金 |
| deduction | DECIMAL(15,2) | 扣款 |
| social_security | DECIMAL(15,2) | 社保 |
| housing_fund | DECIMAL(15,2) | 公积金 |
| tax | DECIMAL(15,2) | 个税 |
| net_salary | GENERATED | 应发合计（自动计算列） |
| status | VARCHAR(20) | `draft` / `confirmed` |
| UNIQUE(employee_id, month) | | 每人每月唯一 |

### 页面与功能

| 路由 | 功能 |
|------|------|
| `/personnel/salary` | 薪资总览表：月份选择器 + 所有员工薪资汇总，底部显示月度总额，确认按钮锁定 |
| `/personnel/salary/batch` | 批量录入：选择月份 → 显示所有在职员工 → 底薪自动填入 → 编辑加班/奖金/扣款等 → Alpine.js 实时计算行合计 → 一次性提交 |
| `/personnel/salary/:id` | 个人薪资条详情 |
| `/personnel/salary/:id/confirm` | 确认月度薪资（确认后不可修改） |
| `/api/personnel/salary/summary` | 薪资趋势图数据 |
| `/api/personnel/attendance-stats` | 考勤统计接口（迟到/缺勤/加班时长），供薪资批量录入页调用 |

### 导航

侧边栏新增「人事管理」分组，包含：员工档案、组织架构、薪资管理三个导航项。

---

## 跨模块集成

### 考勤 ↔ 薪资
- 薪资批量录入页调用考勤统计接口，自动关联加班时长和缺勤扣款
- 员工姓名自动匹配考勤记录

### 报销 ↔ 收付流水
- 报销付款时自动创建 payment 记录（type='支出'），保持现金流视图准确

### 发票 ↔ 订单
- 发票可关联销售/采购订单，订单详情页展示关联发票

### 对账 ↔ 订单/付款
- 对账单自动拉取指定客户/供应商在时间范围内的订单和付款记录

### Dashboard 增强
- 新增统计卡片：待审批报销数、在职员工数、本周报告提交数、合同即将到期数

### 全局搜索扩展
- 新增搜索实体：员工、工作报告、发票、报销单

---

## 数据库迁移文件

| 编号 | 文件名 | 内容 |
|------|--------|------|
| 013 | `013_work_reports.up.sql` / `.down.sql` | work_reports, weekly_report_links, report_templates |
| 014 | `014_finance_enhancement.up.sql` / `.down.sql` | reimbursements, reimbursement_items, invoices, reconciliations, reconciliation_items, reimbursement_sequences, reconciliation_sequences |
| 015 | `015_personnel_salary.up.sql` / `.down.sql` | departments, positions, employees, contracts, salary_components |

---

## 新增文件清单

```
internal/
  workreports/handler.go
  personnel/handler.go
  db/migrations/013_work_reports.{up,down}.sql
  db/migrations/014_finance_enhancement.{up,down}.sql
  db/migrations/015_personnel_salary.{up,down}.sql

view/
  workreports/data.go, reports.templ, report_form.templ, report_detail.templ, weekly_generate.templ
  finance/reimbursements.templ, reimbursement_form.templ, reimbursement_detail.templ
  finance/invoices.templ, invoice_form.templ, invoice_detail.templ
  finance/reconciliations.templ, reconciliation_form.templ, reconciliation_detail.templ
  personnel/data.go, departments.templ, department_form.templ, positions.templ, position_form.templ
  personnel/employees.templ, employee_form.templ, employee_detail.templ
  personnel/contracts.templ, contract_form.templ
  personnel/salary_list.templ, salary_batch.templ, salary_detail.templ
```

## 需修改的现有文件

| 文件 | 修改内容 |
|------|----------|
| `internal/models/models.go` | 新增 ~15 个 model struct |
| `internal/models/domain.go` | 新增报销状态流转逻辑 |
| `internal/server/router.go` | 注册 ~30 条新路由 |
| `internal/finance/handler.go` | 新增报销/发票/对账 ~20 个 handler 方法 |
| `internal/shared/global.go` | 扩展全局搜索 |
| `internal/dashboard/handler.go` | 新增 Dashboard 统计项 |
| `view/layout/base.templ` | 新增侧边栏导航项 |
| `view/finance/data.go` | 新增报销/发票/对账 view data struct |
| `view/components/badge.templ` | 扩展状态标签样式 |

---

## 实施顺序

1. **Phase 1**: 工作报告 — 最简单，无外部依赖
2. **Phase 2**: 财务管理增强 — 扩展现有 finance 模块
3. **Phase 3**: 人员薪资管理 — 最复杂，依赖考勤数据和付款集成

每个 Phase 内按：迁移 → 模型 → Handler → 模板 → 路由 → 导航 → Dashboard/搜索集成 的顺序实施。
