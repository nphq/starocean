package suppliers

import (
	"database/sql"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

const supplierCols = `id, code, name, COALESCE(contact_person,'') as contact_person, COALESCE(phone,'') as phone,
       COALESCE(email,'') as email, COALESCE(address,'') as address,
       COALESCE(balance, 0) as balance,
       COALESCE(rating, 0) as rating, COALESCE(on_time_rate, 0) as on_time_rate, COALESCE(quality_rate, 0) as quality_rate,
       COALESCE(created_at, '1970-01-01') as created_at,
       COALESCE(company_id,'default') as company_id,
       COALESCE(properties,'{}') as properties`

const listSuppliersSQL = `SELECT ` + supplierCols + ` FROM suppliers ORDER BY created_at DESC NULLS LAST LIMIT $1 OFFSET $2`

const getSupplierSQL = `SELECT ` + supplierCols + ` FROM suppliers WHERE id = $1`

const searchSuppliersSQL = `SELECT ` + supplierCols + ` FROM suppliers
WHERE name LIKE '%' || $1 || '%' OR code LIKE '%' || $1 || '%'
ORDER BY name LIMIT 20`

const createSupplierSQL = `INSERT INTO suppliers (id, code, name, contact_person, phone, email, address, properties)
VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''), $8)
RETURNING ` + supplierCols

const updateSupplierSQL = `UPDATE suppliers SET name = $2, contact_person = NULLIF($3,''), phone = NULLIF($4,''),
       email = NULLIF($5,''), address = NULLIF($6,''),
       rating = COALESCE(CAST(NULLIF($7,'') AS NUMERIC), rating), on_time_rate = COALESCE(CAST(NULLIF($8,'') AS NUMERIC), on_time_rate),
       quality_rate = COALESCE(CAST(NULLIF($9,'') AS NUMERIC), quality_rate),
       properties = json_patch(properties, $10), updated_at = (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
WHERE id = $1
RETURNING ` + supplierCols

type supplierInput struct {
	Code          string                 `json:"code"`
	Name          string                 `json:"name"`
	ContactPerson string                 `json:"contact_person"`
	Phone         string                 `json:"phone"`
	Email         string                 `json:"email"`
	Address       string                 `json:"address"`
	Rating        string                 `json:"rating"`
	OnTimeRate    string                 `json:"on_time_rate"`
	QualityRate   string                 `json:"quality_rate"`
	Properties    map[string]interface{} `json:"properties"`
}

func scanSupplier(row interface{ Scan(...interface{}) error }) (models.Supplier, error) {
	var s models.Supplier
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.ContactPerson, &s.Phone,
		&s.Email, &s.Address, &s.Balance, &s.Rating, &s.OnTimeRate, &s.QualityRate,
		&s.CreatedAt, &s.CompanyID, &s.Properties)
	return s, err
}

func (h *Handler) SuppliersPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	rows, err := h.db.QueryContext(ctx, listSuppliersSQL, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var sups []models.Supplier
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		sups = append(sups, s)
	}
	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM suppliers").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, shared.PageResult[models.Supplier]{Items: shared.EmptySlice(sups), Total: count, Page: page})
}

func (h *Handler) SupplierCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in supplierInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if in.Name == "" {
		shared.JSONBadRequest(c, "名称不能为空")
		return
	}
	res := shared.FireBefore(ctx, "supplier.creating", map[string]interface{}{"name": in.Name, "properties": in.Properties})
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM suppliers").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	code := fmt.Sprintf("S%04d", count+1)
	if in.Code != "" {
		code = in.Code
	}
	row := h.db.QueryRowContext(ctx, createSupplierSQL,
		uuid.New(), code, in.Name,
		in.ContactPerson, in.Phone, in.Email, in.Address,
		shared.JSONMapArg(shared.PropertiesFromPatch(res, in.Properties)),
	)
	s, err := scanSupplier(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("supplier.created", map[string]interface{}{"id": s.ID.String()})
	shared.JSONCreated(c, s)
}

func (h *Handler) SupplierDetailPage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	row := h.db.QueryRowContext(ctx, getSupplierSQL, id)
	supplier, err := scanSupplier(row)
	if err != nil {
		shared.JSONNotFound(c, "供应商不存在")
		return
	}

	shared.JSONOK(c, supplier)
}

func (h *Handler) SupplierUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	var in supplierInput
	if !shared.BindJSON(c, &in) {
		return
	}
	ctx := c.Request.Context()
	res := shared.FireBefore(ctx, "supplier.updating", map[string]interface{}{"id": id.String(), "name": in.Name, "properties": in.Properties})
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	row := h.db.QueryRowContext(ctx, updateSupplierSQL,
		id, in.Name, in.ContactPerson, in.Phone, in.Email, in.Address,
		in.Rating, in.OnTimeRate, in.QualityRate, shared.JSONMapArg(shared.PropertiesFromPatch(res, in.Properties)),
	)
	s, err := scanSupplier(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("supplier.updated", map[string]interface{}{"id": id.String()})
	shared.JSONOK(c, s)
}

func (h *Handler) SupplierDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	var refCount int64
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchase_orders WHERE supplier_id = $1`, id).Scan(&refCount); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	if refCount > 0 {
		shared.JSONBadRequest(c, "该供应商存在关联采购订单，不能删除")
		return
	}
	_, err = h.db.ExecContext(ctx, "DELETE FROM suppliers WHERE id = $1", id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) SupplierSearchAPI(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), searchSuppliersSQL, c.Query("q"))
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var sups []models.Supplier
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		sups = append(sups, s)
	}
	shared.JSONOK(c, shared.EmptySlice(sups))
}
