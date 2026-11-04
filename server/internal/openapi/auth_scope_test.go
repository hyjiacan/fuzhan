package openapi

import (
	"net/http/httptest"
	"testing"

	"fuzhan/internal/auth"
	"fuzhan/internal/models"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
)

// TestInjectWebSessionGrantsWriterCompatScope 防止 L6 加固后 Web 会话被空 scope 卡死：
// none 模式下 Web 会话应获得 writer 兼容层级，覆盖全部数据端点 scope。
func TestInjectWebSessionGrantsWriterCompatScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	injectWebSession(c, &jwt.Claims{UUID: "uuid-1", Username: "testuser", Role: "user"})

	rbac := auth.NewRBACService(nil)
	for _, scope := range []string{
		models.ScopeOpenAPIReader,
		models.ScopeFilesInfo,
		models.ScopeFilesUpload,
		models.ScopeFilesDuplicates,
		models.ScopeFilesExists,
		models.ScopeFilesOverwrite,
		models.ScopeFilesDelete,
		models.ScopeFilesRename,
	} {
		if !rbac.HasContextScope(c, scope) {
			t.Errorf("Web 会话应覆盖 scope %s", scope)
		}
	}
	if rbac.HasContextScope(c, models.ScopeOpenAPIAdmin) {
		t.Error("Web 会话不应获得内部 admin scope")
	}
}
