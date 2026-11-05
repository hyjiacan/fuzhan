package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"sort"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/auth"
	"fuzhan/internal/utils"
)

// sortedKeys 返回 map 的排序后的键列表
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ensureJWTPersisted 当 JWT 密钥既未配置也未通过环境变量指定时，
// 生成随机密钥并写入配置文件，保证重启后签名密钥稳定。
func ensureJWTPersisted() {
	if os.Getenv("JWT_SECRET") != "" {
		return
	}
	if len(appconfig.GlobalConfig.Server.JWTSecret) >= 32 {
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		utils.Warn("生成固定 JWT 密钥失败，本次运行将使用动态密钥", utils.Err(err))
		return
	}
	secretHex := hex.EncodeToString(secret)
	if err := appconfig.SaveConfigWithComments(appconfig.ConfigPath, map[string]interface{}{"server.jwt_secret": secretHex}); err != nil {
		utils.Warn("持久化 JWT 密钥失败，本次运行将使用动态密钥", utils.Err(err))
		return
	}
	appconfig.GlobalConfig.Server.JWTSecret = secretHex
	utils.Info("检测到未配置 JWT 密钥，已生成固定密钥并写入配置")
}

// oauth2ConfigFromApp 将应用配置中的 OAuth2 节点转换为认证服务内部使用的配置结构
func oauth2ConfigFromApp(c appconfig.OAuth2Config) *auth.OAuth2Config {
	return &auth.OAuth2Config{
		Enabled:            c.Enabled,
		ProviderName:       c.ProviderName,
		ClientID:           c.ClientID,
		ClientSecret:       c.ClientSecret,
		AuthURL:            c.AuthURL,
		TokenURL:           c.TokenURL,
		UserInfoURL:        c.UserInfoURL,
		Issuer:             c.Issuer,
		Scopes:             c.Scopes,
		RedirectURL:        c.RedirectURL,
		UsernameClaim:      c.UsernameClaim,
		DisplayNameClaim:   c.DisplayNameClaim,
		EmailClaim:         c.EmailClaim,
		AutoCreateUser:     c.AutoCreateUser,
		InsecureSkipVerify: c.InsecureSkipVerify,
	}
}
