package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// defaultAdminPassword 演示密码，仅本地评估用；生产 seed 务必设 ADMIN_PASSWORD。
const defaultAdminPassword = "3dQAKbZHqP6P"

func Seed(database *sql.DB, productCount int, clear bool) {
	ctx := context.Background()

	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = defaultAdminPassword
		log.Println("seed: ADMIN_PASSWORD 未设置，使用演示密码（仅本地评估用，生产环境务必覆盖）")
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	_, err := database.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, role) VALUES ($1, $2, $3, 'admin') ON CONFLICT (username) DO NOTHING`,
		uuid.New(), "admin", string(hash))
	if err != nil {
		log.Printf("seed admin: %v", err)
	}

	customers := []struct{ code, name, contact, phone string }{
		{"CUST-001", "上海鑫达贸易有限公司", "张三", "13800138001"},
		{"CUST-002", "北京恒通科技有限公司", "李四", "13900139002"},
		{"CUST-003", "广州利丰商贸集团", "王五", "13700137003"},
		{"CUST-004", "深圳创维电子有限公司", "赵六", "13600136004"},
		{"CUST-005", "杭州云帆电子商务", "钱七", "13500135005"},
	}
	for _, c := range customers {
		database.ExecContext(ctx,
			`INSERT INTO customers (id, code, name, contact_person, phone) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (code) DO NOTHING`,
			uuid.New(), c.code, c.name, c.contact, c.phone)
	}

	suppliers := []struct{ code, name, contact, phone string }{
		{"SUP-001", "苏州原材料供应商", "陈一", "13100131001"},
		{"SUP-002", "天津包装材料厂", "周二", "13200132002"},
		{"SUP-003", "东莞五金配件有限公司", "吴三", "13300133003"},
	}
	for _, s := range suppliers {
		database.ExecContext(ctx,
			`INSERT INTO suppliers (id, code, name, contact_person, phone) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (code) DO NOTHING`,
			uuid.New(), s.code, s.name, s.contact, s.phone)
	}

	if clear {
		database.ExecContext(ctx, `DELETE FROM products`)
		log.Println("cleared existing products")
	}

	if productCount > 0 {
		var existing int64
		database.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&existing)
		toInsert := productCount - int(existing)
		if toInsert > 0 {
			seedBulkProducts(ctx, database, existing, toInsert)
		}
	}

	var afterCount int64
	database.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&afterCount)
	log.Printf("products: %d total", afterCount)

	baseProducts := []struct {
		code, name, category, unit string
		salePrice, costPrice       float64
		safetyStock                int
	}{
		{"SKU-001", "精密轴承 6205", "原材料", "个", 25.00, 15.00, 100},
		{"SKU-002", "不锈钢螺栓 M8×30", "原材料", "个", 2.50, 1.20, 500},
		{"SKU-003", "电子控制板 V2", "半成品", "块", 180.00, 95.00, 20},
		{"SKU-004", "铝合金外壳 A-100", "成品", "个", 350.00, 200.00, 30},
		{"SKU-005", "包装纸箱 40×30×20", "原材料", "个", 5.00, 3.00, 200},
		{"SKU-006", "密封圈 DN25", "原材料", "个", 8.00, 4.50, 300},
		{"SKU-007", "LED 显示模组 3.2寸", "半成品", "块", 120.00, 65.00, 50},
		{"SKU-008", "电动工具套装 T-PRO", "成品", "套", 899.00, 520.00, 15},
		{"SKU-009", "液压油 46#", "原材料", "桶", 280.00, 180.00, 10},
		{"SKU-010", "工业传感器 PT100", "半成品", "个", 75.00, 40.00, 25},
	}
	for _, p := range baseProducts {
		database.ExecContext(ctx,
			`INSERT INTO products (id, code, name, category, unit, sale_price, cost_price, safety_stock, current_stock)
			 VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''), $8, $9) ON CONFLICT (code) DO NOTHING`,
			uuid.New(), p.code, p.name, p.category, p.unit,
			fmt.Sprintf("%.2f", p.salePrice), fmt.Sprintf("%.2f", p.costPrice),
			p.safetyStock, rand.Intn(200)+20)
	}

	log.Println("seed data complete (admin account ready)")
}

func seedBulkProducts(ctx context.Context, db *sql.DB, startIdx int64, count int) {
	batchSize := 1000
	categories := []string{"原材料", "半成品", "成品", "包装材料", "电子元件", "机械零件", "五金工具", "化工原料", "办公用品", "劳保用品"}
	units := []string{"个", "箱", "桶", "套", "块", "卷", "根", "台", "包", "袋"}
	nameParts := []string{
		"精密", "工业", "标准", "高精", "特种", "通用", "工程", "环保", "智能", "高端",
		"轴承", "齿轮", "电机", "传感器", "控制器", "连接器", "密封件", "紧固件", "传动件", "液压件",
		"钢材", "铝材", "铜材", "塑料", "橡胶", "玻纤", "碳材", "合金", "陶瓷", "纳米",
	}

	totalBatches := (count + batchSize - 1) / batchSize
	startTime := time.Now()

	for batch := 0; batch < totalBatches; batch++ {
		from := batch * batchSize
		to := from + batchSize
		if to > count {
			to = count
		}
		batchLen := to - from

		placeholders := make([]string, batchLen)
		args := make([]interface{}, 0, batchLen*9)
		for j := 0; j < batchLen; j++ {
			idx := from + j
			n := j*9 + 1
			placeholders[j] = fmt.Sprintf("($%d,$%d,$%d,NULLIF($%d,''),NULLIF($%d,''),$%d,$%d,$%d,$%d)",
				n, n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8)
			code := fmt.Sprintf("SKU-%07d", startIdx+int64(idx)+1)
			a, b, c := nameParts[rand.Intn(10)], nameParts[rand.Intn(10)+10], nameParts[rand.Intn(10)+20]
			name := fmt.Sprintf("%s%s%s %02d", a, b, c, rand.Intn(100))
			cat := categories[rand.Intn(len(categories))]
			unit := units[rand.Intn(len(units))]
			cost := float64(rand.Intn(9000)+100) / 100.0
			sale := cost * (1.0 + float64(rand.Intn(60)+15)/100.0)
			args = append(args, uuid.New(), code, name, cat, unit, sale, cost,
				rand.Intn(500)+5, rand.Intn(1000)+10)
		}

		query := fmt.Sprintf(`INSERT INTO products (id, code, name, category, unit, sale_price, cost_price, safety_stock, current_stock) VALUES %s ON CONFLICT (code) DO NOTHING`, strings.Join(placeholders, ","))
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			log.Printf("batch %d error: %v", batch, err)
		}

		if (batch+1)%10 == 0 || batch == totalBatches-1 {
			elapsed := time.Since(startTime)
			done := to
			rate := float64(done) / elapsed.Seconds()
			log.Printf("inserted %d/%d products (%.0f/s)", done, count, rate)
		}
	}
}
