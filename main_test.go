package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/db"
	"github.com/nphq/starocean/internal/hooks"
	"github.com/nphq/starocean/internal/middleware"
	"github.com/nphq/starocean/internal/server"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
)

const perfDBURL = "postgres://starocean:starocean@localhost:5432/starocean_perf?sslmode=disable"

// testDSN 测试数据库连接串: 缺省使用 t.TempDir() 下的 SQLite 临时库
// （零依赖，裸 `go test ./...` 也能真正执行，避免无库静默 Skip 造成假绿），
// 可通过 STAROCEAN_TEST_DSN=postgres://... 切换到 PostgreSQL。
func testDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("STAROCEAN_TEST_DSN"); v != "" {
		return v
	}
	return "sqlite:" + filepath.Join(t.TempDir(), "starocean-test.db")
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := testDSN(t)
	database, err := db.Connect(dbURL)
	if err != nil {
		// 显式指定的 DSN 连不上属于配置错误，必须失败而非 Skip；
		// 缺省 SQLite 临时库本地一定可用，失败同样直接报错。
		t.Fatalf("test database unavailable (DSN=%q): %v", dbURL, err)
	}
	if err := db.Migrate(database, dbURL, migrationsFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database.Exec(`DELETE FROM gl_voucher_lines`)
	database.Exec(`DELETE FROM gl_vouchers`)
	database.Exec(`DELETE FROM gl_account_balances`)
	database.Exec(`DELETE FROM gl_voucher_seq`)
	database.Exec(`UPDATE gl_periods SET status='open', closed_at=NULL, closed_by=''`)
	database.Exec(`DELETE FROM finance_clearings`)
	database.Exec(`DELETE FROM payments`)
	database.Exec(`DELETE FROM sales_order_items`)
	database.Exec(`DELETE FROM purchase_order_items`)
	database.Exec(`DELETE FROM inventory_movements`)
	database.Exec(`DELETE FROM sales_orders`)
	database.Exec(`DELETE FROM purchase_orders`)
	database.Exec(`DELETE FROM products`)
	database.Exec(`DELETE FROM suppliers`)
	database.Exec(`DELETE FROM customers`)
	database.Exec(`DELETE FROM users`)

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	database.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (gen_random_uuid(), 'admin', $1, 'admin') ON CONFLICT DO NOTHING`, string(hash))
	database.Exec(`INSERT INTO customers (id, code, name) VALUES (gen_random_uuid(), 'T1', 'Test Co') ON CONFLICT DO NOTHING`)
	database.Exec(`INSERT INTO products (id, code, name, sale_price, cost_price, safety_stock, current_stock) VALUES (gen_random_uuid(), 'P1', 'Widget', '10.00', '5.00', 10, 50) ON CONFLICT DO NOTHING`)
	return database
}

func testRouter(t testing.TB, database *sql.DB) *gin.Engine {
	t.Helper()
	hooks.Default = hooks.NewRegistry()
	middleware.SetSecretKey("test-secret-key-32-chars-long!!")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.SessionMiddleware())
	server.RegisterRoutes(r, database, publicFS, "StarOcean")
	return r
}

func loginSession(t *testing.T, router *gin.Engine) *http.Cookie {
	t.Helper()
	body := `{"username":"admin","password":"admin"}`
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d body=%s", w.Code, truncate(w.Body.String(), 200))
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "starocean_sess" {
			return c
		}
	}
	t.Fatal("no session cookie after login")
	return nil
}

func authJSON(method, path string, body any, cookie *http.Cookie) *http.Request {
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func jsonBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("json: %v body=%s", err, truncate(w.Body.String(), 200))
	}
	return m
}

func TestLoginLogout(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)

	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", w.Code)
	}
	var sessCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "starocean_sess" {
			sessCookie = c
		}
	}
	if sessCookie == nil {
		t.Fatal("no session cookie set after login")
	}

	req2 := httptest.NewRequest("GET", "/api/me", nil)
	req2.AddCookie(sessCookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("/api/me: expected 200, got %d (body: %s)", w2.Code, truncate(w2.Body.String(), 200))
	}
	me := jsonBody(t, w2)
	if me["username"] != "admin" {
		t.Errorf("me.username: got %v", me["username"])
	}

	bad := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	bad.Header.Set("Content-Type", "application/json")
	wb := httptest.NewRecorder()
	r.ServeHTTP(wb, bad)
	if wb.Code != http.StatusUnauthorized {
		t.Errorf("bad login: expected 401, got %d", wb.Code)
	}

	unauth := httptest.NewRequest("GET", "/api/products", nil)
	wu := httptest.NewRecorder()
	r.ServeHTTP(wu, unauth)
	if wu.Code != http.StatusUnauthorized {
		t.Errorf("unauth products: expected 401, got %d", wu.Code)
	}

	req3 := httptest.NewRequest("POST", "/api/logout", nil)
	req3.AddCookie(sessCookie)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("logout: expected 200, got %d", w3.Code)
	}
}

func TestProductCRUD(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	req := authJSON("POST", "/api/products", map[string]any{
		"code": "SKU-X", "name": "轴承", "category": "配件", "unit": "个",
		"sale_price": "12.50", "cost_price": "6.00", "safety_stock": 5,
	}, sess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	created := jsonBody(t, w)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("create: missing id")
	}

	req2 := authJSON("GET", "/api/products/"+id, nil, sess)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w2.Code)
	}
	got := jsonBody(t, w2)
	if got["name"] != "轴承" {
		t.Errorf("name: got %v", got["name"])
	}

	req3 := authJSON("PUT", "/api/products/"+id, map[string]any{
		"name": "精密轴承", "category": "配件", "unit": "个",
		"sale_price": "15.00", "cost_price": "6.00", "safety_stock": 8,
	}, sess)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d body=%s", w3.Code, w3.Body.String())
	}

	req5 := authJSON("GET", "/api/products", nil, sess)
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w5.Code)
	}

	req6 := authJSON("DELETE", "/api/products/"+id, nil, sess)
	w6 := httptest.NewRecorder()
	r.ServeHTTP(w6, req6)
	if w6.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", w6.Code)
	}

	var p1 string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&p1)
	req4 := authJSON("POST", "/api/products/"+p1+"/stock", map[string]any{"quantity": 3, "type": "in"}, sess)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("stock: expected 200, got %d body=%s", w4.Code, w4.Body.String())
	}
}

func createSalesOrder(t *testing.T, r *gin.Engine, sess *http.Cookie, database *sql.DB, qty int) string {
	t.Helper()
	var productID, customerID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	database.QueryRow(`SELECT id FROM customers WHERE code = 'T1'`).Scan(&customerID)

	req := authJSON("POST", "/api/sales", map[string]any{
		"customer_id": customerID,
		"order_date":  "2026-05-23",
		"items": []map[string]any{{
			"product_id": productID,
			"quantity":   qty,
			"unit_price": "10.00",
		}},
	}, sess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create order: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	m := jsonBody(t, w)
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("create order: missing id")
	}
	return id
}

func TestOrderConfirmCancelStockReversal(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var productID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	orderID := createSalesOrder(t, r, sess, database, 3)

	var orderStatus string
	database.QueryRow(`SELECT status FROM sales_orders WHERE id = $1`, orderID).Scan(&orderStatus)
	if orderStatus != "draft" {
		t.Fatalf("new order status: expected draft, got %s", orderStatus)
	}

	var initialStock int32
	database.QueryRow(`SELECT current_stock FROM products WHERE code = 'P1'`).Scan(&initialStock)

	req2 := authJSON("POST", "/api/sales/"+orderID+"/confirm", nil, sess)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (body: %s)", w2.Code, truncate(w2.Body.String(), 200))
	}

	var afterConfirmStock int32
	database.QueryRow(`SELECT current_stock FROM products WHERE code = 'P1'`).Scan(&afterConfirmStock)
	if afterConfirmStock != initialStock-3 {
		t.Errorf("stock after confirm: expected %d, got %d", initialStock-3, afterConfirmStock)
	}

	var movementCount int
	database.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE product_id = $1 AND type = 'out'`, productID).Scan(&movementCount)
	if movementCount == 0 {
		t.Error("no out movement recorded after confirm")
	}

	req3 := authJSON("POST", "/api/sales/"+orderID+"/cancel", nil, sess)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d", w3.Code)
	}

	var afterCancelStock int32
	database.QueryRow(`SELECT current_stock FROM products WHERE code = 'P1'`).Scan(&afterCancelStock)
	if afterCancelStock != initialStock {
		t.Errorf("stock after cancel: expected %d (reversed back), got %d", initialStock, afterCancelStock)
	}

	database.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE product_id = $1 AND type = 'in' AND reference_type = 'sales_order_cancel'`, productID).Scan(&movementCount)
	if movementCount == 0 {
		t.Error("no in movement recorded after cancel")
	}
}

func TestPaymentCreatesClearingAndBackfillsPartner(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var customerID string
	database.QueryRow(`SELECT id FROM customers WHERE code = 'T1'`).Scan(&customerID)

	orderID := createSalesOrder(t, r, sess, database, 3) // 3 * 10.00 = 30.00

	// 挂单收款：reference 指向订单
	req := authJSON("POST", "/api/finance/payments", map[string]any{
		"type":           "收入",
		"amount":         "30.00",
		"partner_name":   "Test Co",
		"reference_type": "sales_order",
		"reference_id":   orderID,
	}, sess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("payment: expected 201, got %d body=%s", w.Code, truncate(w.Body.String(), 200))
	}

	// 核销明细在同一事务生成
	var clearingCount int
	database.QueryRow(`SELECT COUNT(*) FROM finance_clearings WHERE doc_id = $1 AND doc_type = 'sales_order'`, orderID).Scan(&clearingCount)
	if clearingCount != 1 {
		t.Fatalf("expected 1 clearing row, got %d", clearingCount)
	}

	// 付款伴侣外键回填
	var partnerType, partnerID string
	var paymentID string
	database.QueryRow(`SELECT id, partner_type, partner_id FROM payments WHERE reference_id = $1 AND reference_type = 'sales_order'`, orderID).Scan(&paymentID, &partnerType, &partnerID)
	if partnerType != "customer" || partnerID != customerID {
		t.Fatalf("expected partner customer/%s, got %s/%s", customerID, partnerType, partnerID)
	}

	// paid_amount 增量（数字语义不变）
	var paid float64
	database.QueryRow(`SELECT COALESCE(paid_amount,0) FROM sales_orders WHERE id = $1`, orderID).Scan(&paid)
	if paid != 30.0 {
		t.Fatalf("expected paid_amount 30.00, got %v", paid)
	}

	// 明细查询：/finance/clearings
	req2 := authJSON("GET", "/api/finance/clearings?doc_id="+orderID, nil, sess)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("clearings: expected 200, got %d", w2.Code)
	}
	body := jsonBody(t, w2)
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 clearing item, got %d", len(items))
	}

	// 付款详情含核销
	req3 := authJSON("GET", "/api/finance/payments/"+paymentID, nil, sess)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("payment detail: expected 200, got %d", w3.Code)
	}
	body3 := jsonBody(t, w3)
	cl, _ := body3["clearings"].([]any)
	if len(cl) != 1 {
		t.Fatalf("payment detail: expected 1 clearing, got %d", len(cl))
	}
}

func TestCreditLimitEnforcement(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var productID, customerID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	database.QueryRow(`SELECT id FROM customers WHERE code = 'T1'`).Scan(&customerID)
	database.Exec(`UPDATE customers SET credit_limit = '100.00', balance = '95.00' WHERE code = 'T1'`)

	req := authJSON("POST", "/api/sales", map[string]any{
		"customer_id": customerID,
		"order_date":  "2026-05-23",
		"items": []map[string]any{{
			"product_id": productID,
			"quantity":   2,
			"unit_price": "10.00",
		}},
	}, sess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusCreated || w.Code == http.StatusOK {
		t.Error("order should be rejected: balance(95) + new_order(20) = 115 > credit_limit(100)")
	}
}

func TestConcurrentSalesConfirm(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var productID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	orderID := createSalesOrder(t, r, sess, database, 2)

	var initialStock int32
	database.QueryRow(`SELECT current_stock FROM products WHERE code = 'P1'`).Scan(&initialStock)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	bodies := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := authJSON("POST", "/api/sales/"+orderID+"/confirm", nil, sess)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			codes[i] = w.Code
			bodies[i] = w.Body.String()
		}(i)
	}
	wg.Wait()
	t.Logf("confirm responses: codes=%v bodies=%v", codes, bodies)

	okCount := 0
	for _, c := range codes {
		if c == http.StatusOK {
			okCount++
		}
	}
	if okCount != 1 {
		t.Errorf("concurrent confirm: expected exactly 1 success(200), got %d (codes=%v)", okCount, codes)
	}

	var afterStock int32
	database.QueryRow(`SELECT current_stock FROM products WHERE code = 'P1'`).Scan(&afterStock)
	if afterStock != initialStock-2 {
		t.Errorf("stock after concurrent confirm: expected %d, got %d", initialStock-2, afterStock)
	}

	var movementCount int
	database.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE product_id = $1 AND type = 'out' AND reference_type = 'sales_order'`, productID).Scan(&movementCount)
	if movementCount != 1 {
		dump, _ := database.Query(`SELECT product_id, type, quantity, reference_type, reference_id, before_stock, after_stock FROM inventory_movements`)
		defer dump.Close()
		var lines []string
		for dump.Next() {
			var pid string
			var typ, refType string
			var qty, before, after int32
			var refID any
			dump.Scan(&pid, &typ, &qty, &refType, &refID, &before, &after)
			lines = append(lines, fmt.Sprintf("%s/%s qty=%d ref=%v %d->%d", pid[:8], typ, qty, refType, before, after))
		}
		t.Errorf("expected exactly 1 out movement, got %d (all: %v)", movementCount, lines)
	}

	req3 := authJSON("POST", "/api/sales/"+orderID+"/confirm", nil, sess)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code == http.StatusOK {
		t.Error("third sequential confirm should be rejected")
	}
	var finalStock int32
	database.QueryRow(`SELECT current_stock FROM products WHERE code = 'P1'`).Scan(&finalStock)
	if finalStock != afterStock {
		t.Errorf("stock changed by rejected confirm: %d -> %d", afterStock, finalStock)
	}
}

func TestConcurrentPaymentAllocation(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var productID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	orderID := createSalesOrder(t, r, sess, database, 2)

	req2 := authJSON("POST", "/api/sales/"+orderID+"/confirm", nil, sess)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d", w2.Code)
	}

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := authJSON("POST", "/api/finance/payments", map[string]any{
				"type":           "收入",
				"amount":         "10.00",
				"reference_type": "sales_order",
				"reference_id":   orderID,
			}, sess)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusCreated {
			t.Errorf("payment %d: expected 201, got %d", i, c)
		}
	}

	var paid, total decimal.Decimal
	database.QueryRow(`SELECT paid_amount, total_amount FROM sales_orders WHERE id = $1`, orderID).Scan(&paid, &total)
	if paid.String() != "20" {
		t.Errorf("paid_amount after 2 concurrent payments: expected 20, got %s", paid.String())
	}
	if paid.GreaterThan(total) {
		t.Errorf("paid_amount (%s) must not exceed total (%s)", paid, total)
	}

	req3 := authJSON("POST", "/api/finance/payments", map[string]any{
		"type":           "收入",
		"amount":         "0.01",
		"reference_type": "sales_order",
		"reference_id":   orderID,
	}, sess)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code == http.StatusCreated || w3.Code == http.StatusOK {
		t.Error("over-payment should be rejected")
	}
	var paidAfter decimal.Decimal
	database.QueryRow(`SELECT paid_amount FROM sales_orders WHERE id = $1`, orderID).Scan(&paidAfter)
	if paidAfter.String() != "20" {
		t.Errorf("paid_amount after rejected over-payment: expected 20, got %s", paidAfter.String())
	}
}

var (
	perfDB     *sql.DB
	perfDBOnce sync.Once
)

func ensurePerfDB(b testing.TB) *sql.DB {
	b.Helper()
	perfDBOnce.Do(func() {
		database, err := sql.Open("pgx", perfDBURL)
		if err != nil {
			b.Skipf("perf DB open: %v", err)
		}
		if err := database.Ping(); err != nil {
			b.Skipf("perf DB ping failed (run 'make perf-seed'): %v", err)
		}
		if err := db.Migrate(database, perfDBURL, migrationsFS); err != nil {
			b.Fatalf("migrate perf: %v", err)
		}
		var count int64
		database.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&count)
		if count < 500000 {
			b.Skipf("need >= 100k products in starocean_perf (have %d). Run: make perf-seed", count)
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		database.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (gen_random_uuid(), 'admin', $1, 'admin') ON CONFLICT (username) DO NOTHING`, string(hash))
		perfDB = database
	})
	return perfDB
}

func TestProductSearchBaseline(t *testing.T) {
	database := ensurePerfDB(t)
	defer database.Close()

	r := testRouter(t, database)
	sess := loginSession(t, r)

	cases := []struct {
		name    string
		keyword string
		maxMS   int
	}{
		{"chinese_short", "工程密封", 50},
		{"chinese_long", "工程密封件铜材", 50},
		{"code_prefix", "SKU-00", 50},
		{"code_exact", "PSK-0001000", 100},
		{"no_match", "不存在的内容xyz", 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/products/search?q=" + url.QueryEscape(tc.keyword)
			req := httptest.NewRequest("GET", path, nil)
			req.AddCookie(sess)
			w := httptest.NewRecorder()

			start := time.Now()
			r.ServeHTTP(w, req)
			elapsed := time.Since(start)

			if w.Code != http.StatusOK {
				t.Fatalf("got %d", w.Code)
			}
			if elapsed.Milliseconds() > int64(tc.maxMS) {
				t.Errorf("%s: %dms > %dms threshold (keyword=%q)", tc.name, elapsed.Milliseconds(), tc.maxMS, tc.keyword)
			} else {
				t.Logf("%s: %dms (keyword=%q)", tc.name, elapsed.Milliseconds(), tc.keyword)
			}
		})
	}

	t.Run("page_first", func(t *testing.T) {
		start := time.Now()
		rows, err := database.QueryContext(context.Background(),
			`SELECT id, code, name, COALESCE(category,''), COALESCE(unit,''),
			        COALESCE(CAST(sale_price AS text),'0'), COALESCE(CAST(cost_price AS text),'0'),
			        COALESCE(safety_stock,0), COALESCE(current_stock,0),
			        COALESCE(created_at, '1970-01-01'::timestamptz)
			 FROM products ORDER BY created_at DESC NULLS LAST, id DESC LIMIT 20`)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		rows.Close()
		if elapsed.Milliseconds() > 100 {
			t.Errorf("page_first: %dms > 100ms", elapsed.Milliseconds())
		} else {
			t.Logf("page_first: %dms", elapsed.Milliseconds())
		}
	})

	t.Run("page_deep_sql", func(t *testing.T) {
		var cursorAt time.Time
		var cursorID uuid.UUID
		err := database.QueryRow(`SELECT created_at, id FROM products ORDER BY created_at DESC NULLS LAST, id DESC OFFSET 249990 LIMIT 1`).Scan(&cursorAt, &cursorID)
		if err != nil {
			t.Skipf("cannot get cursor: %v", err)
		}

		start := time.Now()
		rows, err := database.QueryContext(context.Background(),
			`SELECT id, code, name, COALESCE(category,''), COALESCE(unit,''),
			        COALESCE(CAST(sale_price AS text),'0'), COALESCE(CAST(cost_price AS text),'0'),
			        COALESCE(safety_stock,0), COALESCE(current_stock,0),
			        COALESCE(created_at, '1970-01-01'::timestamptz)
			 FROM products WHERE (created_at, id) < ($1, $2)
			 ORDER BY created_at DESC NULLS LAST, id DESC LIMIT 20`, cursorAt, cursorID)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		rows.Close()
		if elapsed.Milliseconds() > 100 {
			t.Errorf("page_deep_sql: %dms > 100ms", elapsed.Milliseconds())
		} else {
			t.Logf("page_deep_sql: %dms", elapsed.Milliseconds())
		}
	})
}

func BenchmarkProductSearch(b *testing.B) {
	database := ensurePerfDB(b)
	defer database.Close()

	r := testRouter(b, database)
	sess := loginSessionB(b, r)

	queries := []struct {
		name    string
		keyword string
	}{
		{"chinese_short", "工程密封"},
		{"chinese_long", "工程密封件铜材"},
		{"code_prefix", "SKU-00"},
		{"code_exact", "PSK-0001000"},
		{"no_match", "不存在的内容xyz"},
	}

	for _, q := range queries {
		b.Run(q.name, func(b *testing.B) {
			path := "/api/products/search?q=" + url.QueryEscape(q.keyword)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest("GET", path, nil)
				req.AddCookie(sess)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatalf("got %d", w.Code)
				}
			}
		})
	}
}

func loginSessionB(b *testing.B, router *gin.Engine) *http.Cookie {
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "starocean_sess" {
			return c
		}
	}
	b.Fatal("no session cookie")
	return nil
}

func TestLedgerAutoPostAndStatements(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	r := testRouter(t, database)
	sess := loginSession(t, r)

	orderID := createSalesOrder(t, r, sess, database, 2)
	req := authJSON("POST", "/api/sales/"+orderID+"/confirm", nil, sess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", w.Code, w.Body.String())
	}

	var voucherNo, status, debit, credit string
	err := database.QueryRow(`
		SELECT voucher_no, status, debit_total::text, credit_total::text
		FROM gl_vouchers WHERE source_type='sales_order' AND source_id=$1 AND reverses_id IS NULL`, orderID).
		Scan(&voucherNo, &status, &debit, &credit)
	if err != nil {
		t.Fatalf("auto voucher: %v", err)
	}
	if status != "posted" {
		t.Fatalf("auto voucher status=%s", status)
	}
	if debit != credit {
		t.Fatalf("auto voucher unbalanced %s/%s", debit, credit)
	}

	bad := authJSON("POST", "/api/ledger/vouchers", map[string]any{
		"voucher_date": "2024-06-01",
		"summary":      "不平",
		"lines": []map[string]any{
			{"account_code": "1001", "debit": "10"},
			{"account_code": "6001", "credit": "3"},
		},
	}, sess)
	bw := httptest.NewRecorder()
	r.ServeHTTP(bw, bad)
	if bw.Code != http.StatusBadRequest {
		t.Fatalf("unbalanced voucher: expected 400, got %d %s", bw.Code, bw.Body.String())
	}

	now := time.Now()
	trial := httptest.NewRequest("GET", fmt.Sprintf("/api/ledger/books/trial?year=%d&month=%d&leaves=1", now.Year(), int(now.Month())), nil)
	trial.AddCookie(sess)
	tw := httptest.NewRecorder()
	r.ServeHTTP(tw, trial)
	if tw.Code != http.StatusOK {
		t.Fatalf("trial: %d %s", tw.Code, tw.Body.String())
	}
	body := jsonBody(t, tw)
	if body["balanced"] != true {
		t.Fatalf("trial not balanced: %v", body)
	}

	bs := httptest.NewRequest("GET", fmt.Sprintf("/api/ledger/reports/balance-sheet?year=%d&month=%d", now.Year(), int(now.Month())), nil)
	bs.AddCookie(sess)
	bsw := httptest.NewRecorder()
	r.ServeHTTP(bsw, bs)
	if bsw.Code != http.StatusOK {
		t.Fatalf("balance sheet: %d %s", bsw.Code, bsw.Body.String())
	}

	skip := authJSON("POST", "/api/ledger/periods/2024/06/close", nil, sess)
	sw := httptest.NewRecorder()
	r.ServeHTTP(sw, skip)
	if sw.Code != http.StatusBadRequest {
		t.Fatalf("skip-month close: expected 400, got %d %s", sw.Code, sw.Body.String())
	}

	closeReq := authJSON("POST", "/api/ledger/periods/2024/01/close", nil, sess)
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, closeReq)
	if cw.Code != http.StatusOK {
		t.Fatalf("close empty period: %d %s", cw.Code, cw.Body.String())
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// TestHTMLPages 服务端渲染页面冒烟：未登录跳登录页；登录后核心页 200；htmx 片段返回表格。
func TestHTMLPages(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	r := testRouter(t, database)

	anon := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, anon)
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Fatalf("anon products: expected redirect to /login, got %d %q", w.Code, w.Header().Get("Location"))
	}

	sess := loginSession(t, r)
	get := func(path string, hx bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(sess)
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		ww := httptest.NewRecorder()
		r.ServeHTTP(ww, req)
		return ww
	}
	for _, p := range []string{"/", "/products", "/customers", "/suppliers", "/sales", "/purchases",
		"/inventory", "/finance", "/ledger", "/ledger/accounts", "/ledger/vouchers", "/ledger/books",
		"/ledger/reports", "/personnel/employees", "/workreports", "/picking", "/attendance", "/search?q=test"} {
		ww := get(p, false)
		if ww.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", p, ww.Code)
			continue
		}
		if ct := ww.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("GET %s: expected text/html, got %q", p, ct)
		}
	}
	frag := get("/products?q=P1", true)
	if frag.Code != http.StatusOK || strings.Contains(frag.Body.String(), "<html") {
		t.Errorf("HX fragment: expected table fragment, got %d (has<html=%v)", frag.Code, strings.Contains(frag.Body.String(), "<html"))
	}

	// 表单登录：错误密码 401 + 错误文案；正确密码 303 跳转
	bad := httptest.NewRequest("POST", "/login", strings.NewReader("username=admin&password=wrong"))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bw := httptest.NewRecorder()
	r.ServeHTTP(bw, bad)
	if bw.Code != http.StatusUnauthorized {
		t.Errorf("bad form login: expected 401, got %d", bw.Code)
	}
	ok := httptest.NewRequest("POST", "/login", strings.NewReader("username=admin&password=admin"))
	ok.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ow := httptest.NewRecorder()
	r.ServeHTTP(ow, ok)
	if ow.Code != http.StatusSeeOther {
		t.Errorf("form login: expected 303, got %d", ow.Code)
	}
	found := false
	for _, c := range ow.Result().Cookies() {
		if c.Name == "starocean_sess" {
			found = true
		}
	}
	if !found {
		t.Errorf("form login: no session cookie")
	}
}

// TestCreditAccumulation 欠款累计：确认记应收 → 超限的新单被拦 → 回款冲减后放行 → 取消冲回。
func TestCreditAccumulation(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var productID, customerID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	database.QueryRow(`SELECT id FROM customers WHERE code = 'T1'`).Scan(&customerID)
	database.Exec(`UPDATE customers SET credit_limit = '100.00', balance = '0' WHERE code = 'T1'`)

	makeOrder := func(price string) (string, int) {
		req := authJSON("POST", "/api/sales", map[string]any{
			"customer_id": customerID,
			"order_date":  "2026-05-23",
			"items":       []map[string]any{{"product_id": productID, "quantity": 1, "unit_price": price}},
		}, sess)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			return "", w.Code
		}
		return jsonBody(t, w)["id"].(string), w.Code
	}
	confirm := func(id string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, authJSON("POST", "/api/sales/"+id+"/confirm", nil, sess))
		return w.Code
	}
	balance := func() string {
		var b string
		database.QueryRow(`SELECT balance::text FROM customers WHERE code = 'T1'`).Scan(&b)
		d, _ := decimal.NewFromString(b)
		return d.StringFixed(2)
	}

	idA, _ := makeOrder("10.00")
	if idA == "" {
		t.Fatal("order A should be created")
	}
	if code := confirm(idA); code != http.StatusOK {
		t.Fatalf("confirm A: got %d", code)
	}
	if b := balance(); b != "10.00" {
		t.Fatalf("balance after confirm A: got %s, want 10.00", b)
	}

	// 欠款 10 + 新单 95 = 105 > 100，必须拒绝
	if _, code := makeOrder("95.00"); code == http.StatusCreated {
		t.Fatal("order B should be rejected by accumulated credit")
	}

	// 全额回款 A → 欠款归零 → B 可建
	pay := authJSON("POST", "/api/finance/payments", map[string]any{
		"type": "收入", "amount": "10.00",
		"reference_type": "sales_order", "reference_id": idA,
	}, sess)
	pw := httptest.NewRecorder()
	r.ServeHTTP(pw, pay)
	if pw.Code != http.StatusCreated {
		t.Fatalf("pay A: got %d (%s)", pw.Code, truncate(pw.Body.String(), 200))
	}
	if b := balance(); b != "0.00" && b != "0" {
		t.Fatalf("balance after pay A: got %s, want 0", b)
	}
	idB, _ := makeOrder("95.00")
	if idB == "" {
		t.Fatal("order B should pass after payment")
	}

	// 取消未付款的 B（draft 直接取消，无余额变动）；确认 C 再取消验证冲回
	if code := confirm(idB); code != http.StatusOK {
		t.Fatalf("confirm B: got %d", code)
	}
	if b := balance(); b != "95.00" {
		t.Fatalf("balance after confirm B: got %s, want 95.00", b)
	}
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, authJSON("POST", "/api/sales/"+idB+"/cancel", nil, sess))
	if cw.Code != http.StatusOK {
		t.Fatalf("cancel B: got %d", cw.Code)
	}
	if b := balance(); b != "0.00" && b != "0" {
		t.Fatalf("balance after cancel B: got %s, want 0", b)
	}
}

// TestClosedPeriodBlocksPosting 已结账期间禁止倒开：确认被拦且回滚， reopen 后放行。
func TestClosedPeriodBlocksPosting(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	sess := loginSession(t, r)

	var productID, customerID string
	database.QueryRow(`SELECT id FROM products WHERE code = 'P1'`).Scan(&productID)
	database.QueryRow(`SELECT id FROM customers WHERE code = 'T1'`).Scan(&customerID)

	now := time.Now()
	database.Exec(`UPDATE gl_periods SET status='closed' WHERE year=$1 AND month=$2 AND company_id='default'`, now.Year(), int(now.Month()))

	req := authJSON("POST", "/api/sales", map[string]any{
		"customer_id": customerID,
		"order_date":  now.Format("2006-01-02"),
		"items":       []map[string]any{{"product_id": productID, "quantity": 1, "unit_price": "10.00"}},
	}, sess)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("draft create should still work in closed period, got %d", w.Code)
	}
	oid := jsonBody(t, w)["id"].(string)

	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, authJSON("POST", "/api/sales/"+oid+"/confirm", nil, sess))
	if cw.Code != http.StatusBadRequest {
		t.Fatalf("confirm in closed period: expected 400, got %d", cw.Code)
	}
	var status string
	var stock int32
	database.QueryRow(`SELECT status FROM sales_orders WHERE id=$1`, oid).Scan(&status)
	database.QueryRow(`SELECT current_stock FROM products WHERE code='P1'`).Scan(&stock)
	if status != "draft" {
		t.Fatalf("order should stay draft after blocked confirm, got %s", status)
	}
	if stock != 50 {
		t.Fatalf("stock should be untouched after blocked confirm, got %d", stock)
	}

	database.Exec(`UPDATE gl_periods SET status='open' WHERE year=$1 AND month=$2 AND company_id='default'`, now.Year(), int(now.Month()))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, authJSON("POST", "/api/sales/"+oid+"/confirm", nil, sess))
	if rw.Code != http.StatusOK {
		t.Fatalf("confirm after reopen: expected 200, got %d", rw.Code)
	}
}

// TestViewerReadOnly 只看账角色：读放行，写 403（含 HTML 表单），登出不受影响。
func TestViewerReadOnly(t *testing.T) {
	database := setupTestDB(t)
	r := testRouter(t, database)
	adminSess := loginSession(t, r)

	// 新建 viewer 用户（与 admin 同口令，role=viewer）
	database.Exec(`INSERT INTO users (id, username, password_hash, role)
		SELECT gen_random_uuid(), 'viewer1', password_hash, 'viewer' FROM users WHERE username='admin'`)

	vLogin := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"viewer1","password":"admin"}`))
	vLogin.Header.Set("Content-Type", "application/json")
	vw := httptest.NewRecorder()
	r.ServeHTTP(vw, vLogin)
	if vw.Code != http.StatusOK {
		t.Fatalf("viewer login: got %d", vw.Code)
	}
	if !strings.Contains(vw.Body.String(), `"role":"viewer"`) {
		t.Fatalf("login should return role=viewer, got %s", truncate(vw.Body.String(), 200))
	}
	var viewerSess *http.Cookie
	for _, c := range vw.Result().Cookies() {
		if c.Name == "starocean_sess" {
			viewerSess = c
		}
	}
	if viewerSess == nil {
		t.Fatal("no session cookie for viewer")
	}

	// 读：200
	for _, p := range []string{"/api/products", "/api/me", "/products", "/sales"} {
		req := httptest.NewRequest("GET", p, nil)
		req.AddCookie(viewerSess)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("viewer GET %s: expected 200, got %d", p, w.Code)
		}
	}
	// /api/me 角色回显
	meReq := httptest.NewRequest("GET", "/api/me", nil)
	meReq.AddCookie(viewerSess)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)
	if !strings.Contains(meW.Body.String(), `"role":"viewer"`) {
		t.Errorf("/api/me should echo viewer, got %s", truncate(meW.Body.String(), 200))
	}

	// 写：403（JSON 与 HTML 表单）
	badWrites := []struct{ method, path string }{
		{"POST", "/api/products"},
		{"DELETE", "/api/customers/00000000-0000-0000-0000-000000000000"},
		{"POST", "/products"},
		{"POST", "/sales"},
	}
	for _, b := range badWrites {
		req := authJSON(b.method, b.path, map[string]any{"name": "x"}, viewerSess)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s: expected 403, got %d", b.method, b.path, w.Code)
		}
	}

	// 登出不受影响
	outReq := httptest.NewRequest("POST", "/api/logout", nil)
	outReq.AddCookie(viewerSess)
	outW := httptest.NewRecorder()
	r.ServeHTTP(outW, outReq)
	if outW.Code != http.StatusOK {
		t.Errorf("viewer logout: expected 200, got %d", outW.Code)
	}

	// admin 写不受影响
	adminWrite := authJSON("POST", "/api/products", map[string]any{
		"code": "SKU-VIEWER-CHECK", "name": "viewer隔离测试",
	}, adminSess)
	aw := httptest.NewRecorder()
	r.ServeHTTP(aw, adminWrite)
	if aw.Code != http.StatusCreated {
		t.Errorf("admin write: expected 201, got %d (%s)", aw.Code, truncate(aw.Body.String(), 200))
	}
}
