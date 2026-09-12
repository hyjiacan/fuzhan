package appconfig

import (
	"testing"
)

func TestToDTO_RoundTrip(t *testing.T) {
	cfg := Config{
		App: APPConfig{
			Name:        "test",
			Initialized: true,
		},
		Account: AccountConfig{
			Anonymous: AnonymousConfig{
				Username: "public",
			},
			ReservedUsernames: []string{"admin", "root"},
		},
		Server: ServerConfig{
			Host:   "0.0.0.0",
			HTTP:   HTTPConfig{Enabled: true, Port: 8080},
			HTTPS:  HTTPSConfig{Enabled: false, Port: 8443},
			FTP:    FTPConfig{Enabled: true, Port: 21},
			FTPS:   FTPSConfig{Enabled: false, Port: 990},
			WebDAV: WebDAVConfig{Enabled: true},
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "./data.db",
		},
		Storage: StorageConfig{
			Public: PublicStorageConfig{
				RootDirs: []DirectoryConfig{
					{Path: "/data/share", Quota: 0},
					{Path: "/data/docs", Quota: 1073741824},
				},
			},
			AllowedExtensions: []string{".jpg", ".png", ".pdf"},
			Private: PrivateStorageConfig{
				Enabled: true,
				Path:    "/data/private",
				Quota: PrivateQuotaConfig{
					GlobalQuota:  10737418240,
					PerUserQuota: 1073741824,
				},
			},
			Temp: TempConfig{
				Enabled:           true,
				Path:              "/data/temp",
				Quota:             TempFilesQuotaConfig{Global: "10G", PerIP: "500M"},
				DefaultExpireDays: 7,
				DeleteOnDownload:  true,
			},
		},
		Upload: UploadConfig{
			ChunkSize:   10485760,
			MaxFileSize: 17179869184,
		},
		Preview: PreviewConfig{
			AllowMimes:    "text/*,image/*",
			AllowExts:     "txt,md,log",
			MaxInlineSize: "1M",
			TextChunkSize: "100K",
		},
	}

	dto := cfg.ToDTO()

	// Verify app fields
	if dto.App.Name != "test" {
		t.Errorf("App.Name = %q, want %q", dto.App.Name, "test")
	}
	if !dto.App.Initialized {
		t.Error("App.Initialized = false, want true")
	}

	// Verify account fields
	if dto.Account.Anonymous.Username != "public" {
		t.Errorf("Account.Anonymous.Username = %q, want %q", dto.Account.Anonymous.Username, "public")
	}
	if len(dto.Account.ReservedUsernames) != 2 {
		t.Errorf("len(Account.ReservedUsernames) = %d, want 2", len(dto.Account.ReservedUsernames))
	}

	// Verify server fields
	if dto.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", dto.Server.Host, "0.0.0.0")
	}
	if !dto.Server.HTTP.Enabled || dto.Server.HTTP.Port != 8080 {
		t.Errorf("Server.HTTP = %+v", dto.Server.HTTP)
	}
	if !dto.Server.FTP.Enabled || dto.Server.FTP.Port != 21 {
		t.Errorf("Server.FTP = %+v", dto.Server.FTP)
	}
	if !dto.Server.WebDAV.Enabled {
		t.Errorf("Server.WebDAV = %+v", dto.Server.WebDAV)
	}

	// Verify root dirs (ToDTO extracts name from path)
	if len(dto.RootDirs) != 2 {
		t.Fatalf("len(RootDirs) = %d, want 2", len(dto.RootDirs))
	}
	if dto.RootDirs[0].Name != "share" || dto.RootDirs[0].FullPath != "/data/share" {
		t.Errorf("RootDirs[0] = %+v", dto.RootDirs[0])
	}
	if dto.RootDirs[1].Name != "docs" || dto.RootDirs[1].FullPath != "/data/docs" {
		t.Errorf("RootDirs[1] = %+v", dto.RootDirs[1])
	}

	// Verify dto_test.go old test still passes
	// Name is derived from path (filepath.Base)

	// Verify allowed extensions
	if len(dto.AllowedExtensions) != 3 {
		t.Fatalf("len(AllowedExtensions) = %d, want 3", len(dto.AllowedExtensions))
	}

	// Verify private storage (maps to PrivateFiles in DTO)
	if !dto.PrivateFiles.Enabled || dto.PrivateFiles.Path != "/data/private" {
		t.Errorf("PrivateFiles (private) = %+v", dto.PrivateFiles)
	}
	if dto.PrivateFiles.QuotaGlobal != 10737418240 || dto.PrivateFiles.QuotaPerUser != 1073741824 {
		t.Errorf("PrivateFiles (private) quota = global:%d, user:%d", dto.PrivateFiles.QuotaGlobal, dto.PrivateFiles.QuotaPerUser)
	}

	// Verify temp files
	if !dto.TempFiles.Enabled || dto.TempFiles.Path != "/data/temp" {
		t.Errorf("TempFiles = %+v", dto.TempFiles)
	}
	if dto.TempFiles.DefaultExpireDays != 7 || !dto.TempFiles.DeleteOnDownload {
		t.Errorf("TempFiles expire/delete = %+v", dto.TempFiles)
	}

	// Verify upload
	if dto.Upload.ChunkSize != 10485760 || dto.Upload.MaxFileSize != 17179869184 {
		t.Errorf("Upload = %+v", dto.Upload)
	}

	// Verify preview
	if dto.Preview.AllowMimes != "text/*,image/*" {
		t.Errorf("Preview.AllowMimes = %q", dto.Preview.AllowMimes)
	}

	// Round-trip: DTO -> Config
	cfg2 := ConfigFromDTO(dto)

	if cfg2.App.Name != "test" || !cfg2.App.Initialized {
		t.Errorf("After round-trip App = %+v", cfg2.App)
	}
	if len(cfg2.Storage.Public.RootDirs) != 2 {
		t.Fatalf("After round-trip len(RootDirs) = %d", len(cfg2.Storage.Public.RootDirs))
	}
	if cfg2.Storage.Public.RootDirs[0].Path != "/data/share" {
		t.Errorf("After round-trip RootDirs[0].Path = %q", cfg2.Storage.Public.RootDirs[0].Path)
	}
	if !cfg2.Storage.Private.Enabled || cfg2.Storage.Private.Path != "/data/private" {
		t.Errorf("After round-trip Private = %+v", cfg2.Storage.Private)
	}
	if !cfg2.Storage.Temp.Enabled || cfg2.Storage.Temp.DefaultExpireDays != 7 {
		t.Errorf("After round-trip Temp = %+v", cfg2.Storage.Temp)
	}
	if len(cfg2.Storage.AllowedExtensions) != 3 {
		t.Errorf("After round-trip AllowedExtensions = %v", cfg2.Storage.AllowedExtensions)
	}
}

func TestToDTO_EmptyConfig(t *testing.T) {
	cfg := Config{}
	dto := cfg.ToDTO()

	if len(dto.RootDirs) != 0 {
		t.Errorf("Expected 0 RootDirs, got %d", len(dto.RootDirs))
	}
	if dto.Server.Host != "" {
		t.Errorf("Expected empty Server.Host, got %q", dto.Server.Host)
	}
}

func TestToDTO_SingleRootDir(t *testing.T) {
	cfg := Config{
		Storage: StorageConfig{
			Public: PublicStorageConfig{
				RootDirs: []DirectoryConfig{
					{Path: "root"},
				},
			},
		},
	}
	dto := cfg.ToDTO()
	if len(dto.RootDirs) != 1 {
		t.Fatalf("len(RootDirs) = %d, want 1", len(dto.RootDirs))
	}
	// No path separator, so name equals path
	if dto.RootDirs[0].Name != "root" || dto.RootDirs[0].Path != "root" {
		t.Errorf("RootDir = %+v", dto.RootDirs[0])
	}
}

func TestConfigFromDTO_EmptyRootDirs(t *testing.T) {
	dto := ConfigDTO{
		RootDirs: nil,
	}
	cfg := ConfigFromDTO(dto)
	if cfg.Storage.Public.RootDirs != nil {
		t.Errorf("Expected nil RootDirs, got %+v", cfg.Storage.Public.RootDirs)
	}
}

func TestConfigFromDTO_RootDirWithoutFullPath(t *testing.T) {
	dto := ConfigDTO{
		RootDirs: []RootDirConfig{
			{Name: "mydir", Path: "mydir", FullPath: ""},
		},
	}
	cfg := ConfigFromDTO(dto)
	if len(cfg.Storage.Public.RootDirs) != 1 {
		t.Fatalf("len(RootDirs) = %d", len(cfg.Storage.Public.RootDirs))
	}
	// FullPath empty, should fallback to Name
	if cfg.Storage.Public.RootDirs[0].Path != "mydir" {
		t.Errorf("RootDir[0].Path = %q, want %q", cfg.Storage.Public.RootDirs[0].Path, "mydir")
	}
}

func TestFormatSizeToString(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{100, "100B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1572864, "1.5M"},
		{1073741824, "1.0G"},
		{1610612736, "1.5G"},
		{1099511627776, "1.0T"},
	}
	for _, tt := range tests {
		got := formatSizeToString(tt.input)
		if got != tt.want {
			t.Errorf("formatSizeToString(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseOrDefault(t *testing.T) {
	tests := []struct {
		input string
		def   int64
		want  int64
	}{
		{"", 100, 100},
		{"1K", 0, 1024},
		{"1M", 0, 1048576},
		{"invalid", 999, 999},
	}
	for _, tt := range tests {
		got := parseOrDefault(tt.input, tt.def)
		if got != tt.want {
			t.Errorf("parseOrDefault(%q, %d) = %d, want %d", tt.input, tt.def, got, tt.want)
		}
	}
}

// TestSecurityConfigDTORoundTrip 验证安全配置（信任代理头 / CORS）在
// ToDTO -> ConfigFromDTO 链路上无损往返，这也是 GET /api/v1/config 与
// 设置页加载/保存所依赖的序列化路径。
func TestSecurityConfigDTORoundTrip(t *testing.T) {
	cfg := Config{
		Security: SecurityConfig{
			TrustProxy:     true,
			AllowedOrigins: []string{"https://a.example.com", "https://b.example.com"},
		},
	}

	dto := cfg.ToDTO()
	if !dto.Security.TrustProxy {
		t.Error("ToDTO: TrustProxy = false, want true")
	}
	if want := []string{"https://a.example.com", "https://b.example.com"}; !equalStrings(dto.Security.AllowedOrigins, want) {
		t.Errorf("ToDTO: AllowedOrigins = %v, want %v", dto.Security.AllowedOrigins, want)
	}

	restored := ConfigFromDTO(dto)
	if restored.Security.TrustProxy != true {
		t.Errorf("ConfigFromDTO: TrustProxy = %v, want true", restored.Security.TrustProxy)
	}
	if !equalStrings(restored.Security.AllowedOrigins, dto.Security.AllowedOrigins) {
		t.Errorf("ConfigFromDTO: AllowedOrigins = %v, want %v", restored.Security.AllowedOrigins, dto.Security.AllowedOrigins)
	}
}

// TestSecurityConfigDTODefaults 验证安全配置零值往返：默认关闭信任代理、
// 来源为空列表，保证未配置时前端拿到稳定默认值。
func TestSecurityConfigDTODefaults(t *testing.T) {
	cfg := Config{} // 零值
	dto := cfg.ToDTO()
	if dto.Security.TrustProxy {
		t.Error("ToDTO(零值): TrustProxy = true, want false")
	}
	if len(dto.Security.AllowedOrigins) != 0 {
		t.Errorf("ToDTO(零值): AllowedOrigins = %v, want empty", dto.Security.AllowedOrigins)
	}
	restored := ConfigFromDTO(dto)
	if restored.Security.TrustProxy {
		t.Error("ConfigFromDTO(零值): TrustProxy = true, want false")
	}
}

// equalStrings 按顺序比较两个字符串切片。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
