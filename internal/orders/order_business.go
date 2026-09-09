package orders

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/shopspring/decimal"
)

type SalesOrderInput struct {
	CustomerID   uuid.UUID
	OrderDate    time.Time
	DeliveryDate *time.Time
	Notes        string
	Properties   map[string]interface{}
	Items        []SalesOrderItemInput
}

type SalesOrderItemInput struct {
	ProductID uuid.UUID
	Quantity  int32
	UnitPrice string
	TaxRate   string
}

type PurchaseOrderInput struct {
	SupplierID   uuid.UUID
	OrderDate    time.Time
	DeliveryDate *time.Time
	Notes        string
	Properties   map[string]interface{}
	Items        []PurchaseOrderItemInput
}

type PurchaseOrderItemInput struct {
	ProductID uuid.UUID
	Quantity  int32
	UnitPrice string
	TaxRate   string
}

// resolveTaxRate 解析订单行税率：入参非空用之，否则取产品默认税率（0=不计税）。
func resolveTaxRate(ctx context.Context, tx *sql.Tx, productID uuid.UUID, input string) (decimal.Decimal, error) {
	if input != "" {
		v, err := decimal.NewFromString(input)
		if err != nil {
			return decimal.Zero, fmt.Errorf("税率无效: %s", input)
		}
		return v, nil
	}
	var dtr decimal.Decimal
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(default_tax_rate,0) FROM products WHERE id=$1`, productID).Scan(&dtr); err != nil {
		return decimal.Zero, err
	}
	return dtr, nil
}

func generateOrderNo(ctx context.Context, tx *sql.Tx, prefix string) (string, error) {
	today := time.Now()
	seqKey := prefix + "-" + today.Format("20060102")
	var seq int32
	err := tx.QueryRowContext(ctx, `
		INSERT INTO order_sequences (seq_key, last_seq) VALUES ($1, 1)
		ON CONFLICT (seq_key) DO UPDATE SET last_seq = order_sequences.last_seq + 1
		RETURNING last_seq`, seqKey).Scan(&seq)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%03d", prefix, today.Format("20060102"), seq), nil
}

func CreateSalesOrder(ctx context.Context, tx *sql.Tx, in SalesOrderInput) (orderID uuid.UUID, orderNo string, err error) {
	orderNo, err = generateOrderNo(ctx, tx, "SO")
	if err != nil {
		return
	}
	orderID = uuid.New()

	var deliveryDate interface{}
	if in.DeliveryDate != nil {
		deliveryDate = *in.DeliveryDate
	}
	notes := in.Notes

	err = tx.QueryRowContext(ctx, `
		INSERT INTO sales_orders (id, order_no, customer_id, status, order_date, delivery_date, notes, company_id, properties)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'default', $8)
		RETURNING total_amount`, orderID, orderNo, in.CustomerID, "draft", in.OrderDate, deliveryDate, notes, shared.JSONMapArg(in.Properties)).Scan(new(string))
	if err != nil {
		return
	}

	for _, item := range in.Items {
		var taxRate decimal.Decimal
		taxRate, err = resolveTaxRate(ctx, tx, item.ProductID, item.TaxRate)
		if err != nil {
			return
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO sales_order_items (id, order_id, product_id, quantity, unit_price, tax_rate)
			VALUES ($1, $2, $3, $4, $5, $6)`, uuid.New(), orderID, item.ProductID, item.Quantity, item.UnitPrice, taxRate)
		if err != nil {
			return
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE sales_orders SET total_amount = (SELECT COALESCE(SUM(amount),0) FROM sales_order_items WHERE order_id = $1), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		WHERE id = $1`, orderID)
	if err != nil {
		return
	}

	if err = checkCustomerCredit(ctx, tx, in.CustomerID, orderID); err != nil {
		return
	}

	return
}

func checkCustomerCredit(ctx context.Context, tx *sql.Tx, customerID, orderID uuid.UUID) error {
	var cust models.Customer
	// 锁住客户行：并发为同一客户创建订单时串行化，避免两个草稿同时通过信用检查
	err := tx.QueryRowContext(ctx, `SELECT id, COALESCE(credit_limit, 0), COALESCE(balance, 0) FROM customers WHERE id = $1`, customerID).
		Scan(&cust.ID, &cust.CreditLimit, &cust.Balance)
	if err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	var totalAmount decimal.Decimal
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(total_amount, 0) FROM sales_orders WHERE id = $1`, orderID).Scan(&totalAmount)
	if err != nil {
		return err
	}

	if !cust.CanExtendCredit(totalAmount) {
		newBalance := cust.Balance.Add(totalAmount)
		return fmt.Errorf("超出客户信用额度 (当前欠款: ¥%s, 信用额度: ¥%s)", newBalance.StringFixed(2), cust.CreditLimit.StringFixed(2))
	}
	return nil
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

const salesOrderCols = `id, order_no, COALESCE(customer_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
	COALESCE(status, 'draft'), COALESCE(total_amount, 0), COALESCE(paid_amount, 0),
	COALESCE(order_date, '1970-01-01'), delivery_date,
	COALESCE(notes, ''), COALESCE(created_at, '1970-01-01'),
	COALESCE(company_id, 'default'), COALESCE(properties, '{}')`

func scanSalesOrder(row *sql.Row) (models.SalesOrder, error) {
	var o models.SalesOrder
	var deliveryDate sql.NullTime
	err := row.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.Status, &o.TotalAmount,
		&o.PaidAmount, &o.OrderDate, &deliveryDate, &o.Notes, &o.CreatedAt,
		&o.CompanyID, &o.Properties)
	if err != nil {
		return o, err
	}
	if deliveryDate.Valid {
		o.DeliveryDate = deliveryDate.Time
	}
	return o, nil
}

func getSalesOrder(ctx context.Context, db querier, id uuid.UUID) (models.SalesOrder, error) {
	return scanSalesOrder(db.QueryRowContext(ctx, `SELECT `+salesOrderCols+` FROM sales_orders WHERE id = $1`, id))
}

// getSalesOrderForUpdate 在写事务内读取订单（外层 BeginTx = BEGIN IMMEDIATE
// 已串行化写者），避免两个请求同时读到同一旧状态导致重复扣库存（TOCTOU）。
func getSalesOrderForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID) (models.SalesOrder, error) {
	return scanSalesOrder(tx.QueryRowContext(ctx, `SELECT `+salesOrderCols+` FROM sales_orders WHERE id = $1`, id))
}

func getSalesOrderItems(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) ([]models.SalesOrderItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT soi.id, COALESCE(soi.order_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(soi.product_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
		       soi.quantity, COALESCE(soi.unit_price, 0), COALESCE(soi.amount, 0), COALESCE(soi.tax_rate, 0)
		FROM sales_order_items soi
		LEFT JOIN products p ON soi.product_id = p.id
		WHERE soi.order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.SalesOrderItem
	for rows.Next() {
		var it models.SalesOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID,
			&it.ProductName, &it.ProductCode, &it.Quantity, &it.UnitPrice, &it.Amount, &it.TaxRate); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// getProductForUpdate 在写事务内读取商品库存（BEGIN IMMEDIATE 串行化写者）。
func getProductForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID) (models.Product, error) {
	return scanProduct(tx.QueryRowContext(ctx, `SELECT `+productCols+` FROM products WHERE id = $1`, id))
}

const productCols = `id, code, name, COALESCE(category, ''), COALESCE(unit, ''),
	COALESCE(sale_price, 0), COALESCE(cost_price, 0),
	COALESCE(safety_stock, 0), COALESCE(current_stock, 0),
	COALESCE(created_at, '1970-01-01'),
	COALESCE(company_id, 'default'), COALESCE(properties, '{}')`

func scanProduct(row *sql.Row) (models.Product, error) {
	var p models.Product
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit,
		&p.SalePrice, &p.CostPrice, &p.SafetyStock, &p.CurrentStock, &p.CreatedAt,
		&p.CompanyID, &p.Properties)
	return p, err
}

// stockDelta 表示一次库存变动（扣减或增加）。
type stockDelta struct {
	ProductID uuid.UUID
	Quantity  int32
}

func salesItemDeltas(items []models.SalesOrderItem) []stockDelta {
	deltas := make([]stockDelta, 0, len(items))
	for _, it := range items {
		deltas = append(deltas, stockDelta{ProductID: it.ProductID, Quantity: it.Quantity})
	}
	return deltas
}

func purchaseItemDeltas(items []models.PurchaseOrderItem) []stockDelta {
	deltas := make([]stockDelta, 0, len(items))
	for _, it := range items {
		deltas = append(deltas, stockDelta{ProductID: it.ProductID, Quantity: it.Quantity})
	}
	return deltas
}

// sortDeltas 按商品 ID 固定加锁顺序，避免多个单据同时操作同一批商品时发生死锁。
func sortDeltas(deltas []stockDelta) {
	sort.Slice(deltas, func(i, j int) bool {
		return bytes.Compare(deltas[i].ProductID[:], deltas[j].ProductID[:]) < 0
	})
}

// applyStockOut 在事务内锁行扣减库存并记录流水。库存不足时拒绝（不允许超卖，
// 也不做静默截断——截断会破坏 before_stock/qty/after_stock 的流水平衡）。
// 调用方必须先锁单据行，再调用本函数（按固定顺序锁商品行）。
func applyStockOut(ctx context.Context, tx *sql.Tx, refType string, refID uuid.UUID, deltas []stockDelta) error {
	sortDeltas(deltas)
	for _, d := range deltas {
		if d.Quantity <= 0 {
			return fmt.Errorf("库存扣减数量必须为正数: %d", d.Quantity)
		}
		product, err := getProductForUpdate(ctx, tx, d.ProductID)
		if err != nil {
			return err
		}
		if product.CurrentStock < d.Quantity {
			return fmt.Errorf("库存不足: %s (%s) 当前库存 %d，需求 %d", product.Name, product.Code, product.CurrentStock, d.Quantity)
		}
		afterStock := product.AfterStockOut(d.Quantity)
		if err := insertMovement(ctx, tx, d.ProductID, "out", d.Quantity, refType, refID, product.CurrentStock, afterStock); err != nil {
			return err
		}
		if err := updateProductStock(ctx, tx, d.ProductID, afterStock); err != nil {
			return err
		}
	}
	return nil
}

// applyStockIn 在事务内锁行增加库存并记录流水。
func applyStockIn(ctx context.Context, tx *sql.Tx, refType string, refID uuid.UUID, deltas []stockDelta) error {
	sortDeltas(deltas)
	for _, d := range deltas {
		if d.Quantity <= 0 {
			return fmt.Errorf("库存增加数量必须为正数: %d", d.Quantity)
		}
		product, err := getProductForUpdate(ctx, tx, d.ProductID)
		if err != nil {
			return err
		}
		afterStock := product.AfterStockIn(d.Quantity)
		if err := insertMovement(ctx, tx, d.ProductID, "in", d.Quantity, refType, refID, product.CurrentStock, afterStock); err != nil {
			return err
		}
		if err := updateProductStock(ctx, tx, d.ProductID, afterStock); err != nil {
			return err
		}
	}
	return nil
}

func insertMovement(ctx context.Context, tx *sql.Tx, productID uuid.UUID, typ string, qty int32, refType string, refID uuid.UUID, before, after int32) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO inventory_movements (id, product_id, type, quantity, reference_type, reference_id, before_stock, after_stock)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New(), productID, typ, qty, refType, refID, before, after)
	return err
}

func updateProductStock(ctx context.Context, tx *sql.Tx, productID uuid.UUID, afterStock int32) error {
	_, err := tx.ExecContext(ctx, `UPDATE products SET current_stock = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, productID, afterStock)
	return err
}

func ConfirmSalesOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	// 状态校验在写事务内完成：BEGIN IMMEDIATE 串行化后，后到者读到新状态并拒绝。
	order, err := getSalesOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("confirmed") {
		return fmt.Errorf("cannot transition from %s to confirmed", order.Status)
	}
	items, err := getSalesOrderItems(ctx, tx, order.ID)
	if err != nil {
		return err
	}
	if err := applyStockOut(ctx, tx, "sales_order", order.ID, salesItemDeltas(items)); err != nil {
		return err
	}
	// 欠款累计：确认即记应收，信用检查才有账可算（取消/回款时冲回）。
	if _, err := tx.ExecContext(ctx, `UPDATE customers SET balance = balance + CAST($2 AS NUMERIC), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`,
		order.CustomerID, order.TotalAmount.StringFixed(2)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sales_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "confirmed")
	return err
}

func ShipSalesOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getSalesOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("shipped") {
		return fmt.Errorf("cannot transition from %s to shipped", order.Status)
	}
	_, err = tx.ExecContext(ctx, `UPDATE sales_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "shipped")
	return err
}

func InvoiceSalesOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getSalesOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("invoiced") {
		return fmt.Errorf("cannot transition from %s to invoiced", order.Status)
	}
	_, err = tx.ExecContext(ctx, `UPDATE sales_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "invoiced")
	return err
}

func CancelSalesOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getSalesOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("cancelled") {
		return fmt.Errorf("cannot cancel order in status %s", order.Status)
	}
	if order.NeedsStockReversal() {
		items, err := getSalesOrderItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		if err := applyStockIn(ctx, tx, "sales_order_cancel", order.ID, salesItemDeltas(items)); err != nil {
			return err
		}
		// 欠款冲回：确认时记的应收，取消时原路扣减。
		if _, err := tx.ExecContext(ctx, `UPDATE customers SET balance = balance - CAST($2 AS NUMERIC), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`,
			order.CustomerID, order.TotalAmount.StringFixed(2)); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sales_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "cancelled")
	return err
}

func DeleteSalesOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getSalesOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	// P0: 已过账单据禁止物理删除（审计要求）：仅 draft/cancelled 可删，
	// confirmed/shipped/invoiced 请先取消（Cancel 会冲销库存并保留痕迹）。
	// cancelled 单据已在 Cancel 时冲销库存，此处不再重复冲销。
	if order.Status != "draft" && order.Status != "cancelled" {
		return fmt.Errorf("仅草稿或已取消的销售订单可以删除，当前状态 %s 请先取消", order.Status)
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM sales_orders WHERE id = $1", id)
	return err
}

const purchaseOrderCols = `id, order_no, COALESCE(supplier_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
	COALESCE(status, 'draft'), COALESCE(total_amount, 0), COALESCE(paid_amount, 0),
	COALESCE(order_date, '1970-01-01'), delivery_date,
	COALESCE(notes, ''), COALESCE(created_at, '1970-01-01'),
	COALESCE(company_id, 'default'), COALESCE(properties, '{}')`

func scanPurchaseOrder(row *sql.Row) (models.PurchaseOrder, error) {
	var o models.PurchaseOrder
	var deliveryDate sql.NullTime
	err := row.Scan(&o.ID, &o.OrderNo, &o.SupplierID, &o.Status, &o.TotalAmount,
		&o.PaidAmount, &o.OrderDate, &deliveryDate, &o.Notes, &o.CreatedAt,
		&o.CompanyID, &o.Properties)
	if err != nil {
		return o, err
	}
	if deliveryDate.Valid {
		o.DeliveryDate = deliveryDate.Time
	}
	return o, nil
}

func getPurchaseOrder(ctx context.Context, db querier, id uuid.UUID) (models.PurchaseOrder, error) {
	return scanPurchaseOrder(db.QueryRowContext(ctx, `SELECT `+purchaseOrderCols+` FROM purchase_orders WHERE id = $1`, id))
}

// getPurchaseOrderForUpdate 与 getSalesOrderForUpdate 同理：锁行后再做状态流转。
func getPurchaseOrderForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID) (models.PurchaseOrder, error) {
	return scanPurchaseOrder(tx.QueryRowContext(ctx, `SELECT `+purchaseOrderCols+` FROM purchase_orders WHERE id = $1`, id))
}

func getPurchaseOrderItems(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) ([]models.PurchaseOrderItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT poi.id, COALESCE(poi.order_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(poi.product_id, (lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr(lower(hex(randomblob(2))),1,4)||'-'||lower(hex(randomblob(6))))),
		       COALESCE(p.name, '') as product_name, COALESCE(p.code, '') as product_code,
		       poi.quantity, COALESCE(poi.unit_price, 0), COALESCE(poi.amount, 0), COALESCE(poi.tax_rate, 0)
		FROM purchase_order_items poi
		LEFT JOIN products p ON poi.product_id = p.id
		WHERE poi.order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.PurchaseOrderItem
	for rows.Next() {
		var it models.PurchaseOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID,
			&it.ProductName, &it.ProductCode, &it.Quantity, &it.UnitPrice, &it.Amount, &it.TaxRate); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func CreatePurchaseOrder(ctx context.Context, tx *sql.Tx, in PurchaseOrderInput) (orderID uuid.UUID, orderNo string, err error) {
	orderNo, err = generateOrderNo(ctx, tx, "PO")
	if err != nil {
		return
	}
	orderID = uuid.New()

	var deliveryDate interface{}
	if in.DeliveryDate != nil {
		deliveryDate = *in.DeliveryDate
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO purchase_orders (id, order_no, supplier_id, status, order_date, delivery_date, notes, company_id, properties)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'default', $8)
		RETURNING total_amount`, orderID, orderNo, in.SupplierID, "draft", in.OrderDate, deliveryDate, in.Notes, shared.JSONMapArg(in.Properties)).Scan(new(string))
	if err != nil {
		return
	}

	for _, item := range in.Items {
		var taxRate decimal.Decimal
		taxRate, err = resolveTaxRate(ctx, tx, item.ProductID, item.TaxRate)
		if err != nil {
			return
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO purchase_order_items (id, order_id, product_id, quantity, unit_price, tax_rate)
			VALUES ($1, $2, $3, $4, $5, $6)`, uuid.New(), orderID, item.ProductID, item.Quantity, item.UnitPrice, taxRate)
		if err != nil {
			return
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE purchase_orders SET total_amount = (SELECT COALESCE(SUM(amount),0) FROM purchase_order_items WHERE order_id = $1), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		WHERE id = $1`, orderID)
	return
}

func ConfirmPurchaseOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getPurchaseOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("confirmed") {
		return fmt.Errorf("cannot transition from %s to confirmed", order.Status)
	}
	_, err = tx.ExecContext(ctx, `UPDATE purchase_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "confirmed")
	return err
}

func ReceivePurchaseOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getPurchaseOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("received") {
		return fmt.Errorf("cannot transition from %s to received", order.Status)
	}
	items, err := getPurchaseOrderItems(ctx, tx, order.ID)
	if err != nil {
		return err
	}
	if err := applyStockIn(ctx, tx, "purchase_order", order.ID, purchaseItemDeltas(items)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE purchase_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "received")
	return err
}

func PayPurchaseOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getPurchaseOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("paid") {
		return fmt.Errorf("cannot transition from %s to paid", order.Status)
	}
	_, err = tx.ExecContext(ctx, `UPDATE purchase_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "paid")
	return err
}

func CancelPurchaseOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getPurchaseOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if !order.CanTransitionTo("cancelled") {
		return fmt.Errorf("cannot cancel order in status %s", order.Status)
	}
	if order.NeedsStockReversal() {
		items, err := getPurchaseOrderItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		if err := applyStockOut(ctx, tx, "purchase_order_cancel", order.ID, purchaseItemDeltas(items)); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE purchase_orders SET status = $2, updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id = $1`, id, "cancelled")
	return err
}

func DeletePurchaseOrder(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	order, err := getPurchaseOrderForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	// P0: 同 DeleteSalesOrder：仅 draft/cancelled 可物理删除，已收货/已付款请走取消流程。
	if order.Status != "draft" && order.Status != "cancelled" {
		return fmt.Errorf("仅草稿或已取消的采购订单可以删除，当前状态 %s 请先取消", order.Status)
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM purchase_orders WHERE id = $1", id)
	return err
}
