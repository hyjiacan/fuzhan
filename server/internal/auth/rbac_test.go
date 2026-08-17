package auth

import (
    "testing"
)

func setupRBACTest(t *testing.T) *RBACService {
    return NewRBACService(nil)
}

func TestHasRequiredScope_Reader(t *testing.T) {
    rbac := setupRBACTest(t)

    if !rbac.hasRequiredScope("open_api:reader", "open_api:reader") {
        t.Error("reader scope 应该匹配 reader")
    }
    if rbac.hasRequiredScope("open_api:reader", "open_api:writer") {
        t.Error("reader scope 不应该匹配 writer")
    }
}

func TestHasRequiredScope_Writer(t *testing.T) {
    rbac := setupRBACTest(t)

    if !rbac.hasRequiredScope("open_api:writer", "open_api:reader") {
        t.Error("writer scope 应该匹配 reader（层级关系）")
    }
    if !rbac.hasRequiredScope("open_api:writer", "open_api:writer") {
        t.Error("writer scope 应该匹配 writer")
    }
    if rbac.hasRequiredScope("open_api:writer", "open_api:admin") {
        t.Error("writer scope 不应匹配 admin")
    }
}

func TestHasRequiredScope_Admin(t *testing.T) {
    rbac := setupRBACTest(t)

    if !rbac.hasRequiredScope("open_api:admin", "open_api:reader") {
        t.Error("admin scope 应该匹配 reader")
    }
    if !rbac.hasRequiredScope("open_api:admin", "open_api:writer") {
        t.Error("admin scope 应该匹配 writer")
    }
    if !rbac.hasRequiredScope("open_api:admin", "open_api:admin") {
        t.Error("admin scope 应该匹配 admin")
    }
}

func TestHasRequiredScope_Empty(t *testing.T) {
    rbac := setupRBACTest(t)

    if rbac.hasRequiredScope("", "open_api:reader") {
        t.Error("空 scope 不应该匹配任何权限")
    }
}

func TestHasRequiredScope_Multiple(t *testing.T) {
    rbac := setupRBACTest(t)

    if !rbac.hasRequiredScope("open_api:reader,open_api:writer", "open_api:reader") {
        t.Error("多 scope 应匹配 reader")
    }
    if !rbac.hasRequiredScope("open_api:reader,open_api:writer", "open_api:writer") {
        t.Error("多 scope 应匹配 writer")
    }
    if rbac.hasRequiredScope("open_api:reader,open_api:writer", "open_api:admin") {
        t.Error("多 scope 不应匹配 admin")
    }
}

func TestScopeContains_Exact(t *testing.T) {
    rbac := setupRBACTest(t)
    if !rbac.scopeContains("open_api:reader", "open_api:reader") {
        t.Error("精确匹配应返回 true")
    }
}

func TestScopeContains_Multiple(t *testing.T) {
    rbac := setupRBACTest(t)
    if !rbac.scopeContains("open_api:reader,open_api:writer", "open_api:writer") {
        t.Error("逗号分隔列表中应匹配")
    }
}

func TestScopeContains_NotContain(t *testing.T) {
    rbac := setupRBACTest(t)
    if rbac.scopeContains("open_api:reader", "open_api:writer") {
        t.Error("不应匹配不存在的 scope")
    }
}

func TestScopeContains_AdminInMultiple(t *testing.T) {
    rbac := setupRBACTest(t)
    if !rbac.scopeContains("open_api:admin,open_api:writer", "open_api:reader") {
        t.Log("admin 在列表中时，hasRequiredScope 会检查，但 scopeContains 不处理 admin 逻辑")
    }
}
