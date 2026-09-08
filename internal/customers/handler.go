package customers

import (
	"database/sql"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/partnernotes"
	"github.com/nphq/starocean/internal/shared"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

const customerCols = `id, code, name, COALESCE(contact_person,'') as contact_person, COALESCE(phone,'') as phone,
       COALESCE(email,'') as email, COALESCE(address,'') as address,
       COALESCE(credit_limit, 0) as credit_limit, COALESCE(balance, 0) as balance,
       COALESCE(tier, 'normal') as tier, COALESCE(sales_person,'') as sales_person,
       COALESCE(created_at, '1970-01-01'::timestamptz) as created_at,
       COALESCE(company_id,'default') as company_id,
       COALESCE(properties::text,'{}') as properties`

const listCustomersSQL = `SELECT ` + customerCols + ` FROM customers ORDER BY created_at DESC NULLS LAST LIMIT $1 OFFSET $2`

const getCustomerSQL = `SELECT ` + customerCols + ` FROM customers WHERE id = $1`

const searchCustomersSQL = `SELECT ` + customerCols + ` FROM customers
WHERE name ILIKE '%' || $1 || '%' OR code ILIKE '%' || $1 || '%' OR phone ILIKE '%' || $1 || '%'
ORDER BY name LIMIT 20`

const createCustomerSQL = `INSERT INTO customers (id, code, name, contact_person, phone, email, address, credit_limit, tier, sales_person, properties)
VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''), NULLIF($8,'')::numeric, COALESCE(NULLIF($9,''), 'normal'), NULLIF($10,''), $11::jsonb)
RETURNING ` + customerCols

const updateCustomerSQL = `UPDATE customers SET name = $2, contact_person = NULLIF($3,''), phone = NULLIF($4,''),
       email = NULLIF($5,''), address = NULLIF($6,''), credit_limit = NULLIF($7,'')::numeric,
       tier = COALESCE(NULLIF($8,''), tier), sales_person = NULLIF($9,''),
       properties = properties || $10::jsonb, updated_at = NOW()
WHERE id = $1
RETURNING ` + customerCols

type customerInput struct {
	Code          string                 `json:"code"`
	Name          string                 `json:"name"`
	ContactPerson string                 `json:"contact_person"`
	Phone         string                 `json:"phone"`
	Email         string                 `json:"email"`
	Address       string                 `json:"address"`
	CreditLimit   string                 `json:"credit_limit"`
	Tier          string                 `json:"tier"`
	SalesPerson   string                 `json:"sales_person"`
	Properties    map[string]interface{} `json:"properties"`
}

type customerDetail struct {
	models.Customer
	Notes []models.PartnerNote `json:"notes"`
}

func scanCustomer(row interface{ Scan(...interface{}) error }) (models.Customer, error) {
	var c models.Customer
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.ContactPerson, &c.Phone,
		&c.Email, &c.Address, &c.CreditLimit, &c.Balance, &c.Tier, &c.SalesPerson,
		&c.CreatedAt, &c.CompanyID, &c.Properties)
	return c, err
}

func (h *Handler) CustomersPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	rows, err := h.db.QueryContext(ctx, listCustomersSQL, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var custs []models.Customer
	for rows.Next() {
		cust, err := scanCustomer(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		custs = append(custs, cust)
	}
	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM customers").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, shared.PageResult[models.Customer]{Items: shared.EmptySlice(custs), Total: count, Page: page})
}

func (h *Handler) CustomerCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in customerInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if in.Name == "" {
		shared.JSONBadRequest(c, "名称不能为空")
		return
	}
	res := shared.FireBefore(ctx, "customer.creating", map[string]interface{}{"name": in.Name, "properties": in.Properties})
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	props := shared.PropertiesFromPatch(res, in.Properties)
	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM customers").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	code := fmt.Sprintf("C%04d", count+1)
	if in.Code != "" {
		code = in.Code
	}
	row := h.db.QueryRowContext(ctx, createCustomerSQL,
		uuid.New(), code, in.Name,
		in.ContactPerson, in.Phone, in.Email, in.Address,
		in.CreditLimit, in.Tier, in.SalesPerson, shared.JSONMapArg(props),
	)
	cust, err := scanCustomer(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("customer.created", map[string]interface{}{"id": cust.ID.String()})
	shared.JSONCreated(c, cust)
}

func (h *Handler) CustomerDetailPage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	row := h.db.QueryRowContext(ctx, getCustomerSQL, id)
	customer, err := scanCustomer(row)
	if err != nil {
		shared.JSONNotFound(c, "客户不存在")
		return
	}

	notes, _ := partnernotes.GetNotesByPartner(ctx, h.db, "customer", id)
	shared.JSONOK(c, customerDetail{Customer: customer, Notes: shared.EmptySlice(notes)})
}

func (h *Handler) CustomerUpdate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	var in customerInput
	if !shared.BindJSON(c, &in) {
		return
	}
	ctx := c.Request.Context()
	res := shared.FireBefore(ctx, "customer.updating", map[string]interface{}{"id": id.String(), "name": in.Name, "properties": in.Properties})
	if shared.AbortIfPluginRejected(c, res) {
		return
	}
	row := h.db.QueryRowContext(ctx, updateCustomerSQL,
		id, in.Name, in.ContactPerson, in.Phone, in.Email, in.Address,
		in.CreditLimit, in.Tier, in.SalesPerson, shared.JSONMapArg(shared.PropertiesFromPatch(res, in.Properties)),
	)
	cust, err := scanCustomer(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.FireAfter("customer.updated", map[string]interface{}{"id": id.String()})
	shared.JSONOK(c, cust)
}

func (h *Handler) CustomerDelete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	var refCount int64
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE customer_id = $1`, id).Scan(&refCount); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	if refCount > 0 {
		shared.JSONBadRequest(c, "该客户存在关联销售订单，不能删除")
		return
	}
	_, err = h.db.ExecContext(ctx, "DELETE FROM customers WHERE id = $1", id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) CustomerSearchAPI(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), searchCustomersSQL, c.Query("q"))
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()
	var custs []models.Customer
	for rows.Next() {
		cust, err := scanCustomer(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		custs = append(custs, cust)
	}
	shared.JSONOK(c, shared.EmptySlice(custs))
}
