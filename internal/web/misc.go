package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/view/pages"
)

const wrCols = `id, type, report_date, employee_code, employee_name, COALESCE(department,''),
	COALESCE(work_done,''), COALESCE(tomorrow_plan,''), COALESCE(issues,''), status,
	COALESCE(created_at,'1970-01-01'), COALESCE(updated_at,'1970-01-01'), COALESCE(company_id,'default')`

func scanWRRow(row interface{ Scan(...any) error }) (models.WorkReport, error) {
	var wr models.WorkReport
	err := row.Scan(&wr.ID, &wr.Type, &wr.ReportDate, &wr.EmployeeCode, &wr.EmployeeName,
		&wr.Department, &wr.WorkDone, &wr.TomorrowPlan, &wr.Issues, &wr.Status,
		&wr.CreatedAt, &wr.UpdatedAt, &wr.CompanyID)
	return wr, err
}

func (h *Handler) WorkReportsPage(c *gin.Context) {
	ctx := c.Request.Context()
	typ := c.Query("type")
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	where, args := "1=1", []any{}
	if typ == "daily" || typ == "weekly" {
		where = "type = $1"
		args = append(args, typ)
	}
	// limit/offset 为内部整数，直接拼接，无注入风险
	rows, err := h.db.QueryContext(ctx, `SELECT `+wrCols+` FROM work_reports WHERE `+where+` ORDER BY report_date DESC LIMIT $`+itoa(len(args)+1)+` OFFSET $`+itoa(len(args)+2), append(args, limit, (page-1)*limit)...)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.WorkReport
	for rows.Next() {
		if wr, err := scanWRRow(rows); err == nil {
			items = append(items, wr)
		}
	}
	var total int64
	_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM work_reports WHERE `+where, args...).Scan(&total)
	if isHX(c) {
		renderFrag(c, pages.WorkReportListInner(items, typ, page, total, limit))
		return
	}
	h.renderPage(c, "工作报告", pages.WorkReportList(items, typ, page, total, limit))
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func (h *Handler) WorkReportNewPage(c *gin.Context) {
	h.renderPage(c, "新建工作报告", pages.WorkReportForm(""))
}

func (h *Handler) WorkReportCreate(c *gin.Context) {
	ctx := c.Request.Context()
	typ := c.PostForm("type")
	if typ != "daily" && typ != "weekly" {
		typ = "daily"
	}
	reportDate := time.Now()
	if d := strings.TrimSpace(c.PostForm("report_date")); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			reportDate = t
		}
	}
	id := uuid.New()
	if _, err := h.db.ExecContext(ctx, `INSERT INTO work_reports (id, type, report_date, employee_code, employee_name, department, work_done, tomorrow_plan, issues, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft')`,
		id, typ, reportDate.Format("2006-01-02"), strings.TrimSpace(c.PostForm("employee_code")),
		strings.TrimSpace(c.PostForm("employee_name")), strings.TrimSpace(c.PostForm("department")),
		strings.TrimSpace(c.PostForm("work_done")), strings.TrimSpace(c.PostForm("tomorrow_plan")),
		strings.TrimSpace(c.PostForm("issues"))); err != nil {
		h.renderPage(c, "新建工作报告", pages.WorkReportForm("保存失败"))
		return
	}
	redirect(c, "/workreports/"+id.String())
}

func (h *Handler) WorkReportDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	wr, err := scanWRRow(h.db.QueryRowContext(c.Request.Context(), "SELECT "+wrCols+" FROM work_reports WHERE id = $1", id))
	if err != nil {
		c.String(http.StatusNotFound, "报告不存在")
		return
	}
	h.renderPage(c, "工作报告", pages.WorkReportDetail(wr))
}

func (h *Handler) PickingPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, `SELECT id, picking_no, type, status, order_date, COALESCE(assigned_to,''), COALESCE(notes,''),
		created_at, updated_at, COALESCE(company_id,'default') FROM picking_orders ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载失败")
		return
	}
	defer rows.Close()
	var items []models.PickingOrder
	for rows.Next() {
		var o models.PickingOrder
		if err := rows.Scan(&o.ID, &o.PickingNo, &o.Type, &o.Status, &o.OrderDate, &o.AssignedTo, &o.Notes,
			&o.CreatedAt, &o.UpdatedAt, &o.CompanyID); err == nil {
			items = append(items, o)
		}
	}
	h.renderPage(c, "分拣", pages.PickingList(items))
}

func (h *Handler) PickingDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	ctx := c.Request.Context()
	var o models.PickingOrder
	err = h.db.QueryRowContext(ctx, `SELECT id, picking_no, type, status, order_date, COALESCE(assigned_to,''), COALESCE(notes,''),
		created_at, updated_at, COALESCE(company_id,'default') FROM picking_orders WHERE id = $1`, id).Scan(
		&o.ID, &o.PickingNo, &o.Type, &o.Status, &o.OrderDate, &o.AssignedTo, &o.Notes, &o.CreatedAt, &o.UpdatedAt, &o.CompanyID)
	if err != nil {
		c.String(http.StatusNotFound, "分拣单不存在")
		return
	}
	rows, _ := h.db.QueryContext(ctx, `SELECT id, picking_id, product_id, COALESCE(product_name,''), COALESCE(product_code,''),
		required_quantity, picked_quantity, COALESCE(source_order_id, '00000000-0000-0000-0000-000000000000'),
		COALESCE(source_customer_id, '00000000-0000-0000-0000-000000000000'), COALESCE(source_customer_name,''),
		COALESCE(status,''), created_at FROM picking_items WHERE picking_id = $1 ORDER BY product_name`, id)
	var items []models.PickingItem
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var it models.PickingItem
			if err := rows.Scan(&it.ID, &it.PickingID, &it.ProductID, &it.ProductName, &it.ProductCode,
				&it.RequiredQuantity, &it.PickedQuantity, &it.SourceOrderID, &it.SourceCustomerID, &it.SourceCustomerName,
				&it.Status, &it.CreatedAt); err == nil {
				items = append(items, it)
			}
		}
	}
	h.renderPage(c, o.PickingNo, pages.PickingDetail(o, items, ""))
}

func (h *Handler) PickingComplete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "无效的ID")
		return
	}
	_, _ = h.db.ExecContext(c.Request.Context(), `UPDATE picking_orders SET status='completed', updated_at=NOW() WHERE id=$1 AND status != 'completed'`, id)
	redirect(c, "/picking/"+id.String())
}

func (h *Handler) AttendancePage(c *gin.Context) {
	ctx := c.Request.Context()
	date := strings.TrimSpace(c.Query("date"))
	page := getPage(c.Request.URL.Query(), "page")
	const limit = 20
	var items []models.AttendanceRecord
	var total int64
	if date == "" {
		rws, err := h.db.QueryContext(ctx, `SELECT id, employee_code, employee_name, COALESCE(department,''), check_in_time, check_type, source, COALESCE(location,''), COALESCE(remark,''), check_date, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default') FROM attendance_records ORDER BY check_in_time DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rws.Close()
		for rws.Next() {
			var r models.AttendanceRecord
			if err := rws.Scan(&r.ID, &r.EmployeeCode, &r.EmployeeName, &r.Department, &r.CheckInTime, &r.CheckType,
				&r.Source, &r.Location, &r.Remark, &r.CheckDate, &r.CreatedAt, &r.CompanyID); err == nil {
				items = append(items, r)
			}
		}
		_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM attendance_records").Scan(&total)
	} else {
		rws, err := h.db.QueryContext(ctx, `SELECT id, employee_code, employee_name, COALESCE(department,''), check_in_time, check_type, source, COALESCE(location,''), COALESCE(remark,''), check_date, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default') FROM attendance_records WHERE check_date = $1 ORDER BY check_in_time DESC LIMIT $2 OFFSET $3`, date, limit, (page-1)*limit)
		if err != nil {
			c.String(http.StatusInternalServerError, "加载失败")
			return
		}
		defer rws.Close()
		for rws.Next() {
			var r models.AttendanceRecord
			if err := rws.Scan(&r.ID, &r.EmployeeCode, &r.EmployeeName, &r.Department, &r.CheckInTime, &r.CheckType,
				&r.Source, &r.Location, &r.Remark, &r.CheckDate, &r.CreatedAt, &r.CompanyID); err == nil {
				items = append(items, r)
			}
		}
		_ = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM attendance_records WHERE check_date = $1", date).Scan(&total)
	}
	if isHX(c) {
		renderFrag(c, pages.AttendanceListInner(items, date, page, total, limit))
		return
	}
	h.renderPage(c, "考勤打卡", pages.AttendanceList(items, date, page, total, limit))
}
