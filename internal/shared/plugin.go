package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/nphq/starocean/internal/hooks"
)

func JSONConflict(c *gin.Context, msg string) {
	JSONError(c, http.StatusConflict, msg)
}

func AbortIfPluginRejected(c *gin.Context, res hooks.Result) bool {
	if !res.Aborted {
		return false
	}
	msg := res.Reason
	if msg == "" {
		msg = "插件拒绝了此操作"
	}
	JSONConflict(c, msg)
	return true
}

func FireBefore(ctx context.Context, name string, payload map[string]interface{}) hooks.Result {
	return hooks.Default.FireBefore(ctx, hooks.Event{Name: name, Payload: payload})
}

func FireAfter(name string, payload map[string]interface{}) {
	hooks.Default.FireAfter(hooks.Event{Name: name, Payload: payload})
}

// WaitHooks 等待在途 FireAfter 异步监听器收尾（进程退出前调用）。超时返回 false。
func WaitHooks(timeout time.Duration) bool {
	return hooks.Default.WaitTimeout(timeout)
}

func JSONMapArg(m map[string]interface{}) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func PropertiesFromPatch(res hooks.Result, base map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range base {
		out[k] = v
	}
	if raw, ok := res.Patches["properties"]; ok {
		if m, ok := raw.(map[string]interface{}); ok {
			for k, v := range m {
				out[k] = v
			}
		}
	}
	return out
}

var propertyTables = map[string]struct{}{
	"products":        {},
	"customers":       {},
	"suppliers":       {},
	"sales_orders":    {},
	"purchase_orders": {},
}

func MergeProperties(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}, table string, id uuid.UUID, patch map[string]interface{}) error {
	if len(patch) == 0 {
		return nil
	}
	if _, ok := propertyTables[table]; !ok {
		return fmt.Errorf("unsupported properties table")
	}
	_, err := exec.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET properties = json_patch(properties, $2), updated_at = (strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ','now')) WHERE id = $1`, table),
		id, JSONMapArg(patch))
	return err
}

func PatchString(res hooks.Result, key string) (string, bool) {
	v, ok := res.Patches[key]
	if !ok || v == nil {
		return "", false
	}
	return fmt.Sprint(v), true
}
