// Package jwt 提供 JWT Token 的签发和验证
package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"

	"fuzhan/internal/utils"
	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtMu           sync.RWMutex
	JWTSecret       []byte
	oldJWTSecrets   [][]byte
	jwtSecretGetter func() string
)

// Claims JWT声明结构体
type Claims struct {
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
	jwt.RegisteredClaims
}

// SetJWTSecretGetter 设置 JWT 密钥获取器
func SetJWTSecretGetter(getter func() string) {
	jwtSecretGetter = getter
}

// UpdateJWTSecret 更新 JWT 密钥（旧密钥保留用于验证已有 token）
func UpdateJWTSecret(newSecret []byte) {
	if len(newSecret) == 0 {
		return
	}
	jwtMu.Lock()
	defer jwtMu.Unlock()
	if len(JWTSecret) > 0 {
		oldJWTSecrets = append(oldJWTSecrets, JWTSecret)
		if len(oldJWTSecrets) > 3 {
			oldJWTSecrets = oldJWTSecrets[len(oldJWTSecrets)-3:]
		}
	}
	JWTSecret = newSecret
}

// InitJWTSecret 初始化JWT密钥
func InitJWTSecret() error {
	jwtMu.Lock()
	defer jwtMu.Unlock()
	secretStr := os.Getenv("JWT_SECRET")
	if secretStr != "" {
		JWTSecret = []byte(secretStr)
		if len(JWTSecret) < 32 {
			return errors.New("JWT_SECRET 环境变量长度必须至少为 32 字节")
		}
		return nil
	}
	if jwtSecretGetter != nil {
		configSecret := jwtSecretGetter()
		if len(configSecret) >= 32 {
			JWTSecret = []byte(configSecret)
			return nil
		}
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	JWTSecret = secret
	utils.Warn("使用动态生成的 JWT 密钥，建议设置固定密钥")
	return nil
}

// readJWTSecret 安全读取 JWTSecret（调用方须持有读锁）
func readJWTSecret() []byte {
	return JWTSecret
}

// readOldJWTSecrets 安全读取 oldJWTSecrets（调用方须持有读锁）
func readOldJWTSecrets() [][]byte {
	return oldJWTSecrets
}

// GetJWTSecret 获取当前 JWT 密钥
func GetJWTSecret() []byte {
	jwtMu.RLock()
	defer jwtMu.RUnlock()
	return readJWTSecret()
}

// GenerateJWT 生成JWT令牌（默认24小时）
func GenerateJWT(uuid, username, role string, disabled bool) (string, error) {
	return GenerateJWTWithExpiry(uuid, username, role, disabled, 24*time.Hour)
}

// GenerateJWTWithExpiry 生成JWT令牌（自定义过期时间）
func GenerateJWTWithExpiry(uuid, username, role string, disabled bool, expiry time.Duration) (string, error) {
	jwtMu.RLock()
	secret := readJWTSecret()
	jwtMu.RUnlock()
	if len(secret) == 0 {
		return "", errors.New("JWT 密钥未初始化")
	}
	expirationTime := time.Now().Add(expiry)
	claims := &Claims{
		UUID:     uuid,
		Username: username,
		Role:     role,
		Disabled: disabled,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   username,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	jwtMu.RLock()
	secret = readJWTSecret()
	jwtMu.RUnlock()
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

// ParseJWT 解析JWT令牌（支持新旧密钥验证）
func ParseJWT(tokenString string) (*Claims, error) {
	jwtMu.RLock()
	secret := readJWTSecret()
	oldSecrets := readOldJWTSecrets()
	jwtMu.RUnlock()
	if len(secret) == 0 {
		return nil, errors.New("JWT 密钥未初始化")
	}
	claims, err := parseWithKey(tokenString, secret)
	if err == nil {
		return claims, nil
	}
	for _, oldKey := range oldSecrets {
		claims, err := parseWithKey(tokenString, oldKey)
		if err == nil {
			return claims, nil
		}
	}
	return nil, err
}

func parseWithKey(tokenString string, secret []byte) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// 固定仅接受 HS256 签名算法，防止 alg 混淆（none/其它算法）伪造令牌
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("不支持的 JWT 签名算法")
		}
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("无效的令牌")
	}
	return claims, nil
}

// IsValidToken 验证令牌是否有效
func IsValidToken(tokenString string) bool {
	_, err := ParseJWT(tokenString)
	return err == nil
}

// GetUsernameFromToken 从令牌中获取用户名
func GetUsernameFromToken(tokenString string) (string, error) {
	claims, err := ParseJWT(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Username, nil
}

// GetRoleFromToken 从令牌中获取角色
func GetRoleFromToken(tokenString string) (string, error) {
	claims, err := ParseJWT(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Role, nil
}

// GetUUIDFromToken 从令牌中获取UUID
func GetUUIDFromToken(tokenString string) (string, error) {
	claims, err := ParseJWT(tokenString)
	if err != nil {
		return "", err
	}
	return claims.UUID, nil
}

// GetJWTSecretHex 获取密钥的十六进制表示（用于配置）
func GetJWTSecretHex() string {
	jwtMu.RLock()
	s := hex.EncodeToString(readJWTSecret())
	jwtMu.RUnlock()
	return s
}
