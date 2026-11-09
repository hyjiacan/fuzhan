// Package version 提供应用版本号，构建时由构建脚本注入。
package version

// Version 由构建脚本通过 -ldflags "-X fuzhan/internal/version.Version=<version>" 注入；
// 直接 go run / go test 时回退为开发版本。
var Version = "dev"
