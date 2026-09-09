package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/view/pages"
)

const supplierCols = `id, code, name, COALESCE(contact_person,'') as contact_person, COALESCE(phone,'') as phone,
       COALESCE(email,'') as email, COALESCE(address,'') as address,
       COALESCE(balance, 0) as balance,
       COALESCE(rating, 0) as rating, COALESCE(on_time_rate, 0) as on_time_rate, COALESCE(quality_rate, 0) as quality_rate,
       COALESCE(created_at, '1970-01-01') as created_at,
       COALESCE(company_id,'default') as company_id,
       COALESCE(properties,'{}') as properties`

func scanSupplierRow(row interface{ Scan(...any) error }) (models.Supplier, error) {
	var s models.Supplier
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.ContactPerson, &s.Phone,
		&s.Email, &s.Address, &s.Balance, &s.Rating, &s.OnTimeRate, &s.QualityRate,
		&s.CreatedAt, &s.CompanyID, &s.Properties)
	return s, err
}

func (h *Handler) SuppliersPage(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	var items []models.Supplier
	var total int64
	if q != "" {
		rows, err := h.db.QueryContext(ctx, `SELECT `+supplierCols+` FROM suppliers
			WHERE name LIKE '%' || $1 || '%' OR code LIKE '%' || $1 || '%' ORDER BY name LIMIT 100`, q)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if s, err := scanSupplierRow(rows); err == nil {
				items = append(items, s)
			}
		}
		total = int64(len(items))
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT `+supplierCols+` FROM suppliers ORDER BY created_at DESC NULLS LAST LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if s, err := scanSupplierRow(rows); err == nil {
				items = append(items, s)
			}
		}
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppliers`).Scan(&total)
	}
	if isHX(c) {
		renderFrag(c, pages.SupplierListInner(items, q, page, total, limit))
		return
	}
	h.renderPage(c, "供应商", pages.SupplierList(items, q, page, total, limit))
}

func (h *Handler) SupplierNewPage(c *gin.Context) {
	h.renderPage(c, "新增供应商", pages.SupplierForm(nil, ""))
}

func (h *Handler) SupplierCreate(c *gin.Context) {
	ctx := c.Request.Context()
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		h.renderPage(c, "新增供应商", pages.SupplierForm(nil, "名称不能为空"))
		return
	}
	var count int64
	_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM suppliers").Scan(&count)
	code := strings.TrimSpace(c.PostForm("code"))
	if code == "" {
		code = "S" + strconv.FormatInt(count+1, 10)
	}
	var id uuid.UUID
	err := h.db.QueryRowContext(ctx, `INSERT INTO suppliers (id, code, name, contact_person, phone, email, address, properties)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''), '{}') RETURNING id`,
		uuid.New(), code, name, strings.TrimSpace(c.PostForm("contact_person")), strings.TrimSpace(c.PostForm("phone")),
		strings.TrimSpace(c.PostForm("email")), strings.TrimSpace(c.PostForm("address"))).Scan(&id)
	if err != nil {
		h.renderPage(c, "新增供应商", pages.SupplierForm(nil, "保存失败：编码可能重复"))
		return
	}
	redirect(c, "/suppliers/"+id.String())
}

func (h *Handler) SupplierDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	s, err := scanSupplierRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+supplierCols+` FROM suppliers WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "供应商不存在")
		return
	}
	var orders []models.PurchaseOrder
	rows, _ := h.db.QueryContext(c.Request.Context(), `SELECT id, order_no, COALESCE(supplier_id, '00000000-0000-0000-0000-000000000000'), '',
		status, COALESCE(total_amount,0), COALESCE(paid_amount,0), COALESCE(order_date,'1970-01-01'), COALESCE(delivery_date,'1970-01-01'),
		COALESCE(notes,''), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM purchase_orders WHERE supplier_id = $1 ORDER BY created_at DESC LIMIT 20`, id)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var o models.PurchaseOrder
			if err := rows.Scan(&o.ID, &o.OrderNo, &o.SupplierID, &o.SupplierName, &o.Status, &o.TotalAmount,
				&o.PaidAmount, &o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err == nil {
				orders = append(orders, o)
			}
		}
	}
	h.renderPage(c, s.Name, pages.SupplierDetail(s, orders, ""))
}

func (h *Handler) SupplierEditPage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	s, err := scanSupplierRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+supplierCols+` FROM suppliers WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "供应商不存在")
		return
	}
	h.renderPage(c, "编辑供应商", pages.SupplierForm(&s, ""))
}

func (h *Handler) SupplierUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		s, _ := scanSupplierRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+supplierCols+` FROM suppliers WHERE id = $1`, id))
		h.renderPage(c, "编辑供应商", pages.SupplierForm(&s, "名称不能为空"))
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), `UPDATE suppliers SET name=$2, contact_person=NULLIF($3,''),
		phone=NULLIF($4,''), email=NULLIF($5,''), address=NULLIF($6,''), updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$1`,
		id, name, strings.TrimSpace(c.PostForm("contact_person")), strings.TrimSpace(c.PostForm("phone")),
		strings.TrimSpace(c.PostForm("email")), strings.TrimSpace(c.PostForm("address")))
	if err != nil {
		s, _ := scanSupplierRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+supplierCols+` FROM suppliers WHERE id = $1`, id))
		h.renderPage(c, "编辑供应商", pages.SupplierForm(&s, "保存失败"))
		return
	}
	redirect(c, "/suppliers/"+id.String())
}

func (h *Handler) SupplierDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	var refCount int64
	_ = h.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM purchase_orders WHERE supplier_id = $1`, id).Scan(&refCount)
	if refCount > 0 {
		s, _ := scanSupplierRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+supplierCols+` FROM suppliers WHERE id = $1`, id))
		h.renderPage(c, s.Name, pages.SupplierDetail(s, nil, "该供应商存在关联采购订单，不能删除"))
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `DELETE FROM suppliers WHERE id = $1`, id)
	redirect(c, "/suppliers")
}
