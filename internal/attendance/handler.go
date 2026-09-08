package attendance

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"hash/fnv"
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

const attCols = `id, employee_code, employee_name, COALESCE(department,''), check_in_time, check_type, source, COALESCE(location,''), COALESCE(remark,''), check_date, COALESCE(created_at,'1970-01-01'), COALESCE(company_id,'default') as company_id`

const listSQL = `SELECT ` + attCols + ` FROM attendance_records
WHERE ($1::date IS NULL OR check_date = $1)
ORDER BY check_in_time DESC LIMIT $2 OFFSET $3`

const insertSQL = `INSERT INTO attendance_records (id, employee_code, employee_name, department, check_in_time, check_type, source, location, remark, check_date)
VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7, NULLIF($8,''), NULLIF($9,''), $10)
ON CONFLICT (employee_name, check_in_time) DO NOTHING`

func (h *Handler) AttendancePage(c *gin.Context) {
	ctx := c.Request.Context()
	page := shared.GetPage(c)
	date := c.Query("date")

	var (
		records []models.AttendanceRecord
		count   int64
		err     error
	)
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		var rows *sql.Rows
		var e error
		if date == "" {
			rows, e = h.db.QueryContext(ctx, listSQL, nil, 20, (page-1)*20)
		} else {
			rows, e = h.db.QueryContext(ctx, listSQL, date, 20, (page-1)*20)
		}
		if e != nil {
			err = e
			return
		}
		defer rows.Close()
		for rows.Next() {
			var r models.AttendanceRecord
			if e := rows.Scan(&r.ID, &r.EmployeeCode, &r.EmployeeName, &r.Department,
				&r.CheckInTime, &r.CheckType, &r.Source, &r.Location,
				&r.Remark, &r.CheckDate, &r.CreatedAt, &r.CompanyID); e != nil {
				err = e
				return
			}
			records = append(records, r)
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		var e error
		if date == "" {
			e = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM attendance_records").Scan(&count)
		} else {
			e = h.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM attendance_records WHERE check_date = $1", date).Scan(&count)
		}
		if e != nil {
			count = 0
		}
	}()
	<-done
	<-done
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, shared.PageResult[models.AttendanceRecord]{Items: shared.EmptySlice(records), Total: count, Page: page})
}

func (h *Handler) AttendanceImport(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		shared.JSONBadRequest(c, "请选择CSV文件")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, e := reader.ReadAll()
	if e != nil {
		shared.JSONBadRequest(c, "CSV文件解析失败")
		return
	}

	if len(records) < 2 {
		shared.JSONBadRequest(c, "CSV文件为空或无数据行")
		return
	}

	header := records[0]
	if strings.Contains(header[0], "员工姓名") {
		h.importDingTalk(c, records[1:])
	} else {
		h.importWeCom(c, records[1:])
	}
}

func (h *Handler) importWeCom(c *gin.Context, rows [][]string) {
	imported, skipped := h.importRows(c, rows, "wecom")
	shared.JSONOK(c, gin.H{
		"imported": imported,
		"skipped":  skipped,
		"message":  fmt.Sprintf("WeCom 导入完成：成功 %d 条，跳过 %d 条", imported, skipped),
	})
}

func (h *Handler) importDingTalk(c *gin.Context, rows [][]string) {
	imported, skipped := h.importRows(c, rows, "dingtalk")
	shared.JSONOK(c, gin.H{
		"imported": imported,
		"skipped":  skipped,
		"message":  fmt.Sprintf("DingTalk 导入完成：成功 %d 条，跳过 %d 条", imported, skipped),
	})
}

func (h *Handler) importRows(c *gin.Context, rows [][]string, source string) (imported, skipped int) {
	for _, row := range rows {
		if len(row) < 5 {
			skipped++
			continue
		}
		name := strings.TrimSpace(row[0])
		dept := ""
		if len(row) > 1 {
			dept = strings.TrimSpace(row[1])
		}
		dateStr := ""
		if len(row) > 2 {
			dateStr = strings.TrimSpace(row[2])
		}
		timeStr := ""
		if len(row) > 3 {
			timeStr = strings.TrimSpace(row[3])
		}
		checkType := "check_in"
		if len(row) > 4 && strings.Contains(row[4], "下班") {
			checkType = "check_out"
		}
		location := ""
		if len(row) > 5 {
			location = strings.TrimSpace(row[5])
		}
		remark := ""
		if len(row) > 6 {
			remark = strings.TrimSpace(row[6])
		}

		if name == "" || dateStr == "" || timeStr == "" {
			skipped++
			continue
		}

		checkInTime, err := time.Parse("2006-01-02 15:04:05", dateStr+" "+timeStr)
		if err != nil {
			checkInTime, err = time.Parse("2006-01-02 15:04", dateStr+" "+timeStr)
			if err != nil {
				skipped++
				continue
			}
		}
		checkDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			skipped++
			continue
		}

		employeeCode := fmt.Sprintf("EMP-%08x", hashName(name))

		_, err = h.db.ExecContext(c.Request.Context(), insertSQL,
			uuid.New(), employeeCode, name, dept, checkInTime, checkType, source,
			location, remark, checkDate,
		)
		if err != nil {
			skipped++
			continue
		}
		imported++
	}
	return imported, skipped
}

func hashName(name string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return h.Sum32()
}
