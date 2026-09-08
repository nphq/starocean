package web

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/products"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/view/pages"
)

const productCols = `id, code, name, COALESCE(category,'') as category, COALESCE(unit,'') as unit,
       COALESCE(sale_price, 0) as sale_price, COALESCE(cost_price, 0) as cost_price,
       COALESCE(safety_stock,0) as safety_stock, COALESCE(current_stock,0) as current_stock,
       COALESCE(pricing_type, 'standard') as pricing_type,
       COALESCE(shelf_life_days,0) as shelf_life_days,
       COALESCE(created_at, '1970-01-01'::timestamptz) as created_at,
       COALESCE(company_id,'default') as company_id,
       COALESCE(properties::text,'{}') as properties`

func scanProductRow(row interface{ Scan(...any) error }) (models.Product, error) {
	var p models.Product
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit,
		&p.SalePrice, &p.CostPrice, &p.SafetyStock, &p.CurrentStock, &p.PricingType,
		&p.ShelfLifeDays, &p.CreatedAt, &p.CompanyID, &p.Properties)
	return p, err
}

func (h *Handler) ProductsPage(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	offset := (page - 1) * limit

	var items []models.Product
	var total int64
	if q != "" {
		rows, err := h.db.QueryContext(ctx, `SELECT `+productCols+` FROM products
			WHERE code ILIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%' ORDER BY name LIMIT 100`, q)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if p, err := scanProductRow(rows); err == nil {
				items = append(items, p)
			}
		}
		total = int64(len(items))
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT `+productCols+` FROM products
			ORDER BY created_at DESC NULLS LAST, id DESC LIMIT $1 OFFSET $2`, limit, offset)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if p, err := scanProductRow(rows); err == nil {
				items = append(items, p)
			}
		}
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&total)
	}
	if isHX(c) {
		renderFrag(c, pages.ProductListInner(items, q, page, total, limit))
		return
	}
	h.renderPage(c, "商品资料", pages.ProductList(items, q, page, total, limit))
}

func (h *Handler) ProductNewPage(c *gin.Context) {
	h.renderPage(c, "新增商品", pages.ProductForm(nil, ""))
}

func (h *Handler) ProductCreate(c *gin.Context) {
	in := productFormInput(c)
	if in.Name == "" {
		h.renderPage(c, "新增商品", pages.ProductForm(nil, "名称不能为空"))
		return
	}
	if _, err := shared.ParsePrice(nonEmpty(in.SalePrice, "0")); err != nil {
		h.renderPage(c, "新增商品", pages.ProductForm(nil, "售价格式无效"))
		return
	}
	if _, err := shared.ParsePrice(nonEmpty(in.CostPrice, "0")); err != nil {
		h.renderPage(c, "新增商品", pages.ProductForm(nil, "成本价格式无效"))
		return
	}
	var count int64
	_ = h.db.QueryRowContext(c.Request.Context(), "SELECT COUNT(*) FROM products").Scan(&count)
	code := in.Code
	if code == "" {
		code = "P" + strconv.FormatInt(count+1, 10)
	}
	var id uuid.UUID
	err := h.db.QueryRowContext(c.Request.Context(), `INSERT INTO products (id, code, name, category, unit, sale_price, cost_price, safety_stock, properties)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6::numeric, $7::numeric, $8, '{}'::jsonb) RETURNING id`,
		uuid.New(), code, in.Name, in.Category, in.Unit, nonEmpty(in.SalePrice, "0"), nonEmpty(in.CostPrice, "0"), in.SafetyStock).Scan(&id)
	if err != nil {
		h.renderPage(c, "新增商品", pages.ProductForm(nil, "保存失败：编码可能重复"))
		return
	}
	redirect(c, "/products/"+id.String())
}

type productForm struct {
	Code, Name, Category, Unit, SalePrice, CostPrice string
	SafetyStock                                      int32
}

func productFormInput(c *gin.Context) productForm {
	safety, _ := strconv.ParseInt(c.PostForm("safety_stock"), 10, 32)
	if safety < 0 {
		safety = 0
	}
	return productForm{
		Code: strings.TrimSpace(c.PostForm("code")), Name: strings.TrimSpace(c.PostForm("name")),
		Category: strings.TrimSpace(c.PostForm("category")), Unit: strings.TrimSpace(c.PostForm("unit")),
		SalePrice: strings.TrimSpace(c.PostForm("sale_price")), CostPrice: strings.TrimSpace(c.PostForm("cost_price")),
		SafetyStock: int32(safety),
	}
}

func nonEmpty(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (h *Handler) ProductDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	p, err := scanProductRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+productCols+` FROM products WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "商品不存在")
		return
	}
	h.renderPage(c, p.Name, pages.ProductDetail(p, ""))
}

func (h *Handler) ProductEditPage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	p, err := scanProductRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+productCols+` FROM products WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "商品不存在")
		return
	}
	h.renderPage(c, "编辑商品", pages.ProductForm(&p, ""))
}

func (h *Handler) ProductUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	in := productFormInput(c)
	if in.Name == "" {
		p, _ := scanProductRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+productCols+` FROM products WHERE id = $1`, id))
		h.renderPage(c, "编辑商品", pages.ProductForm(&p, "名称不能为空"))
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), `UPDATE products SET name=$2, category=NULLIF($3,''),
		unit=NULLIF($4,''), sale_price=$5::numeric, cost_price=$6::numeric, safety_stock=$7, updated_at=NOW() WHERE id=$1`,
		id, in.Name, in.Category, in.Unit, nonEmpty(in.SalePrice, "0"), nonEmpty(in.CostPrice, "0"), in.SafetyStock)
	if err != nil {
		p, _ := scanProductRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+productCols+` FROM products WHERE id = $1`, id))
		h.renderPage(c, "编辑商品", pages.ProductForm(&p, "保存失败"))
		return
	}
	redirect(c, "/products/"+id.String())
}

func (h *Handler) ProductDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	var refCount int64
	_ = h.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM (
		SELECT 1 FROM sales_order_items WHERE product_id = $1 UNION ALL
		SELECT 1 FROM purchase_order_items WHERE product_id = $1 UNION ALL
		SELECT 1 FROM inventory_movements WHERE product_id = $1 LIMIT 1) t`, id).Scan(&refCount)
	if refCount > 0 {
		h.renderPage(c, "商品资料", pages.ProductDetail(mustProduct(c, h.db, id), "该商品存在关联单据或库存流水，不能删除"))
		return
	}
	if _, err := h.db.ExecContext(c.Request.Context(), "DELETE FROM products WHERE id = $1", id); err != nil {
		c.String(http.StatusInternalServerError, "删除失败")
		return
	}
	redirect(c, "/products")
}

func mustProduct(c *gin.Context, db *sql.DB, id uuid.UUID) models.Product {
	p, _ := scanProductRow(db.QueryRowContext(c.Request.Context(), `SELECT `+productCols+` FROM products WHERE id = $1`, id))
	return p
}

func (h *Handler) ProductStockAdjust(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	qty, _ := strconv.ParseInt(c.PostForm("quantity"), 10, 32)
	adjType := c.PostForm("type")
	if adjType != "in" && adjType != "out" {
		adjType = "in"
	}
	ctx := c.Request.Context()
	msg := ""
	if qty <= 0 {
		msg = "调整数量必须为正数"
	} else {
		tx, err := h.db.BeginTx(ctx, nil)
		if err == nil {
			defer tx.Rollback()
			if _, err := products.AdjustStock(ctx, tx, id, adjType, qty, "web"); err != nil {
				msg = err.Error()
			} else if err := tx.Commit(); err != nil {
				msg = "保存失败"
			}
		} else {
			msg = "保存失败"
		}
	}
	if msg != "" {
		h.renderPage(c, "商品资料", pages.ProductDetail(mustProduct(c, h.db, id), msg))
		return
	}
	redirect(c, "/products/"+id.String())
}

// productOptions 商品下拉选项（限 500，创建时间倒序）。
func (h *Handler) productOptions(ctx context.Context) []models.Product {
	rows, err := h.db.QueryContext(ctx, `SELECT id, code, name, '', '', 0, 0, 0, 0, 'standard', 0,
		'1970-01-01', 'default', '{}' FROM products ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []models.Product
	for rows.Next() {
		var p models.Product
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Category, &p.Unit, &p.SalePrice,
			&p.CostPrice, &p.SafetyStock, &p.CurrentStock, &p.PricingType, &p.ShelfLifeDays,
			&p.CreatedAt, &p.CompanyID, &p.Properties); err == nil {
			out = append(out, p)
		}
	}
	return out
}
