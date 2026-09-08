package workreports

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

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

const wrCols = `id, type, report_date, employee_code, employee_name, COALESCE(department,''),
	COALESCE(work_done,''), COALESCE(tomorrow_plan,''), COALESCE(issues,''), status,
	COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')`

type reportInput struct {
	Type         string `json:"type"`
	ReportDate   string `json:"report_date"`
	EmployeeCode string `json:"employee_code"`
	EmployeeName string `json:"employee_name"`
	Department   string `json:"department"`
	WorkDone     string `json:"work_done"`
	TomorrowPlan string `json:"tomorrow_plan"`
	Issues       string `json:"issues"`
	Action       string `json:"action"`
	Status       string `json:"status"`
}

type templateInput struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	WorkDone     string `json:"work_done"`
	TomorrowPlan string `json:"tomorrow_plan"`
	Issues       string `json:"issues"`
	IsDefault    bool   `json:"is_default"`
}

type weeklyGenerateInput struct {
	WeekEnd      string   `json:"week_end"`
	EmployeeName string   `json:"employee_name"`
	WorkDone     string   `json:"work_done"`
	TomorrowPlan string   `json:"tomorrow_plan"`
	Issues       string   `json:"issues"`
	DailyIDs     []string `json:"daily_ids"`
}

type reportDetail struct {
	models.WorkReport
	LinkedReports []models.WorkReport `json:"linked_reports,omitempty"`
}

func scanWorkReport(row interface{ Scan(...interface{}) error }) (models.WorkReport, error) {
	var wr models.WorkReport
	err := row.Scan(&wr.ID, &wr.Type, &wr.ReportDate, &wr.EmployeeCode, &wr.EmployeeName,
		&wr.Department, &wr.WorkDone, &wr.TomorrowPlan, &wr.Issues, &wr.Status,
		&wr.CreatedAt, &wr.UpdatedAt, &wr.CompanyID)
	return wr, err
}

func (h *Handler) ReportsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	reportType := c.Query("type")

	where := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1

	if reportType == "daily" || reportType == "weekly" {
		where = append(where, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, reportType)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	queryArgs := append(args, 20, (page-1)*20)
	rows, err := h.db.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM work_reports WHERE %s ORDER BY report_date DESC LIMIT $%d OFFSET $%d",
			wrCols, whereClause, argIdx, argIdx+1),
		queryArgs...)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var reports []models.WorkReport
	for rows.Next() {
		wr, err := scanWorkReport(rows)
		if err != nil {
			shared.JSONInternal(c, err)
			return
		}
		reports = append(reports, wr)
	}

	var count int64
	if err := h.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM work_reports WHERE %s", whereClause),
		args...).Scan(&count); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.PageResult[models.WorkReport]{Items: shared.EmptySlice(reports), Total: count, Page: page})
}

func (h *Handler) ReportCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in reportInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.Type == "" || in.EmployeeName == "" || in.ReportDate == "" {
		shared.JSONBadRequest(c, "请填写类型、员工姓名和日期")
		return
	}

	var reportDate time.Time
	if d, err := time.Parse("2006-01-02", in.ReportDate); err == nil {
		reportDate = d
	}

	status := "draft"
	if in.Action == "submit" || in.Status == "submitted" {
		status = "submitted"
	}

	id := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO work_reports (id, type, report_date, employee_code, employee_name, department, work_done, tomorrow_plan, issues, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		id, in.Type, reportDate.Format("2006-01-02"), in.EmployeeCode, in.EmployeeName,
		in.Department, in.WorkDone, in.TomorrowPlan, in.Issues, status)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) ReportDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	row := h.db.QueryRowContext(ctx, "SELECT "+wrCols+" FROM work_reports WHERE id = $1", id)
	wr, err := scanWorkReport(row)
	if err != nil {
		shared.JSONNotFound(c, "报告不存在")
		return
	}

	var linkedReports []models.WorkReport
	if wr.Type == "weekly" {
		linkedRows, err := h.db.QueryContext(ctx,
			`SELECT `+wrCols+` FROM work_reports WHERE id IN (SELECT daily_report_id FROM weekly_report_links WHERE weekly_report_id = $1) ORDER BY report_date`, id)
		if err == nil {
			defer linkedRows.Close()
			for linkedRows.Next() {
				lr, err := scanWorkReport(linkedRows)
				if err == nil {
					linkedReports = append(linkedReports, lr)
				}
			}
		}
	}

	shared.JSONOK(c, reportDetail{WorkReport: wr, LinkedReports: shared.EmptySlice(linkedReports)})
}

func (h *Handler) ReportUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}
	var in reportInput
	if !shared.BindJSON(c, &in) {
		return
	}

	status := in.Status
	if in.Action == "submit" {
		status = "submitted"
	} else if status == "" {
		// 读取失败时保持 status 为空串，交由下方 UPDATE 上报错误。
		_ = h.db.QueryRowContext(ctx, "SELECT status FROM work_reports WHERE id = $1", id).Scan(&status)
	}

	_, err = h.db.ExecContext(ctx, `UPDATE work_reports SET type=$1, report_date=$2, employee_code=$3, employee_name=$4, department=$5, work_done=$6, tomorrow_plan=$7, issues=$8, status=$9, updated_at=NOW() WHERE id=$10`,
		in.Type, in.ReportDate, in.EmployeeCode, in.EmployeeName, in.Department, in.WorkDone, in.TomorrowPlan, in.Issues, status, id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, gin.H{"ok": true})
}

func (h *Handler) WeeklyGenerateCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in weeklyGenerateInput
	if !shared.BindJSON(c, &in) {
		return
	}

	weeklyID := uuid.New()
	var reportDate time.Time
	if d, err := time.Parse("2006-01-02", in.WeekEnd); err == nil {
		reportDate = d
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO work_reports (id, type, report_date, employee_code, employee_name, department, work_done, tomorrow_plan, issues, status)
		VALUES ($1, 'weekly', $2, '', $3, '', $4, $5, $6, 'submitted')`,
		weeklyID, reportDate.Format("2006-01-02"), in.EmployeeName, in.WorkDone, in.TomorrowPlan, in.Issues)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	for _, didStr := range in.DailyIDs {
		didStr = strings.TrimSpace(didStr)
		if didStr == "" {
			continue
		}
		did, err := uuid.Parse(didStr)
		if err != nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO weekly_report_links (id, weekly_report_id, daily_report_id) VALUES ($1, $2, $3)",
			uuid.New(), weeklyID, did); err != nil {
			shared.JSONInternal(c, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": weeklyID})
}

func (h *Handler) TemplatesPage(c *gin.Context) {
	ctx := c.Request.Context()
	templateType := c.DefaultQuery("type", "daily")

	rows, err := h.db.QueryContext(ctx,
		"SELECT id, name, type, COALESCE(work_done,''), COALESCE(tomorrow_plan,''), COALESCE(issues,''), COALESCE(is_default,false), COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default') FROM report_templates WHERE type = $1 ORDER BY name",
		templateType)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	defer rows.Close()

	var allTemplates []models.ReportTemplate
	for rows.Next() {
		var t models.ReportTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.WorkDone, &t.TomorrowPlan, &t.Issues, &t.IsDefault, &t.CreatedAt, &t.CompanyID); err != nil {
			continue
		}
		allTemplates = append(allTemplates, t)
	}

	shared.JSONOK(c, shared.PageResult[models.ReportTemplate]{Items: shared.EmptySlice(allTemplates), Total: int64(len(allTemplates)), Page: 1})
}

func (h *Handler) TemplateCreate(c *gin.Context) {
	ctx := c.Request.Context()
	var in templateInput
	if !shared.BindJSON(c, &in) {
		return
	}

	if in.Name == "" || in.Type == "" {
		shared.JSONBadRequest(c, "请填写名称和类型")
		return
	}

	id := uuid.New()
	_, err := h.db.ExecContext(ctx, `INSERT INTO report_templates (id, name, type, work_done, tomorrow_plan, issues, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		id, in.Name, in.Type, in.WorkDone, in.TomorrowPlan, in.Issues, in.IsDefault)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, gin.H{"ok": true, "id": id})
}

func (h *Handler) TemplateDelete(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效ID")
		return
	}

	if _, err := h.db.ExecContext(ctx, "DELETE FROM report_templates WHERE id = $1", id); err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}
