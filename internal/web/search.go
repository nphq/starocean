package web

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nphq/starocean/view/pages"
)

// SearchPage 全局搜索结果页（顶栏表单 GET /search?q=）。
func (h *Handler) SearchPage(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	var res pages.SearchResults
	res.Query = q
	if q == "" {
		h.renderPage(c, "搜索", pages.Search(res))
		return
	}
	ctx := c.Request.Context()
	like := "%" + q + "%"

	rows, _ := h.db.QueryContext(ctx, `SELECT id, code, name FROM products WHERE code LIKE $1 OR name LIKE $1 ORDER BY name LIMIT 10`, like)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id, code, name string
			if err := rows.Scan(&id, &code, &name); err == nil {
				res.Products = append(res.Products, pages.SearchHit{ID: id, Title: name, Sub: code})
			}
		}
	}
	rows, _ = h.db.QueryContext(ctx, `SELECT id, code, name FROM customers WHERE code LIKE $1 OR name LIKE $1 ORDER BY name LIMIT 10`, like)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id, code, name string
			if err := rows.Scan(&id, &code, &name); err == nil {
				res.Customers = append(res.Customers, pages.SearchHit{ID: id, Title: name, Sub: code})
			}
		}
	}
	rows, _ = h.db.QueryContext(ctx, `SELECT id, code, name FROM suppliers WHERE code LIKE $1 OR name LIKE $1 ORDER BY name LIMIT 10`, like)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id, code, name string
			if err := rows.Scan(&id, &code, &name); err == nil {
				res.Suppliers = append(res.Suppliers, pages.SearchHit{ID: id, Title: name, Sub: code})
			}
		}
	}
	rows, _ = h.db.QueryContext(ctx, `SELECT so.id, so.order_no, COALESCE(c.name,'') FROM sales_orders so
		LEFT JOIN customers c ON so.customer_id = c.id
		WHERE so.order_no LIKE $1 OR c.name LIKE $1 ORDER BY so.created_at DESC LIMIT 10`, like)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id, no, cname string
			if err := rows.Scan(&id, &no, &cname); err == nil {
				res.Sales = append(res.Sales, pages.SearchHit{ID: id, Title: no, Sub: cname})
			}
		}
	}
	rows, _ = h.db.QueryContext(ctx, `SELECT po.id, po.order_no, COALESCE(s.name,'') FROM purchase_orders po
		LEFT JOIN suppliers s ON po.supplier_id = s.id
		WHERE po.order_no LIKE $1 OR s.name LIKE $1 ORDER BY po.created_at DESC LIMIT 10`, like)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id, no, sname string
			if err := rows.Scan(&id, &no, &sname); err == nil {
				res.Purchases = append(res.Purchases, pages.SearchHit{ID: id, Title: no, Sub: sname})
			}
		}
	}
	h.renderPage(c, "搜索", pages.Search(res))
}
