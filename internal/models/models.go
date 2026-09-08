package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
}

type Product struct {
	ID              uuid.UUID       `json:"id"`
	Code            string          `json:"code"`
	Name            string          `json:"name"`
	Category        string          `json:"category"`
	Unit            string          `json:"unit"`
	SalePrice       decimal.Decimal `json:"sale_price"`
	CostPrice       decimal.Decimal `json:"cost_price"`
	SafetyStock     int32           `json:"safety_stock"`
	CurrentStock    int32           `json:"current_stock"`
	PricingType     string          `json:"pricing_type"`
	ShelfLifeDays   int32           `json:"shelf_life_days"`
	DefaultTaxRate  decimal.Decimal `json:"default_tax_rate"`
	CreatedAt       time.Time       `json:"created_at"`
	CompanyID       string          `json:"company_id"`
	Properties      string          `json:"properties"`
}

type Customer struct {
	ID            uuid.UUID       `json:"id"`
	Code          string          `json:"code"`
	Name          string          `json:"name"`
	ContactPerson string          `json:"contact_person"`
	Phone         string          `json:"phone"`
	Email         string          `json:"email"`
	Address       string          `json:"address"`
	CreditLimit   decimal.Decimal `json:"credit_limit"`
	Balance       decimal.Decimal `json:"balance"`
	Tier          string          `json:"tier"`
	SalesPerson   string          `json:"sales_person"`
	CreatedAt     time.Time       `json:"created_at"`
	CompanyID     string          `json:"company_id"`
	Properties    string          `json:"properties"`
}

type Supplier struct {
	ID            uuid.UUID       `json:"id"`
	Code          string          `json:"code"`
	Name          string          `json:"name"`
	ContactPerson string          `json:"contact_person"`
	Phone         string          `json:"phone"`
	Email         string          `json:"email"`
	Address       string          `json:"address"`
	Balance       decimal.Decimal `json:"balance"`
	Rating        decimal.Decimal `json:"rating"`
	OnTimeRate    decimal.Decimal `json:"on_time_rate"`
	QualityRate   decimal.Decimal `json:"quality_rate"`
	CreatedAt     time.Time       `json:"created_at"`
	CompanyID     string          `json:"company_id"`
	Properties    string          `json:"properties"`
}

type SalesOrder struct {
	ID           uuid.UUID       `json:"id"`
	OrderNo      string          `json:"order_no"`
	CustomerID   uuid.UUID       `json:"customer_id"`
	CustomerName string          `json:"customer_name"`
	Status       string          `json:"status"`
	TotalAmount  decimal.Decimal `json:"total_amount"`
	PaidAmount   decimal.Decimal `json:"paid_amount"`
	OrderDate    time.Time       `json:"order_date"`
	DeliveryDate time.Time       `json:"delivery_date"`
	Notes        string          `json:"notes"`
	CreatedAt    time.Time       `json:"created_at"`
	CompanyID    string          `json:"company_id"`
	Properties   string          `json:"properties"`
}

type SalesOrderItem struct {
	ID             uuid.UUID       `json:"id"`
	OrderID        uuid.UUID       `json:"order_id"`
	ProductID      uuid.UUID       `json:"product_id"`
	ProductName    string          `json:"product_name"`
	ProductCode    string          `json:"product_code"`
	Quantity       int32           `json:"quantity"`
	UnitPrice      decimal.Decimal `json:"unit_price"`
	Amount         decimal.Decimal `json:"amount"`
	TaxRate        decimal.Decimal `json:"tax_rate"`
	PricingType    string          `json:"pricing_type"`
	ActualQuantity decimal.Decimal `json:"actual_quantity"`
	ActualAmount   decimal.Decimal `json:"actual_amount"`
}

type SalesOrderDetail struct {
	Order SalesOrder       `json:"order"`
	Items []SalesOrderItem `json:"items"`
}

type PurchaseOrder struct {
	ID           uuid.UUID       `json:"id"`
	OrderNo      string          `json:"order_no"`
	SupplierID   uuid.UUID       `json:"supplier_id"`
	SupplierName string          `json:"supplier_name"`
	Status       string          `json:"status"`
	TotalAmount  decimal.Decimal `json:"total_amount"`
	PaidAmount   decimal.Decimal `json:"paid_amount"`
	OrderDate    time.Time       `json:"order_date"`
	DeliveryDate time.Time       `json:"delivery_date"`
	Notes        string          `json:"notes"`
	CreatedAt    time.Time       `json:"created_at"`
	CompanyID    string          `json:"company_id"`
	Properties   string          `json:"properties"`
}

type PurchaseOrderItem struct {
	ID             uuid.UUID       `json:"id"`
	OrderID        uuid.UUID       `json:"order_id"`
	ProductID      uuid.UUID       `json:"product_id"`
	ProductName    string          `json:"product_name"`
	ProductCode    string          `json:"product_code"`
	Quantity       int32           `json:"quantity"`
	UnitPrice      decimal.Decimal `json:"unit_price"`
	Amount         decimal.Decimal `json:"amount"`
	TaxRate        decimal.Decimal `json:"tax_rate"`
	PricingType    string          `json:"pricing_type"`
	ActualQuantity decimal.Decimal `json:"actual_quantity"`
	ActualAmount   decimal.Decimal `json:"actual_amount"`
}

type PurchaseOrderDetail struct {
	Order PurchaseOrder       `json:"order"`
	Items []PurchaseOrderItem `json:"items"`
}

type InventoryMovement struct {
	ID            uuid.UUID `json:"id"`
	ProductID     uuid.UUID `json:"product_id"`
	ProductName   string    `json:"product_name"`
	ProductCode   string    `json:"product_code"`
	Type          string    `json:"type"`
	Quantity      int32     `json:"quantity"`
	ReferenceType string    `json:"reference_type"`
	BeforeStock   int32     `json:"before_stock"`
	AfterStock    int32     `json:"after_stock"`
	BatchID       uuid.UUID `json:"batch_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	CompanyID     string    `json:"company_id"`
}

type InventoryBatch struct {
	ID               uuid.UUID       `json:"id"`
	ProductID        uuid.UUID       `json:"product_id"`
	WarehouseID      string          `json:"warehouse_id"`
	BatchNo          string          `json:"batch_no"`
	ProductionDate   *time.Time      `json:"production_date"`
	ExpiryDate       *time.Time      `json:"expiry_date"`
	Quantity         decimal.Decimal `json:"quantity"`
	OriginalQuantity decimal.Decimal `json:"original_quantity"`
	UnitCost         decimal.Decimal `json:"unit_cost"`
	Status           string          `json:"status"`
	ReferenceType    string          `json:"reference_type"`
	ReferenceID      uuid.UUID       `json:"reference_id"`
	CreatedAt        time.Time       `json:"created_at"`
	CompanyID        string          `json:"company_id"`
}

type Payment struct {
	ID          uuid.UUID       `json:"id"`
	Type        string          `json:"type"`
	Amount      decimal.Decimal `json:"amount"`
	PartnerName string          `json:"partner_name"`
	PartnerType string          `json:"partner_type"`
	PartnerID   *uuid.UUID      `json:"partner_id,omitempty"`
	Notes       string          `json:"notes"`
	PaymentDate time.Time       `json:"payment_date"`
	CreatedAt   time.Time       `json:"created_at"`
	CompanyID   string          `json:"company_id"`
}

// Clearing 记录每笔挂单收付款冲抵的应收/应付单据（权威核销明细）。
type Clearing struct {
	ID         uuid.UUID       `json:"id"`
	PaymentID  uuid.UUID       `json:"payment_id"`
	DocType    string          `json:"doc_type"`
	DocID      uuid.UUID       `json:"doc_id"`
	Amount     decimal.Decimal `json:"amount"`
	Status     string          `json:"status"`
	ClearedBy  string          `json:"cleared_by"`
	ClearedAt  time.Time       `json:"cleared_at"`
	CompanyID  string          `json:"company_id"`
}

type DashboardData struct {
	MonthSales            string       `json:"month_sales"`
	PendingOrders         int64        `json:"pending_orders"`
	LowStockCount         int64        `json:"low_stock_count"`
	ActiveCustomers       int64        `json:"active_customers"`
	RecentOrders          []SalesOrder `json:"recent_orders"`
	LowStockItems         []Product    `json:"low_stock_items"`
	PendingReimbursements int64        `json:"pending_reimbursements"`
	ActiveEmployees       int64        `json:"active_employees"`
	WeeklyReports         int64        `json:"weekly_reports"`
	ExpiringContracts     int64        `json:"expiring_contracts"`
}

type PartnerNote struct {
	ID           uuid.UUID  `json:"id"`
	PartnerType  string     `json:"partner_type"`
	PartnerID    uuid.UUID  `json:"partner_id"`
	PartnerName  string     `json:"partner_name"`
	NoteType     string     `json:"note_type"`
	Content      string     `json:"content"`
	NextFollowUp *time.Time `json:"next_follow_up"`
	CreatedAt    time.Time  `json:"created_at"`
	CompanyID    string     `json:"company_id"`
}

type AttendanceRecord struct {
	ID           uuid.UUID `json:"id"`
	EmployeeCode string    `json:"employee_code"`
	EmployeeName string    `json:"employee_name"`
	Department   string    `json:"department"`
	CheckInTime  time.Time `json:"check_in_time"`
	CheckType    string    `json:"check_type"`
	Source       string    `json:"source"`
	Location     string    `json:"location"`
	Remark       string    `json:"remark"`
	CheckDate    time.Time `json:"check_date"`
	CreatedAt    time.Time `json:"created_at"`
	CompanyID    string    `json:"company_id"`
}

type WorkReport struct {
	ID           uuid.UUID `json:"id"`
	Type         string    `json:"type"`
	ReportDate   time.Time `json:"report_date"`
	EmployeeCode string    `json:"employee_code"`
	EmployeeName string    `json:"employee_name"`
	Department   string    `json:"department"`
	WorkDone     string    `json:"work_done"`
	TomorrowPlan string    `json:"tomorrow_plan"`
	Issues       string    `json:"issues"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CompanyID    string    `json:"company_id"`
}

type WeeklyReportLink struct {
	ID             uuid.UUID `json:"id"`
	WeeklyReportID uuid.UUID `json:"weekly_report_id"`
	DailyReportID  uuid.UUID `json:"daily_report_id"`
}

type ReportTemplate struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	WorkDone     string    `json:"work_done"`
	TomorrowPlan string    `json:"tomorrow_plan"`
	Issues       string    `json:"issues"`
	IsDefault    bool      `json:"is_default"`
	CreatedAt    time.Time `json:"created_at"`
	CompanyID    string    `json:"company_id"`
}

type Reimbursement struct {
	ID              uuid.UUID       `json:"id"`
	ReimbursementNo string          `json:"reimbursement_no"`
	ApplicantName   string          `json:"applicant_name"`
	Department      string          `json:"department"`
	Amount          decimal.Decimal `json:"amount"`
	Category        string          `json:"category"`
	Description     string          `json:"description"`
	Status          string          `json:"status"`
	ApproverName    string          `json:"approver_name"`
	ApprovedAt      *time.Time      `json:"approved_at"`
	RejectedReason  string          `json:"rejected_reason"`
	PaymentID       *uuid.UUID      `json:"payment_id"`
	ExpenseDate     *time.Time      `json:"expense_date"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CompanyID       string          `json:"company_id"`
}

type ReimbursementItem struct {
	ID              uuid.UUID       `json:"id"`
	ReimbursementID uuid.UUID       `json:"reimbursement_id"`
	Category        string          `json:"category"`
	Amount          decimal.Decimal `json:"amount"`
	Description     string          `json:"description"`
}

type Invoice struct {
	ID            uuid.UUID       `json:"id"`
	InvoiceNo     string          `json:"invoice_no"`
	Type          string          `json:"type"`
	PartnerType   string          `json:"partner_type"`
	PartnerID     *uuid.UUID      `json:"partner_id"`
	PartnerName   string          `json:"partner_name"`
	Amount        decimal.Decimal `json:"amount"`
	TaxRate       decimal.Decimal `json:"tax_rate"`
	TaxAmount     decimal.Decimal `json:"tax_amount"`
	TotalAmount   decimal.Decimal `json:"total_amount"`
	InvoiceDate   *time.Time      `json:"invoice_date"`
	InvoiceCode   string          `json:"invoice_code"`
	InvoiceStatus string          `json:"invoice_status"`
	ReferenceType string          `json:"reference_type"`
	ReferenceID   *uuid.UUID      `json:"reference_id"`
	CreatedAt     time.Time       `json:"created_at"`
	CompanyID     string          `json:"company_id"`
}

type Reconciliation struct {
	ID               uuid.UUID       `json:"id"`
	ReconciliationNo string          `json:"reconciliation_no"`
	PartnerType      string          `json:"partner_type"`
	PartnerID        uuid.UUID       `json:"partner_id"`
	PartnerName      string          `json:"partner_name"`
	PeriodStart      time.Time       `json:"period_start"`
	PeriodEnd        time.Time       `json:"period_end"`
	OrderTotal       decimal.Decimal `json:"order_total"`
	PaymentTotal     decimal.Decimal `json:"payment_total"`
	Discrepancy      decimal.Decimal `json:"discrepancy"`
	Status           string          `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
	CompanyID        string          `json:"company_id"`
}

type ReconciliationItem struct {
	ID               uuid.UUID       `json:"id"`
	ReconciliationID uuid.UUID       `json:"reconciliation_id"`
	ItemType         string          `json:"item_type"`
	ReferenceNo      string          `json:"reference_no"`
	Amount           decimal.Decimal `json:"amount"`
	ReferenceDate    *time.Time      `json:"reference_date"`
}

type Department struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Code        string     `json:"code"`
	ParentID    *uuid.UUID `json:"parent_id"`
	ManagerName string     `json:"manager_name"`
	SortOrder   int32      `json:"sort_order"`
	CreatedAt   time.Time  `json:"created_at"`
	CompanyID   string     `json:"company_id"`
}

type Position struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	DepartmentID *uuid.UUID      `json:"department_id"`
	BaseSalary   decimal.Decimal `json:"base_salary"`
	CreatedAt    time.Time       `json:"created_at"`
	CompanyID    string          `json:"company_id"`
}

type Employee struct {
	ID               uuid.UUID  `json:"id"`
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	DepartmentID     *uuid.UUID `json:"department_id"`
	PositionID       *uuid.UUID `json:"position_id"`
	Phone            string     `json:"phone"`
	Email            string     `json:"email"`
	IDNumber         string     `json:"id_number"`
	HireDate         *time.Time `json:"hire_date"`
	SeparationDate   *time.Time `json:"separation_date"`
	Status           string     `json:"status"`
	EmergencyContact string     `json:"emergency_contact"`
	EmergencyPhone   string     `json:"emergency_phone"`
	BankName         string     `json:"bank_name"`
	BankAccount      string     `json:"bank_account"`
	Properties       string     `json:"properties"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompanyID        string     `json:"company_id"`
	DepartmentName   string     `json:"department_name,omitempty"`
	PositionName     string     `json:"position_name,omitempty"`
}

type Contract struct {
	ID           uuid.UUID       `json:"id"`
	EmployeeID   uuid.UUID       `json:"employee_id"`
	EmployeeName string          `json:"employee_name"`
	ContractNo   string          `json:"contract_no"`
	ContractType string          `json:"contract_type"`
	StartDate    *time.Time      `json:"start_date"`
	EndDate      *time.Time      `json:"end_date"`
	Salary       decimal.Decimal `json:"salary"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	CompanyID    string          `json:"company_id"`
}

type SalaryComponent struct {
	ID             uuid.UUID       `json:"id"`
	EmployeeID     uuid.UUID       `json:"employee_id"`
	EmployeeName   string          `json:"employee_name"`
	Month          time.Time       `json:"month"`
	BaseSalary     decimal.Decimal `json:"base_salary"`
	OvertimePay    decimal.Decimal `json:"overtime_pay"`
	Bonus          decimal.Decimal `json:"bonus"`
	Deduction      decimal.Decimal `json:"deduction"`
	SocialSecurity decimal.Decimal `json:"social_security"`
	HousingFund    decimal.Decimal `json:"housing_fund"`
	Tax            decimal.Decimal `json:"tax"`
	NetSalary      decimal.Decimal `json:"net_salary"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompanyID      string          `json:"company_id"`
}

type PriceTier struct {
	ID          uuid.UUID       `json:"id"`
	ProductID   uuid.UUID       `json:"product_id"`
	MinQuantity int32           `json:"min_quantity"`
	MaxQuantity int32           `json:"max_quantity"`
	UnitPrice   decimal.Decimal `json:"unit_price"`
	CreatedAt   time.Time       `json:"created_at"`
	CompanyID   string          `json:"company_id"`
}

type CustomerProductPrice struct {
	ID            uuid.UUID       `json:"id"`
	CustomerID    uuid.UUID       `json:"customer_id"`
	ProductID     uuid.UUID       `json:"product_id"`
	UnitPrice     decimal.Decimal `json:"unit_price"`
	EffectiveFrom *time.Time      `json:"effective_from"`
	EffectiveTo   *time.Time      `json:"effective_to"`
	CreatedAt     time.Time       `json:"created_at"`
	CompanyID     string          `json:"company_id"`
}

type PickingOrder struct {
	ID         uuid.UUID `json:"id"`
	PickingNo  string    `json:"picking_no"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	OrderDate  time.Time `json:"order_date"`
	AssignedTo string    `json:"assigned_to"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	CompanyID  string    `json:"company_id"`
}

type PickingItem struct {
	ID                 uuid.UUID       `json:"id"`
	PickingID          uuid.UUID       `json:"picking_id"`
	ProductID          uuid.UUID       `json:"product_id"`
	ProductName        string          `json:"product_name"`
	ProductCode        string          `json:"product_code"`
	RequiredQuantity   decimal.Decimal `json:"required_quantity"`
	PickedQuantity     decimal.Decimal `json:"picked_quantity"`
	SourceOrderID      uuid.UUID       `json:"source_order_id"`
	SourceCustomerID   uuid.UUID       `json:"source_customer_id"`
	SourceCustomerName string          `json:"source_customer_name"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"created_at"`
}
