package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadConfig_LoadsAllTopLevelSections 回归测试：readConfig 必须把配置文件中
// 所有顶层节加载进 GlobalConfig。历史上 work_dir / database / ddos / security / auth
// 因临时解析结构体漏字段而被整体丢弃，导致界面保存后重启配置失效。
func TestReadConfig_LoadsAllTopLevelSections(t *testing.T) {
	content := `
work_dir: "./data"
app:
  name: 测试站点
  initialized: true
database:
  driver: mysql
  dsn: "user:pass@tcp(127.0.0.1:3306)/demo"
upload:
  chunk_size: 5MB
  max_file_size: 1GB
ddos:
  enabled: true
  window_seconds: 60
  max_requests: 120
security:
  trust_proxy: true
  allowed_origins:
    - https://a.example.com
auth:
  ldap:
    enabled: true
    host: ldap.example.com
    port: 389
    use_ssl: true
    base_dn: "dc=example,dc=com"
    bind_dn: "cn=admin,dc=example,dc=com"
    bind_password: secret
    user_filter: "(uid=%s)"
    username_attribute: uid
    auto_create_user: true
    insecure_skip_verify: true
  oauth2:
    enabled: false
    provider_id: keycloak-prod
    provider_name: oidc
    client_id: cid
    client_secret: csecret
    auth_url: https://id.example.com/auth
    token_url: https://id.example.com/token
    user_info_url: https://id.example.com/userinfo
    issuer: https://id.example.com
    scopes:
      - openid
      - profile
    redirect_url: https://demo.example.com/callback
    username_claim: sub
    display_name_claim: name
    email_claim: email
    auto_create_user: true
    insecure_skip_verify: true
`
	dir := t.TempDir()
	configFile := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(configFile, []byte(content), 0600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	origPath, origConfig := ConfigPath, GlobalConfig
	defer func() {
		ConfigPath, GlobalConfig = origPath, origConfig
	}()

	ConfigPath = configFile
	GlobalConfig = Config{}
	readConfig()

	if GlobalConfig.WorkDir != "./data" {
		t.Errorf("WorkDir = %q, want %q", GlobalConfig.WorkDir, "./data")
	}

	if GlobalConfig.Database.Driver != "mysql" || GlobalConfig.Database.DSN == "" {
		t.Errorf("Database = %+v, want driver=mysql 且 DSN 非空", GlobalConfig.Database)
	}

	if !GlobalConfig.DDos.Enabled {
		t.Errorf("DDos.Enabled = false, want true")
	}

	if !GlobalConfig.Security.TrustProxy {
		t.Errorf("Security.TrustProxy = false, want true")
	}
	if want := []string{"https://a.example.com"}; len(GlobalConfig.Security.AllowedOrigins) != 1 ||
		GlobalConfig.Security.AllowedOrigins[0] != want[0] {
		t.Errorf("Security.AllowedOrigins = %v, want %v", GlobalConfig.Security.AllowedOrigins, want)
	}

	ldap := GlobalConfig.Auth.LDAP
	if !ldap.Enabled || ldap.Host != "ldap.example.com" || ldap.Port != 389 || !ldap.UseSSL {
		t.Errorf("Auth.LDAP 基础字段未加载: %+v", ldap)
	}
	if ldap.BaseDN != "dc=example,dc=com" || ldap.BindDN == "" || ldap.BindPassword != "secret" {
		t.Errorf("Auth.LDAP 绑定字段未加载: %+v", ldap)
	}
	if ldap.UserFilter != "(uid=%s)" || ldap.UsernameAttribute != "uid" {
		t.Errorf("Auth.LDAP 用户检索字段未加载: %+v", ldap)
	}
	if !ldap.AutoCreateUser || !ldap.InsecureSkipVerify {
		t.Errorf("Auth.LDAP 自动创建/证书校验字段未加载: %+v", ldap)
	}

	oauth := GlobalConfig.Auth.OAuth2
	// LDAP 与 OAuth2 互斥：本用例只验证字段加载，OAuth2 保持关闭
	if oauth.Enabled || oauth.ClientID != "cid" || oauth.ClientSecret != "csecret" {
		t.Errorf("Auth.OAuth2 基础字段未加载: %+v", oauth)
	}
	if oauth.ProviderID != "keycloak-prod" {
		t.Errorf("Auth.OAuth2 provider_id 未加载: %+v", oauth)
	}
	if oauth.AuthURL == "" || oauth.TokenURL == "" || oauth.UserInfoURL == "" || oauth.Issuer == "" {
		t.Errorf("Auth.OAuth2 端点字段未加载: %+v", oauth)
	}
	if len(oauth.Scopes) != 2 || oauth.RedirectURL == "" {
		t.Errorf("Auth.OAuth2 scope/回调未加载: %+v", oauth)
	}
	if oauth.UsernameClaim != "sub" || oauth.DisplayNameClaim != "name" || oauth.EmailClaim != "email" {
		t.Errorf("Auth.OAuth2 claim 未加载: %+v", oauth)
	}
	if !oauth.AutoCreateUser || !oauth.InsecureSkipVerify {
		t.Errorf("Auth.OAuth2 开关未加载: %+v", oauth)
	}
}

// TestLDAPUsernameAttributeDTORoundTrip 回归测试：DTO 双向转换必须保留
// username_attribute，否则界面保存时该字段会被清空。
func TestLDAPUsernameAttributeDTORoundTrip(t *testing.T) {
	cfg := Config{Auth: AuthConfig{LDAP: LDAPClientConfig{
		Enabled:           true,
		Host:              "ldap.example.com",
		Port:              389,
		UsernameAttribute: "sAMAccountName",
	}}}

	dto := cfg.ToDTO()
	if dto.Auth.LDAP.UsernameAttribute != "sAMAccountName" {
		t.Errorf("ToDTO: UsernameAttribute = %q, want %q", dto.Auth.LDAP.UsernameAttribute, "sAMAccountName")
	}

	restored := ConfigFromDTO(dto)
	if got := restored.Auth.LDAP.UsernameAttribute; got != "sAMAccountName" {
		t.Errorf("ConfigFromDTO: UsernameAttribute = %q, want %q", got, "sAMAccountName")
	}
}

// TestValidateAuthConfig 校验第三方认证互斥与 OAuth2 provider_id 必填规则。
func TestValidateAuthConfig(t *testing.T) {
	cases := []struct {
		name         string
		ldapEnabled  bool
		oauthEnabled bool
		providerID   string
		wantErr      bool
	}{
		{"均未启用", false, false, "", false},
		{"仅 LDAP", true, false, "", false},
		{"仅 OAuth2 且配置 provider_id", false, true, "keycloak-prod", false},
		{"仅 OAuth2 缺 provider_id", false, true, "  ", true},
		{"同时启用", true, true, "keycloak-prod", true},
	}
	for _, tc := range cases {
		if got := ValidateAuthConfig(tc.ldapEnabled, tc.oauthEnabled, tc.providerID); (got != "") != tc.wantErr {
			t.Errorf("%s: ValidateAuthConfig = %q, wantErr=%v", tc.name, got, tc.wantErr)
		}
	}
}

// TestIdentityConflictModeDefaults 空值默认 self_service，大小写/空白归一化。
func TestIdentityConflictModeDefaults(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", IdentityConflictModeSelfService},
		{"   ", IdentityConflictModeSelfService},
		{" ADMIN ", IdentityConflictModeAdmin},
		{"self_service", IdentityConflictModeSelfService},
		{"custom", "custom"},
	}
	for _, tc := range cases {
		if got := NormalizeIdentityConflictMode(tc.in); got != tc.want {
			t.Errorf("NormalizeIdentityConflictMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := (AuthConfig{}).EffectiveIdentityConflictMode(); got != IdentityConflictModeSelfService {
		t.Errorf("空配置应默认 self_service，实际 %q", got)
	}
}

// TestValidateIdentityConflictMode 非法值必须被拒绝，空值与白名单值通过。
func TestValidateIdentityConflictMode(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"admin", false},
		{"self_service", false},
		{"ADMIN", false},
		{"auto", true},
		{"self-service", true},
	}
	for _, tc := range cases {
		if got := ValidateIdentityConflictMode(tc.in); (got != "") != tc.wantErr {
			t.Errorf("ValidateIdentityConflictMode(%q) = %q, wantErr=%v", tc.in, got, tc.wantErr)
		}
	}
}

// TestIdentityConflictModeDTORoundTrip DTO 双向转换保留同名冲突处理方式。
func TestIdentityConflictModeDTORoundTrip(t *testing.T) {
	cfg := Config{Auth: AuthConfig{IdentityConflictMode: IdentityConflictModeAdmin}}
	dto := cfg.ToDTO()
	if dto.Auth.IdentityConflictMode != IdentityConflictModeAdmin {
		t.Errorf("ToDTO: IdentityConflictMode = %q", dto.Auth.IdentityConflictMode)
	}
	restored := ConfigFromDTO(dto)
	if restored.Auth.IdentityConflictMode != IdentityConflictModeAdmin {
		t.Errorf("ConfigFromDTO: IdentityConflictMode = %q", restored.Auth.IdentityConflictMode)
	}

	// 空值经 ToDTO 应呈现生效默认值
	empty := Config{}
	if got := empty.ToDTO().Auth.IdentityConflictMode; got != IdentityConflictModeSelfService {
		t.Errorf("空配置 DTO = %q, want %q", got, IdentityConflictModeSelfService)
	}
}

// TestReadConfig_IdentityConflictModeLoaded 配置文件中的取值必须被加载进 GlobalConfig。
func TestReadConfig_IdentityConflictModeLoaded(t *testing.T) {
	content := `
upload:
  chunk_size: 10M
auth:
  identity_conflict_mode: admin
`
	dir := t.TempDir()
	configFile := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(configFile, []byte(content), 0600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	origPath, origConfig := ConfigPath, GlobalConfig
	defer func() {
		ConfigPath, GlobalConfig = origPath, origConfig
	}()

	ConfigPath = configFile
	GlobalConfig = Config{}
	readConfig()

	if got := GlobalConfig.Auth.IdentityConflictMode; got != IdentityConflictModeAdmin {
		t.Errorf("IdentityConflictMode = %q, want %q", got, IdentityConflictModeAdmin)
	}
}
