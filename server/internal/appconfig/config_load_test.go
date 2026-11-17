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
    sync_interval: 300
    auto_create_user: true
    insecure_skip_verify: true
  oauth2:
    enabled: true
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
	if ldap.SyncInterval != 300 || !ldap.AutoCreateUser || !ldap.InsecureSkipVerify {
		t.Errorf("Auth.LDAP 同步字段未加载: %+v", ldap)
	}

	oauth := GlobalConfig.Auth.OAuth2
	if !oauth.Enabled || oauth.ClientID != "cid" || oauth.ClientSecret != "csecret" {
		t.Errorf("Auth.OAuth2 基础字段未加载: %+v", oauth)
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
