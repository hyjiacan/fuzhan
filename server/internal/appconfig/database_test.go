package appconfig

import (
	"os"
	"testing"
)

func TestInitDB_UnsupportedDriver(t *testing.T) {
	cfg := &DatabaseConfig{
		Driver: "unsupported",
		DSN:    "test",
	}

	_, err := InitDB(cfg)
	if err == nil {
		t.Error("预期返回错误，但得到了 nil")
	}
}

// TestCloseDB_Nil 测试 CloseDB 对 nil 数据库的行为
func TestCloseDB_Nil(t *testing.T) {
	// nil 数据库应该不返回错误
	if err := CloseDB(); err != nil {
		t.Errorf("CloseDB 对 nil 数据库应返回 nil: %v", err)
	}
}

func TestDatabaseConfig_Fields(t *testing.T) {
	cfg := DatabaseConfig{
		Driver: "sqlite",
		DSN:    "test.db",
	}

	if cfg.Driver != "sqlite" {
		t.Errorf("Driver 字段不正确: %s", cfg.Driver)
	}
	if cfg.DSN != "test.db" {
		t.Errorf("DSN 字段不正确: %s", cfg.DSN)
	}
}

// TestInitDB_Integration 需要 CGO 的集成测试
// 在有 CGO 环境的机器上运行: CGO_ENABLED=1 go test -run TestInitDB_Integration ./config/...
func TestInitDB_Integration(t *testing.T) {
	t.Skip("需要 CGO 支持 (gcc)，在 Windows 上跳过")

	// 创建临时数据库文件
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &DatabaseConfig{
		Driver: "sqlite",
		DSN:    tmpFile.Name(),
	}

	db, err := InitDB(cfg)
	if err != nil {
		t.Fatalf("SQLite 连接失败: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层数据库连接失败: %v", err)
	}
	sqlDB.Close()
}
