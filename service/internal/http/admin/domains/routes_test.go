package domains

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRoutesRegisterNoConflict 确认域名与证书的路由能注册成功。
// 重点：/domains/:id（参数段）与 /domain-health、/domain-certificates、/domain-releases（独立顶层）
// 不在同一层，gin 不会因「静态段与参数段同层」而 panic。
func TestRoutesRegisterNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("路由注册 panic（疑似路径冲突）: %v", r)
		}
	}()
	(&Handlers{}).Routes(gin.New())
}
