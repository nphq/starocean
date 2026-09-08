package web

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/view/pages"
)

const empCols = `e.id, e.code, e.name, e.department_id, e.position_id, COALESCE(e.phone,''), COALESCE(e.email,''), COALESCE(e.id_number,''),
	e.hire_date, e.separation_date, e.status, COALESCE(e.emergency_contact,''), COALESCE(e.emergency_phone,''),
	COALESCE(e.bank_name,''), COALESCE(e.bank_account,''), COALESCE(e.properties::text,'{}'),
	COALESCE(e.created_at,'1970-01-01'), COALESCE(e.updated_at,'1970-01-01'), COALESCE(e.company_id,'default')`

func scanEmpRow(row interface{ Scan(...any) error }) (models.Employee, error) {
	var e models.Employee
	var deptID, posID uuid.NullUUID
	var hire, sep sql.NullTime
	err := row.Scan(&e.ID, &e.Code, &e.Name, &deptID, &posID, &e.Phone, &e.Email, &e.IDNumber,
		&hire, &sep, &e.Status, &e.EmergencyContact, &e.EmergencyPhone,
		&e.BankName, &e.BankAccount, &e.Properties, &e.CreatedAt, &e.UpdatedAt, &e.CompanyID)
	if err != nil {
		return e, err
	}
	e.DepartmentID = nullUUID(deptID)
	e.PositionID = nullUUID(posID)
	e.HireDate = nullTime(hire)
	e.SeparationDate = nullTime(sep)
	return e, nil
}

func (h *Handler) EmployeesPage(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	var items []models.Employee
	var total int64
	if q != "" {
		rows, err := h.db.QueryContext(ctx, `SELECT `+empCols+` FROM employees e
			WHERE e.name ILIKE '%' || $1 || '%' OR e.code ILIKE '%' || $1 || '%' ORDER BY e.name LIMIT 100`, q)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if e, err := scanEmpRow(rows); err == nil {
				items = append(items, e)
			}
		}
		total = int64(len(items))
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT `+empCols+` FROM employees e
			ORDER BY e.created_at DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rows.Close()
		for rows.Next() {
			if e, err := scanEmpRow(rows); err == nil {
				items = append(items, e)
			}
		}
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM employees`).Scan(&total)
	}
	for i := range items {
		_ = h.db.QueryRowContext(ctx, "SELECT name FROM departments WHERE id=$1", items[i].DepartmentID).Scan(&items[i].DepartmentName)
	}
	if isHX(c) {
		renderFrag(c, pages.EmployeeListInner(items, q, page, total, limit))
		return
	}
	h.renderPage(c, "员工档案", pages.EmployeeList(items, q, page, total, limit))
}

func (h *Handler) EmployeeNewPage(c *gin.Context) {
	depts := h.departmentOptions(c.Request.Context())
	h.renderPage(c, "新增员工", pages.EmployeeForm(depts, ""))
}

func (h *Handler) departmentOptions(ctx context.Context) []models.Department {
	rows, err := h.db.QueryContext(ctx, `SELECT id, name, code, parent_id, COALESCE(manager_name,''), COALESCE(sort_order,0), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default') FROM departments ORDER BY sort_order, name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []models.Department
	for rows.Next() {
		var d models.Department
		var parentID uuid.NullUUID
		if err := rows.Scan(&d.ID, &d.Name, &d.Code, &parentID, &d.ManagerName, &d.SortOrder, &d.CreatedAt, &d.CompanyID); err == nil {
			d.ParentID = nullUUID(parentID)
			out = append(out, d)
		}
	}
	return out
}

func (h *Handler) EmployeeCreate(c *gin.Context) {
	ctx := c.Request.Context()
	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	fail := func(msg string) {
		h.renderPage(c, "新增员工", pages.EmployeeForm(h.departmentOptions(ctx), msg))
	}
	if name == "" || code == "" {
		fail("姓名与工号不能为空")
		return
	}
	var deptID, posID any
	if d := strings.TrimSpace(c.PostForm("department_id")); d != "" {
		if id, err := uuid.Parse(d); err == nil {
			deptID = id
		}
	}
	id := uuid.New()
	if _, err := h.db.ExecContext(ctx, `INSERT INTO employees (id, code, name, department_id, phone, status)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),'active')`,
		id, code, name, deptID, strings.TrimSpace(c.PostForm("phone"))); err != nil {
		_ = posID
		fail("保存失败：工号可能重复")
		return
	}
	redirect(c, "/personnel/employees/"+id.String())
}

func (h *Handler) EmployeeDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	emp, err := scanEmpRow(h.db.QueryRowContext(ctx, `SELECT `+empCols+` FROM employees e WHERE e.id=$1`, id))
	if err != nil {
		c.String(http.StatusNotFound, "员工不存在")
		return
	}
	_ = h.db.QueryRowContext(ctx, "SELECT name FROM departments WHERE id=$1", emp.DepartmentID).Scan(&emp.DepartmentName)
	h.renderPage(c, emp.Name, pages.EmployeeDetail(emp))
}

func (h *Handler) SalaryPage(c *gin.Context) {
	ctx := c.Request.Context()
	month := c.DefaultQuery("month", currentMonth())
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	rows, err := h.db.QueryContext(ctx, `SELECT sc.id, sc.employee_id, sc.month, sc.base_salary, sc.overtime_pay, sc.bonus, sc.deduction, sc.social_security, sc.housing_fund, sc.tax, sc.net_salary, sc.status, COALESCE(sc.created_at,'1970-01-01'), COALESCE(sc.updated_at,'1970-01-01'), COALESCE(sc.company_id,'default'), COALESCE(e.name,'')
		FROM salary_components sc LEFT JOIN employees e ON sc.employee_id = e.id
		WHERE TO_CHAR(sc.month,'YYYY-MM') = $1 ORDER BY e.name LIMIT $2 OFFSET $3`, month, limit, (page-1)*limit)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.SalaryComponent
	for rows.Next() {
		var s models.SalaryComponent
		if err := rows.Scan(&s.ID, &s.EmployeeID, &s.Month, &s.BaseSalary, &s.OvertimePay, &s.Bonus, &s.Deduction,
			&s.SocialSecurity, &s.HousingFund, &s.Tax, &s.NetSalary, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.CompanyID, &s.EmployeeName); err == nil {
			items = append(items, s)
		}
	}
	var total int64
	_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM salary_components WHERE TO_CHAR(month,'YYYY-MM') = $1", month).Scan(&total)
	h.renderPage(c, "薪资管理", pages.SalaryList(items, month, page, total, limit))
}

func (h *Handler) ContractsPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT c.id, c.employee_id, COALESCE(e.name,''), c.contract_no, c.contract_type, c.start_date, c.end_date, c.salary, c.status, COALESCE(c.created_at,'1970-01-01'), COALESCE(c.company_id,'default')
		FROM contracts c LEFT JOIN employees e ON c.employee_id = e.id ORDER BY c.created_at DESC LIMIT 100`)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.Contract
	for rows.Next() {
		var ct models.Contract
		var start, end sql.NullTime
		if err := rows.Scan(&ct.ID, &ct.EmployeeID, &ct.EmployeeName, &ct.ContractNo, &ct.ContractType,
			&start, &end, &ct.Salary, &ct.Status, &ct.CreatedAt, &ct.CompanyID); err == nil {
			ct.StartDate = nullTime(start)
			ct.EndDate = nullTime(end)
			items = append(items, ct)
		}
	}
	h.renderPage(c, "劳动合同", pages.ContractList(items))
}

func (h *Handler) DepartmentsPage(c *gin.Context) {
	depts := h.departmentOptions(c.Request.Context())
	h.renderPage(c, "组织架构", pages.DepartmentList(depts))
}
