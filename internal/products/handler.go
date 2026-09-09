package products

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/ledger"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/shopspring/decimal"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

const productCols = `id, code, name, COALESCE(category,'') as category, COALESCE(unit,'') as unit,
       COALESCE(sale_price, 0) as sale_price, COALESCE(cost_price, 0) as cost_price,
       COALESCE(safety_stock,0) as safety_stock, COALESCE(current_stock,0) as current_stock,
       COALESCE(pricing_type, 'standard') as pricing_type,
       COALESCE(shelf_life_days,0) as shelf_life_days,
       COALESCE(created_at, '1970-01-01') as created_at,
       COALESCE(company_id,'default') as company_id,
       COALESCE(properties,'{}') as properties`

const listProductsSQL = `SELECT ` + productCols + ` FROM products ORDER BY products.created_at DESC NULLS LAST, products.id DESC LIMIT $1`

const listProductsAfterSQL = `SELECT ` + productCols + ` FROM products
	WHERE (created_at, id) < ($1, $2)
	ORDER BY created_at DESC NULLS LAST, id DESC LIMIT $3`

const getProductSQL = `SELECT ` + productCols + ` FROM products WHERE id = $1`

const searchProductsSQL = `SELECT ` + productCols + ` FROM products
WHERE search_text LIKE '%' || $1 || '%'
ORDER BY name LIMIT 20`

const createProductSQL = `INSERT INTO products (id, code, name, category, unit, sale_price, cost_price, safety_stock, pricing_type, shelf_life_days, properties)
VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), CAST(NULLIF($6,'') AS NUMERIC), CAST(NULLIF($7,'') AS NUMERIC), $8, COALESCE(NULLIF($9,''), 'standard'), $10, $11)
RETURNING ` + productCols

const updateProductSQL = `UPDATE products SET name = $2, category = NULLIF($3,''), unit = NULLIF($4,''),
       sale_price = CAST(NULLIF($5,'') AS NUMERIC), cost_price = CAST(NULLIF($6,'') AS NUMERIC), safety_stock = $7,
       pricing_type = COALESCE(NULLIF($8,''), pricing_type), shelf_life_days = $9,
       properties = json_patch(properties, $10), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
WHERE id = $1
RETURNING ` + productCols

type productInput struct {
	Code          string                 `json:"code"`
	Name          string                 `json:"name"`
	Category      string                 `json:"category"`
	Unit          string                 `json:"unit"`
	SalePrice     string                 `json:"sale_price"`
	CostPrice     string                 `json:"cost_price"`
	SafetyStock   int32                  `json:"safety_stock"`
	PricingType   string                 `json:"pricing_type"`
	ShelfLifeDays int32                  `json:"shelf_life_days"`
	Properties    map[string]interface{} `json:"properties"`
}

type stockAdjustInput struct {
	Quantity int32  `json:"quantity"`
	Type     string `json:"type"`
}

type priceTierInput struct {
	MinQuantity int32  `json:"min_quantity"`
	MaxQuantity int32  `json:"max_quantity"`
	UnitPrice   string `json:"unit_price"`
}

func scanProduct(row interface{ Scan(...interface{}) error }) (models.Product, error) {
	var p models.Product
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit,
		&p.SalePrice, &p.CostPrice, &p.SafetyStock, &p.CurrentStock, &p.PricingType,
		&p.ShelfLifeDays,
		&p.CreatedAt, &p.CompanyID, &p.Properties)
	return p, err
}

func (h *Handler) ProductsPage(c *gin.Context) {
	ctx := c.Request.Context()
	cursorTime, cursorID, hasCursor := parseCursor(c.Query("cursor"))

	var rows *sql.Rows
	var err error
	if hasCursor {
		rows, err = h.db.QueryContext(ctx, listProductsAfterSQL, cursorTime, cursorID, 21)
	} else {
		rows, err = h.db.QueryContext(ctx, listProductsSQL, 21)
	}
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var prods []models.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		prods = append(prods, p)
	}
	hasMore := len(prods) > 20
	if hasMore {
		prods = prods[:20]
	}
	nextCursor := ""
	if hasMore && len(prods) > 0 {
		last := prods[len(prods)-1]
		nextCursor = last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID.String()
	}
	shared.JSONOK(c, gin.H{
		"items":       shared.EmptySlice(prods),
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
}

func parseCursor(s string) (time.Time, uuid.UUID, bool) {
	if s == "" {
		return time.Time{}, uuid.Nil, false
	}
	parts := strings.SplitN(s, "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return t, id, true
}

func (h *Handler) ProductCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in productInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if in.Name == "" {
		shared.JSONBadRequest(c, "名称不能为空")
		return
	}
	// P0: 单价校验，避免非法字符串经 NULLIF 写入 NULL 或静默归零。
	if in.SalePrice != "" {
		if _, err := shared.ParsePrice(in.SalePrice); err != nil {
			shared.JSONBadRequest(c, "售价格式无效")
			return
		}
	}
	if in.CostPrice != "" {
		if _, err := shared.ParsePrice(in.CostPrice); err != nil {
			shared.JSONBadRequest(c, "成本价格式无效")
			return
		}
	}
	if in.SafetyStock < 0 {
		shared.JSONBadRequest(c, "安全库存不能为负数")
		return
	}
	payload := map[string]interface{}{"name": in.Name, "code": in.Code, "properties": in.Properties}
	res := shared.FireBefore(ctx, "product.creating", payload)
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	props := shared.PropertiesFromPatch(res, in.Properties)
	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	code := GenerateProductCode(count)
	if in.Code != "" {
		code = in.Code
	}
	row := h.db.QueryRowContext(ctx, createProductSQL,
		uuid.New(), code, in.Name,
		in.Category, in.Unit, in.SalePrice, in.CostPrice,
		in.SafetyStock, in.PricingType, in.ShelfLifeDays, shared.JSONMapArg(props),
	)
	p, err := scanProduct(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("product.created", map[string]interface{}{"id": p.ID.String()})
	shared.JSONCreated(c, p)
}

func (h *Handler) ProductDetailPage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	row := h.db.QueryRowContext(c.Request.Context(), getProductSQL, id)
	product, err := scanProduct(row)
	if err != nil {
		shared.JSONNotFound(c, "商品不存在")
		return
	}
	shared.JSONOK(c, product)
}

func (h *Handler) ProductUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	var in productInput
	if !shared.BindJSON(c, &in) {
		return
	}
	ctx := c.Request.Context()
	if in.SalePrice != "" {
		if _, err := shared.ParsePrice(in.SalePrice); err != nil {
			shared.JSONBadRequest(c, "售价格式无效")
			return
		}
	}
	if in.CostPrice != "" {
		if _, err := shared.ParsePrice(in.CostPrice); err != nil {
			shared.JSONBadRequest(c, "成本价格式无效")
			return
		}
	}
	if in.SafetyStock < 0 {
		shared.JSONBadRequest(c, "安全库存不能为负数")
		return
	}
	payload := map[string]interface{}{"id": id.String(), "name": in.Name, "properties": in.Properties}
	res := shared.FireBefore(ctx, "product.updating", payload)
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	props := shared.PropertiesFromPatch(res, in.Properties)
	row := h.db.QueryRowContext(ctx, updateProductSQL,
		id, in.Name, in.Category, in.Unit, in.SalePrice, in.CostPrice,
		in.SafetyStock, in.PricingType, in.ShelfLifeDays, shared.JSONMapArg(props),
	)
	p, err := scanProduct(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("product.updated", map[string]interface{}{"id": id.String()})
	shared.JSONOK(c, p)
}

func (h *Handler) ProductDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	if shared.AbortIfPluginRejected(c, shared.FireBefore(ctx, "product.deleting", map[string]interface{}{"id": id.String()})) {
		return
	}
	// P0: 被单据/库存流水引用的商品禁止删除，避免破坏审计链；请先处理关联单据。
	var refCount int64
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT 1 FROM sales_order_items WHERE product_id = $1 UNION ALL
		SELECT 1 FROM purchase_order_items WHERE product_id = $1 UNION ALL
		SELECT 1 FROM inventory_movements WHERE product_id = $1 LIMIT 1
	) t`, id).Scan(&refCount); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	if refCount > 0 {
		shared.JSONBadRequest(c, "该商品存在关联单据或库存流水，不能删除")
		return
	}
	_, err = h.db.ExecContext(ctx, "DELETE FROM products WHERE id = $1", id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("product.deleted", map[string]interface{}{"id": id.String()})
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ProductStockAdjust(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	var in stockAdjustInput
	if !shared.BindJSON(c, &in) {
		return
	}
	qty := int64(in.Quantity)
	adjType := in.Type
	if adjType != "in" && adjType != "out" {
		adjType = "in"
	}
	if qty <= 0 {
		shared.JSONBadRequest(c, "调整数量必须为正数")
		return
	}
	ctx := c.Request.Context()
	payload := map[string]interface{}{"product_id": id.String(), "type": adjType, "quantity": qty}
	if shared.AbortIfPluginRejected(c, shared.FireBefore(ctx, "stock.adjusting", payload)) {
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	afterStock, err := AdjustStock(ctx, tx, id, adjType, qty, ledger.Actor(c))
	if err != nil {
		if errors.Is(err, ErrInsufficientStock) {
			shared.JSONBadRequest(c, err.Error())
		} else {
			shared.JSONInternal(c, err)
		}
		return
	}
	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("stock.adjusted", payload)
	shared.JSONOK(c, gin.H{"ok": true, "current_stock": afterStock})
}

func (h *Handler) ProductSearchAPI(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), searchProductsSQL, c.Query("q"))
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var prods []models.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		prods = append(prods, p)
	}
	shared.JSONOK(c, shared.EmptySlice(prods))
}

func (h *Handler) ImportProducts(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		shared.JSONBadRequest(c, "请选择CSV文件")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		shared.JSONBadRequest(c, "CSV文件解析失败")
		return
	}

	if len(records) < 2 {
		shared.JSONBadRequest(c, "CSV文件为空或无数据行")
		return
	}

	ctx := c.Request.Context()
	imported, skipped := 0, 0
	for i, rec := range records[1:] {
		if len(rec) < 2 || strings.TrimSpace(rec[1]) == "" {
			skipped++
			continue
		}
		code := ""
		if len(rec) > 0 {
			code = strings.TrimSpace(rec[0])
		}
		name := strings.TrimSpace(rec[1])
		var category, unit, salePrice, costPrice string
		if len(rec) > 2 {
			category = strings.TrimSpace(rec[2])
		}
		if len(rec) > 3 {
			unit = strings.TrimSpace(rec[3])
		}
		if len(rec) > 4 {
			salePrice = strings.TrimSpace(rec[4])
		}
		if len(rec) > 5 {
			costPrice = strings.TrimSpace(rec[5])
		}
		if code == "" {
			var count int64
			if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count); err != nil {
				shared.JSONInternal(c, err)
				return
			}
			code = GenerateProductCode(count + int64(i))
		}
		row := h.db.QueryRowContext(ctx, createProductSQL,
			uuid.New(), code, name, category, unit, salePrice, costPrice, int32(0), "standard", int32(0),
		)
		if _, err := scanProduct(row); err != nil {
			skipped++
			continue
		}
		imported++
	}

	shared.JSONOK(c, gin.H{
		"imported": imported,
		"skipped":  skipped,
		"message":  fmt.Sprintf("导入完成：成功 %d 条，跳过 %d 条", imported, skipped),
	})
}

func ToInt32(v string) int32 {
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0
	}
	return int32(n)
}

func GenerateProductCode(count int64) string {
	return fmt.Sprintf("P%04d", count+1)
}

func (h *Handler) PriceTiersAPI(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid id")
		return
	}
	tiers, err := ListPriceTiers(c.Request.Context(), h.db, id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, shared.EmptySlice(tiers))
}

func (h *Handler) PriceTierCreate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid id")
		return
	}
	var in priceTierInput
	if !shared.BindJSON(c, &in) {
		return
	}
	price, err := shared.ParsePrice(in.UnitPrice)
	if err != nil {
		shared.JSONBadRequest(c, "单价格式无效")
		return
	}
	ctx := c.Request.Context()
	t := &models.PriceTier{
		ProductID:   id,
		MinQuantity: in.MinQuantity,
		MaxQuantity: in.MaxQuantity,
		UnitPrice:   price,
		CompanyID:   "default",
	}
	if t.MinQuantity <= 0 {
		t.MinQuantity = 1
	}
	if err := CreatePriceTier(ctx, h.db, t); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONCreated(c, t)
}

func (h *Handler) PriceTierDelete(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid id")
		return
	}
	tierID, err := uuid.Parse(c.Param("tierId"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid tier id")
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), `DELETE FROM product_price_tiers WHERE id = $1 AND product_id = $2`, tierID, productID)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ResolvePriceAPI(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "invalid product id")
		return
	}
	var customerID uuid.UUID
	if cid := c.Query("customer_id"); cid != "" {
		customerID, _ = uuid.Parse(cid)
	}
	qty := ToInt32(c.Query("quantity"))
	if qty <= 0 {
		qty = 1
	}
	var defaultPrice decimal.Decimal
	if err := h.db.QueryRowContext(c.Request.Context(), `SELECT COALESCE(sale_price, 0) FROM products WHERE id = $1`, productID).Scan(&defaultPrice); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	engine := NewPriceEngine(h.db)
	price := engine.ResolvePrice(c.Request.Context(), customerID, productID, qty, defaultPrice)
	res := shared.FireBefore(c.Request.Context(), "price.resolving", map[string]interface{}{
		"customer_id": customerID.String(),
		"product_id":  productID.String(),
		"quantity":    qty,
		"price":       price.StringFixed(2),
	})
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	if patched, ok := shared.PatchString(res, "price"); ok {
		if d, err := decimal.NewFromString(patched); err == nil {
			price = d
		}
	}
	shared.JSONOK(c, gin.H{"unit_price": price.StringFixed(2)})
}
