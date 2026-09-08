package personnel

import (
	"database/sql"
	"fmt"
	"hash/fnv"
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

type departmentInput struct {
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	ParentID    uuid.UUID `json:"parent_id"`
	ManagerName string    `json:"manager_name"`
	SortOrder   int32     `json:"sort_order"`
}

type positionInput struct {
	Name         string    `json:"name"`
	DepartmentID uuid.UUID `json:"department_id"`
	BaseSalary   string    `json:"base_salary"`
}

type employeeInput struct {
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	DepartmentID     uuid.UUID `json:"department_id"`
	PositionID       uuid.UUID `json:"position_id"`
	Phone            string    `json:"phone"`
	Email            string    `json:"email"`
	IDNumber         string    `json:"id_number"`
	HireDate         string    `json:"hire_date"`
	SeparationDate   string    `json:"separation_date"`
	Status           string    `json:"status"`
	EmergencyContact string    `json:"emergency_contact"`
	EmergencyPhone   string    `json:"emergency_phone"`
	BankName         string    `json:"bank_name"`
	BankAccount      string    `json:"bank_account"`
}

type contractInput struct {
	EmployeeID   uuid.UUID `json:"employee_id"`
	ContractNo   string    `json:"contract_no"`
	ContractType string    `json:"contract_type"`
	StartDate    string    `json:"start_date"`
	EndDate      string    `json:"end_date"`
	Salary       string    `json:"salary"`
}

type salaryRowInput struct {
	EmployeeID     uuid.UUID `json:"employee_id"`
	BaseSalary     string    `json:"base_salary"`
	OvertimePay    string    `json:"overtime_pay"`
	Bonus          string    `json:"bonus"`
	Deduction      string    `json:"deduction"`
	SocialSecurity string    `json:"social_security"`
	HousingFund    string    `json:"housing_fund"`
	Tax            string    `json:"tax"`
}

type salaryBatchInput struct {
	Month    string           `json:"month"`
	Salaries []salaryRowInput `json:"salaries"`
}

type employeeDetail struct {
	models.Employee
	Contracts []models.Contract        `json:"contracts"`
	Salaries  []models.SalaryComponent `json:"salaries"`
}

func optionalUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func parseOptionalDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &d
}

func (h *Handler) DepartmentsPage(c *gin.Context) {
	ctx := c.Request.Context()

	rows, err := h.db.QueryContext(ctx, `SELECT id, name, code, parent_id, COALESCE(manager_name,''), COALESCE(sort_order,0), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM departments ORDER BY sort_order, name`)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var depts []models.Department
	for rows.Next() {
		var d models.Department
		if err := rows.Scan(&d.ID, &d.Name, &d.Code, &d.ParentID, &d.ManagerName, &d.SortOrder, &d.CreatedAt, &d.CompanyID); err != nil {
			continue
		}
		depts = append(depts, d)
	}

	shared.JSONOK(c, shared.PageResult[models.Department]{Items: shared.EmptySlice(depts), Total: int64(len(depts)), Page: 1})
}

func (h *Handler) DepartmentCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in departmentInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.Name == "" || in.Code == "" {
		shared.JSONBadRequest(c, "请填写名称和编码")
		return
	}

	id := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO departments (id, name, code, parent_id, manager_name, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		id, in.Name, in.Code, optionalUUID(in.ParentID), in.ManagerName, in.SortOrder)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) DepartmentUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in departmentInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE departments SET name=$1, code=$2, parent_id=$3, manager_name=$4, sort_order=$5 WHERE id=$6`,
		in.Name, in.Code, optionalUUID(in.ParentID), in.ManagerName, in.SortOrder, id); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) PositionsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)

	rows, err := h.db.QueryContext(ctx, `SELECT id, name, department_id, base_salary, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM positions ORDER BY name LIMIT $1 OFFSET $2`, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var positions []models.Position
	for rows.Next() {
		var p models.Position
		if err := rows.Scan(&p.ID, &p.Name, &p.DepartmentID, &p.BaseSalary, &p.CreatedAt, &p.CompanyID); err != nil {
			continue
		}
		positions = append(positions, p)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM positions").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.Position]{Items: shared.EmptySlice(positions), Total: count, Page: page})
}

func (h *Handler) PositionCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in positionInput
	if !shared.BindJSON(c, &in) {
		return
	}
	if in.Name == "" {
		shared.JSONBadRequest(c, "请填写岗位名称")
		return
	}

	id := uuid.New()
	if _, err := h.db.ExecContext(ctx, "INSERT INTO positions (id, name, department_id, base_salary) VALUES ($1,$2,$3,$4)",
		id, in.Name, optionalUUID(in.DepartmentID), in.BaseSalary); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) PositionUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in positionInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if _, err := h.db.ExecContext(ctx, "UPDATE positions SET name=$1, department_id=$2, base_salary=$3 WHERE id=$4",
		in.Name, optionalUUID(in.DepartmentID), in.BaseSalary, id); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) PositionDelete(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	if _, err := h.db.ExecContext(ctx, "DELETE FROM positions WHERE id=$1", id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) EmployeesPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	tab := c.DefaultQuery("status", "all")
	deptFilter := c.Query("department_id")

	where := "1=1"
	args := []interface{}{}
	argIdx := 1

	if tab != "all" {
		where = fmt.Sprintf("status = $%d", argIdx)
		args = append(args, tab)
		argIdx++
	}
	if deptFilter != "" {
		where += fmt.Sprintf(" AND department_id = $%d::uuid", argIdx)
		args = append(args, deptFilter)
		argIdx++
	}

	queryArgs := append(args, 20, (page-1)*20)
	rows, err := h.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT e.id, e.code, e.name, e.department_id, e.position_id, COALESCE(e.phone,''), COALESCE(e.email,''), COALESCE(e.id_number,''),
			e.hire_date, e.separation_date, e.status, COALESCE(e.emergency_contact,''), COALESCE(e.emergency_phone,''),
			COALESCE(e.bank_name,''), COALESCE(e.bank_account,''), COALESCE(e.properties::text,'{}'),
			COALESCE(e.created_at,'1970-01-01'), COALESCE(e.updated_at,'1970-01-01'), COALESCE(e.company_id,'default')
			FROM employees e WHERE %s ORDER BY e.created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1),
		queryArgs...)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var employees []models.Employee
	for rows.Next() {
		var e models.Employee
		if err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.DepartmentID, &e.PositionID, &e.Phone, &e.Email, &e.IDNumber,
			&e.HireDate, &e.SeparationDate, &e.Status, &e.EmergencyContact, &e.EmergencyPhone,
			&e.BankName, &e.BankAccount, &e.Properties, &e.CreatedAt, &e.UpdatedAt, &e.CompanyID); err != nil {
			continue
		}
		employees = append(employees, e)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM employees WHERE %s", where), args...).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.Employee]{Items: shared.EmptySlice(employees), Total: count, Page: page})
}

func (h *Handler) EmployeeCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in employeeInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.Name == "" {
		shared.JSONBadRequest(c, "请填写姓名")
		return
	}

	code := in.Code
	if code == "" {
		code = fmt.Sprintf("EMP-%08x", hashName(in.Name))
	}

	status := in.Status
	if status == "" {
		status = "active"
	}

	id := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO employees (id, code, name, department_id, position_id, phone, email, id_number, hire_date, separation_date, status, emergency_contact, emergency_phone, bank_name, bank_account)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		id, code, in.Name, optionalUUID(in.DepartmentID), optionalUUID(in.PositionID), in.Phone, in.Email, in.IDNumber,
		parseOptionalDate(in.HireDate), parseOptionalDate(in.SeparationDate), status, in.EmergencyContact, in.EmergencyPhone,
		in.BankName, in.BankAccount)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) EmployeeDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	row := h.db.QueryRowContext(ctx, `SELECT e.id, e.code, e.name, e.department_id, e.position_id, COALESCE(e.phone,''), COALESCE(e.email,''), COALESCE(e.id_number,''),
		e.hire_date, e.separation_date, e.status, COALESCE(e.emergency_contact,''), COALESCE(e.emergency_phone,''),
		COALESCE(e.bank_name,''), COALESCE(e.bank_account,''), COALESCE(e.properties::text,'{}'),
		COALESCE(e.created_at,'1970-01-01'), COALESCE(e.updated_at,'1970-01-01'), COALESCE(e.company_id,'default')
		FROM employees e WHERE e.id=$1`, id)

	var emp models.Employee
	if err := row.Scan(&emp.ID, &emp.Code, &emp.Name, &emp.DepartmentID, &emp.PositionID, &emp.Phone, &emp.Email, &emp.IDNumber,
		&emp.HireDate, &emp.SeparationDate, &emp.Status, &emp.EmergencyContact, &emp.EmergencyPhone,
		&emp.BankName, &emp.BankAccount, &emp.Properties, &emp.CreatedAt, &emp.UpdatedAt, &emp.CompanyID); err != nil {
		shared.JSONNotFound(c, "员工不存在")
		return
	}

	if emp.DepartmentID != nil {
		_ = h.db.QueryRowContext(ctx, "SELECT name FROM departments WHERE id=$1", *emp.DepartmentID).Scan(&emp.DepartmentName)
	}
	if emp.PositionID != nil {
		_ = h.db.QueryRowContext(ctx, "SELECT name FROM positions WHERE id=$1", *emp.PositionID).Scan(&emp.PositionName)
	}

	contractRows, _ := h.db.QueryContext(ctx, `SELECT id, employee_id, contract_no, contract_type, start_date, end_date, salary, status, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM contracts WHERE employee_id=$1 ORDER BY start_date DESC`, id)
	var contracts []models.Contract
	if contractRows != nil {
		defer contractRows.Close()
		for contractRows.Next() {
			var ct models.Contract
			if err := contractRows.Scan(&ct.ID, &ct.EmployeeID, &ct.ContractNo, &ct.ContractType, &ct.StartDate, &ct.EndDate, &ct.Salary, &ct.Status, &ct.CreatedAt, &ct.CompanyID); err != nil {
				continue
			}
			contracts = append(contracts, ct)
		}
	}

	salaryRows, _ := h.db.QueryContext(ctx, `SELECT id, employee_id, month, base_salary, overtime_pay, bonus, deduction, social_security, housing_fund, tax, net_salary, status, COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM salary_components WHERE employee_id=$1 ORDER BY month DESC LIMIT 12`, id)
	var salaries []models.SalaryComponent
	if salaryRows != nil {
		defer salaryRows.Close()
		for salaryRows.Next() {
			var s models.SalaryComponent
			if err := salaryRows.Scan(&s.ID, &s.EmployeeID, &s.Month, &s.BaseSalary, &s.OvertimePay, &s.Bonus, &s.Deduction,
				&s.SocialSecurity, &s.HousingFund, &s.Tax, &s.NetSalary, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.CompanyID); err != nil {
				continue
			}
			salaries = append(salaries, s)
		}
	}

	shared.JSONOK(c, employeeDetail{
		Employee:  emp,
		Contracts: shared.EmptySlice(contracts),
		Salaries:  shared.EmptySlice(salaries),
	})
}

func (h *Handler) EmployeeUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in employeeInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if _, err := h.db.ExecContext(ctx, `UPDATE employees SET name=$1, department_id=$2, position_id=$3, phone=$4, email=$5, id_number=$6,
		hire_date=$7, separation_date=$8, status=$9, emergency_contact=$10, emergency_phone=$11, bank_name=$12, bank_account=$13, updated_at=NOW() WHERE id=$14`,
		in.Name, optionalUUID(in.DepartmentID), optionalUUID(in.PositionID), in.Phone, in.Email, in.IDNumber,
		parseOptionalDate(in.HireDate), parseOptionalDate(in.SeparationDate), in.Status, in.EmergencyContact, in.EmergencyPhone,
		in.BankName, in.BankAccount, id); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) EmployeeDeactivate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	today := time.Now()
	if _, err := h.db.ExecContext(ctx, "UPDATE employees SET status='inactive', separation_date=$1, updated_at=NOW() WHERE id=$2", today, id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) ContractsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)

	rows, err := h.db.QueryContext(ctx, `SELECT c.id, c.employee_id, c.contract_no, c.contract_type, c.start_date, c.end_date, c.salary, c.status, COALESCE(c.created_at,'1970-01-01'), COALESCE(c.company_id,'default'), COALESCE(e.name,'')
		FROM contracts c LEFT JOIN employees e ON c.employee_id = e.id ORDER BY c.created_at DESC LIMIT $1 OFFSET $2`, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var allContracts []models.Contract
	for rows.Next() {
		var ct models.Contract
		if err := rows.Scan(&ct.ID, &ct.EmployeeID, &ct.ContractNo, &ct.ContractType, &ct.StartDate, &ct.EndDate, &ct.Salary, &ct.Status, &ct.CreatedAt, &ct.CompanyID, &ct.EmployeeName); err != nil {
			continue
		}
		allContracts = append(allContracts, ct)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM contracts").Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.Contract]{Items: shared.EmptySlice(allContracts), Total: count, Page: page})
}

func (h *Handler) ContractCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in contractInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.ContractNo == "" || in.EmployeeID == uuid.Nil {
		shared.JSONBadRequest(c, "请填写合同编号和员工")
		return
	}

	id := uuid.New()
	if _, err := h.db.ExecContext(ctx, `INSERT INTO contracts (id, employee_id, contract_no, contract_type, start_date, end_date, salary, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'active')`,
		id, in.EmployeeID, in.ContractNo, in.ContractType, parseOptionalDate(in.StartDate), parseOptionalDate(in.EndDate), in.Salary); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) SalaryListPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	rows, err := h.db.QueryContext(ctx, `SELECT sc.id, sc.employee_id, sc.month, sc.base_salary, sc.overtime_pay, sc.bonus, sc.deduction, sc.social_security, sc.housing_fund, sc.tax, sc.net_salary, sc.status, COALESCE(sc.created_at,'1970-01-01'), COALESCE(sc.updated_at,'1970-01-01'), COALESCE(sc.company_id,'default'), COALESCE(e.name,'')
		FROM salary_components sc LEFT JOIN employees e ON sc.employee_id = e.id
		WHERE TO_CHAR(sc.month,'YYYY-MM') = $1 ORDER BY e.name LIMIT $2 OFFSET $3`, month, 20, (page-1)*20)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var salaryComponents []models.SalaryComponent
	for rows.Next() {
		var s models.SalaryComponent
		if err := rows.Scan(&s.ID, &s.EmployeeID, &s.Month, &s.BaseSalary, &s.OvertimePay, &s.Bonus, &s.Deduction,
			&s.SocialSecurity, &s.HousingFund, &s.Tax, &s.NetSalary, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.CompanyID, &s.EmployeeName); err != nil {
			continue
		}
		salaryComponents = append(salaryComponents, s)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM salary_components WHERE TO_CHAR(month,'YYYY-MM') = $1", month).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.SalaryComponent]{Items: shared.EmptySlice(salaryComponents), Total: count, Page: page})
}

func (h *Handler) SalaryBatchCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in salaryBatchInput
	if !shared.BindJSON(c, &in) {
		return
	}

	monthDate, err := time.Parse("2006-01-02", in.Month+"-01")
	if err != nil {
		shared.JSONBadRequest(c, "无效月份")
		return
	}

	for _, row := range in.Salaries {
		if row.EmployeeID == uuid.Nil {
			continue
		}
		baseSalary := orZero(row.BaseSalary)
		overtimePay := orZero(row.OvertimePay)
		bonus := orZero(row.Bonus)
		deduction := orZero(row.Deduction)
		socialSecurity := orZero(row.SocialSecurity)
		housingFund := orZero(row.HousingFund)
		tax := orZero(row.Tax)

		if _, err := h.db.ExecContext(ctx, `INSERT INTO salary_components (id, employee_id, month, base_salary, overtime_pay, bonus, deduction, social_security, housing_fund, tax, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'draft')
			ON CONFLICT (employee_id, month) DO UPDATE SET base_salary=$4, overtime_pay=$5, bonus=$6, deduction=$7, social_security=$8, housing_fund=$9, tax=$10, updated_at=NOW()`,
			uuid.New(), row.EmployeeID, monthDate, baseSalary, overtimePay, bonus, deduction, socialSecurity, housingFund, tax); err != nil {
			shared.JSONInternal(c, err)
			return
		}
	}

	shared.JSONOK(c, gin.H{"ok": true})
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func (h *Handler) SalaryDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	row := h.db.QueryRowContext(ctx, `SELECT id, employee_id, month, base_salary, overtime_pay, bonus, deduction, social_security, housing_fund, tax, net_salary, status, COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')
		FROM salary_components WHERE id=$1`, id)

	var s models.SalaryComponent
	if err := row.Scan(&s.ID, &s.EmployeeID, &s.Month, &s.BaseSalary, &s.OvertimePay, &s.Bonus, &s.Deduction,
		&s.SocialSecurity, &s.HousingFund, &s.Tax, &s.NetSalary, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.CompanyID); err != nil {
		shared.JSONNotFound(c, "薪资记录不存在")
		return
	}

	_ = h.db.QueryRowContext(ctx, "SELECT name FROM employees WHERE id=$1", s.EmployeeID).Scan(&s.EmployeeName)

	shared.JSONOK(c, s)
}

func (h *Handler) SalaryConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	var base, ot, bonus, deduction, ss, hf, tax decimal.Decimal
	var employeeName string
	var month time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT sc.base_salary, sc.overtime_pay, sc.bonus, sc.deduction, sc.social_security, sc.housing_fund, sc.tax,
		       COALESCE(e.name,''), sc.month
		FROM salary_components sc
		LEFT JOIN employees e ON e.id = sc.employee_id
		WHERE sc.id=$1 AND sc.status='draft' FOR UPDATE OF sc`, id).Scan(
		&base, &ot, &bonus, &deduction, &ss, &hf, &tax, &employeeName, &month)
	if err != nil {
		shared.JSONBadRequest(c, "工资单不存在或已确认")
		return
	}
	res, err := tx.ExecContext(ctx, "UPDATE salary_components SET status='confirmed', updated_at=NOW() WHERE id=$1 AND status='draft'", id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		shared.JSONBadRequest(c, "确认失败")
		return
	}
	gross := base.Add(ot).Add(bonus)
	withhold := deduction.Add(ss).Add(hf).Add(tax)
	net := gross.Sub(withhold)
	if err := ledger.PostSalary(ctx, tx, id, gross, net, withhold, employeeName, month.Format("2006-01"), ledger.Actor(c)); err != nil {
		shared.JSONBadRequest(c, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) SalarySummaryAPI(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, `
		SELECT TO_CHAR(month, 'YYYY-MM') AS label,
			COALESCE(SUM(base_salary),0) AS base,
			COALESCE(SUM(overtime_pay),0) AS overtime,
			COALESCE(SUM(bonus),0) AS bonus,
			COALESCE(SUM(deduction),0) AS deduction,
			COALESCE(SUM(net_salary),0) AS net
		FROM salary_components
		WHERE status = 'confirmed'
		AND month >= DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '11 months'
		GROUP BY month ORDER BY month`)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	type salaryMonth struct {
		Label     string  `json:"label"`
		Base      float64 `json:"base"`
		Overtime  float64 `json:"overtime"`
		Bonus     float64 `json:"bonus"`
		Deduction float64 `json:"deduction"`
		Net       float64 `json:"net"`
	}
	var result []salaryMonth
	for rows.Next() {
		var m salaryMonth
		if err := rows.Scan(&m.Label, &m.Base, &m.Overtime, &m.Bonus, &m.Deduction, &m.Net); err != nil {
			continue
		}
		result = append(result, m)
	}
	shared.JSONOK(c, shared.EmptySlice(result))
}

func (h *Handler) AttendanceStatsAPI(c *gin.Context) {
	ctx := c.Request.Context()
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	rows, err := h.db.QueryContext(ctx, `
		SELECT employee_name,
			COUNT(CASE WHEN check_type = 'check_in' THEN 1 END) AS work_days,
			COUNT(CASE WHEN check_in_time::time > '09:00' THEN 1 END) AS late_days,
			COUNT(CASE WHEN check_type = 'check_out' AND check_in_time::time > '18:00' THEN 1 END) AS overtime_days
		FROM attendance_records
		WHERE TO_CHAR(check_date, 'YYYY-MM') = $1
		GROUP BY employee_name ORDER BY employee_name`, month)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	type attStat struct {
		EmployeeName string `json:"employee_name"`
		WorkDays     int64  `json:"work_days"`
		LateDays     int64  `json:"late_days"`
		OvertimeDays int64  `json:"overtime_days"`
	}
	var result []attStat
	for rows.Next() {
		var s attStat
		if err := rows.Scan(&s.EmployeeName, &s.WorkDays, &s.LateDays, &s.OvertimeDays); err != nil {
			continue
		}
		result = append(result, s)
	}
	shared.JSONOK(c, shared.EmptySlice(result))
}

func hashName(name string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return h.Sum32()
}
