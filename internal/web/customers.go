package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
	"github.com/nphq/starocean/view/pages"
)

const customerCols = `id, code, name, COALESCE(contact_person,'') as contact_person, COALESCE(phone,'') as phone,
       COALESCE(email,'') as email, COALESCE(address,'') as address,
       COALESCE(credit_limit, 0) as credit_limit, COALESCE(balance, 0) as balance,
       COALESCE(tier, 'normal') as tier, COALESCE(sales_person,'') as sales_person,
       COALESCE(created_at, '1970-01-01') as created_at,
       COALESCE(company_id,'default') as company_id,
       COALESCE(properties,'{}') as properties`

func scanCustomerRow(row interface{ Scan(...any) error }) (models.Customer, error) {
	var c models.Customer
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.ContactPerson, &c.Phone,
		&c.Email, &c.Address, &c.CreditLimit, &c.Balance, &c.Tier, &c.SalesPerson,
		&c.CreatedAt, &c.CompanyID, &c.Properties)
	return c, err
}

func (h *Handler) CustomersPage(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	var items []models.Customer
	var total int64
	if q != "" {
		rows, err := h.db.QueryContext(ctx, `SELECT `+customerCols+` FROM customers
			WHERE name LIKE '%' || $1 || '%' OR code LIKE '%' || $1 || '%' OR phone LIKE '%' || $1 || '%' ORDER BY name LIMIT 100`, q)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if cu, err := scanCustomerRow(rows); err == nil {
				items = append(items, cu)
			}
		}
		total = int64(len(items))
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT `+customerCols+` FROM customers ORDER BY created_at DESC NULLS LAST LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if cu, err := scanCustomerRow(rows); err == nil {
				items = append(items, cu)
			}
		}
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&total)
	}
	if isHX(c) {
		renderFrag(c, pages.CustomerListInner(items, q, page, total, limit))
		return
	}
	h.renderPage(c, "客户管理", pages.CustomerList(items, q, page, total, limit))
}

func (h *Handler) CustomerNewPage(c *gin.Context) {
	h.renderPage(c, "新增客户", pages.CustomerForm(nil, ""))
}

func (h *Handler) CustomerCreate(c *gin.Context) {
	ctx := c.Request.Context()
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		h.renderPage(c, "新增客户", pages.CustomerForm(nil, "名称不能为空"))
		return
	}
	var count int64
	_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM customers").Scan(&count)
	code := strings.TrimSpace(c.PostForm("code"))
	if code == "" {
		code = "C" + strconv.FormatInt(count+1, 10)
	}
	credit := strings.TrimSpace(c.PostForm("credit_limit"))
	if credit == "" {
		credit = "0"
	}
	if _, err := shared.ParsePrice(credit); err != nil {
		h.renderPage(c, "新增客户", pages.CustomerForm(nil, "信用额度格式无效"))
		return
	}
	var id uuid.UUID
	err := h.db.QueryRowContext(ctx, `INSERT INTO customers (id, code, name, contact_person, phone, email, address, credit_limit, properties)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''), CAST($8 AS NUMERIC), '{}') RETURNING id`,
		uuid.New(), code, name, strings.TrimSpace(c.PostForm("contact_person")), strings.TrimSpace(c.PostForm("phone")),
		strings.TrimSpace(c.PostForm("email")), strings.TrimSpace(c.PostForm("address")), credit).Scan(&id)
	if err != nil {
		h.renderPage(c, "新增客户", pages.CustomerForm(nil, "保存失败：编码可能重复"))
		return
	}
	redirect(c, "/customers/"+id.String())
}

func (h *Handler) CustomerDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	cu, err := scanCustomerRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+customerCols+` FROM customers WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "客户不存在")
		return
	}
	var orders []models.SalesOrder
	rows, _ := h.db.QueryContext(c.Request.Context(), `SELECT id, order_no, COALESCE(customer_id, '00000000-0000-0000-0000-000000000000'), '',
		status, COALESCE(total_amount,0), COALESCE(paid_amount,0), COALESCE(order_date,'1970-01-01'), COALESCE(delivery_date,'1970-01-01'),
		COALESCE(notes,''), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default'), COALESCE(properties,'{}')
		FROM sales_orders WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 20`, id)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var o models.SalesOrder
			if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName, &o.Status, &o.TotalAmount,
				&o.PaidAmount, &o.OrderDate, &o.DeliveryDate, &o.Notes, &o.CreatedAt, &o.CompanyID, &o.Properties); err == nil {
				orders = append(orders, o)
			}
		}
	}
	h.renderPage(c, cu.Name, pages.CustomerDetail(cu, orders, ""))
}

func (h *Handler) CustomerEditPage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	cu, err := scanCustomerRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+customerCols+` FROM customers WHERE id = $1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "客户不存在")
		return
	}
	h.renderPage(c, "编辑客户", pages.CustomerForm(&cu, ""))
}

func (h *Handler) CustomerUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		cu, _ := scanCustomerRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+customerCols+` FROM customers WHERE id = $1`, id))
		h.renderPage(c, "编辑客户", pages.CustomerForm(&cu, "名称不能为空"))
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), `UPDATE customers SET name=$2, contact_person=NULLIF($3,''),
		phone=NULLIF($4,''), email=NULLIF($5,''), address=NULLIF($6,''), updated_at=(strftime('%Y-%m-%dT%H:%M:%SZ','now')) WHERE id=$1`,
		id, name, strings.TrimSpace(c.PostForm("contact_person")), strings.TrimSpace(c.PostForm("phone")),
		strings.TrimSpace(c.PostForm("email")), strings.TrimSpace(c.PostForm("address")))
	if err != nil {
		cu, _ := scanCustomerRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+customerCols+` FROM customers WHERE id = $1`, id))
		h.renderPage(c, "编辑客户", pages.CustomerForm(&cu, "保存失败"))
		return
	}
	redirect(c, "/customers/"+id.String())
}

func (h *Handler) CustomerDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	var refCount int64
	_ = h.db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*) FROM sales_orders WHERE customer_id = $1`, id).Scan(&refCount)
	if refCount > 0 {
		cu, _ := scanCustomerRow(h.db.QueryRowContext(c.Request.Context(), `SELECT `+customerCols+` FROM customers WHERE id = $1`, id))
		h.renderPage(c, cu.Name, pages.CustomerDetail(cu, nil, "该客户存在关联销售订单，不能删除"))
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `DELETE FROM customers WHERE id = $1`, id)
	redirect(c, "/customers")
}
