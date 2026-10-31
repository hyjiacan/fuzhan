// Package captcha 提供图形验证码的生成与一次性校验。
//
// 本包是 github.com/mojocn/base64Captcha v1.3.8 的薄封装，只暴露两个入口：
// Generate 生成验证码（id + PNG data URI），Verify 校验并销毁。
// 输出严格限定为 PNG data URI（data:image/png;base64,...），不提供 SVG 图像。
//
// 校验为一次性语义：每次 Verify 都以 clear=true 读取存储，无论答案对错，
// 同一 id 在校验后即被销毁，避免同一验证码被重放。
//
// 本包不依赖任何业务配置（appconfig/auth/lite 等），由调用方按
// account.captcha.mode 决定何时生成与校验，以免形成循环依赖。
package captcha

import (
	"errors"
	"image/color"
	"strings"
	"time"

	"github.com/mojocn/base64Captcha"
)

const (
	// pngDataURIPrefix PNG 图片 data URI 前缀
	pngDataURIPrefix = "data:image/png;base64,"
	// codeLength 验证码字符数
	codeLength = 4
	// codeSource 随机字符集：剔除易混淆的 0/O/1/l/I
	codeSource = "23456789ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz"
	// storeCollectNum 触发内存回收的计数阈值
	storeCollectNum = 80
	// storeExpiration 验证码有效期
	storeExpiration = 5 * time.Minute

	// imageHeight 图片高度（像素）
	imageHeight = 48
	// imageWidth 图片宽度（像素）
	imageWidth = 160
	// noiseCount 噪点数量（适中）
	noiseCount = 2
	// lineOptions 干扰线：三条细线，强度适中
	lineOptions = base64Captcha.OptionShowSlimeLine
)

// errUnsupportedImage 库返回的图像不是 PNG data URI（防御性错误，正常不会出现）
var errUnsupportedImage = errors.New("captcha: 仅接受 PNG data URI")

// errEmptyImage 库返回的图像数据为空
var errEmptyImage = errors.New("captcha: 图像数据为空")

// bgColor 淡雅背景色（浅灰蓝），避免与深色字符的对比度过高
var bgColor = &color.RGBA{R: 238, G: 242, B: 247, A: 255}

// engine 包级单例：4 字符 PNG 驱动 + 库内置内存 store
var engine = newEngine()

// newEngine 构建验证码单例。
// 字体存储与字体列表均交由库的内置字体集（参数 nil / 空切片由库回退）。
func newEngine() *base64Captcha.Captcha {
	driver := base64Captcha.NewDriverString(
		imageHeight, // 高度
		imageWidth,  // 宽度
		noiseCount,  // 噪点数量
		lineOptions, // 干扰线选项
		codeLength,  // 字符数
		codeSource,  // 字符集
		bgColor,     // 背景色
		nil,         // 字体存储：nil 时使用库内置字体
		[]string{},  // 字体列表：为空时使用库内置字体集
	)
	store := base64Captcha.NewMemoryStore(storeCollectNum, storeExpiration)
	return base64Captcha.NewCaptcha(driver, store)
}

// Generate 生成一枚验证码，返回 id 与 PNG data URI。
// 明文答案由库自行写入内存 store，本包不对外暴露。
func Generate() (id string, pngDataURI string, err error) {
	genID, b64s, _, genErr := engine.Generate()
	if genErr != nil {
		return "", "", genErr
	}
	if genID == "" || b64s == "" {
		return "", "", errEmptyImage
	}
	uri, uriErr := normalizeDataURI(b64s)
	if uriErr != nil {
		return "", "", uriErr
	}
	return genID, uri, nil
}

// Verify 校验验证码，一次性语义：无论答案对错，该 id 都会被销毁。
// 空 id 或空（含全空白）答案直接返回 false，且不触碰存储。
// 答案仅做去空白处理，大小写比对沿用库的默认行为（不额外转换）。
func Verify(id, answer string) bool {
	answer = strings.TrimSpace(answer)
	if id == "" || answer == "" {
		return false
	}
	return engine.Verify(id, answer, true)
}

// normalizeDataURI 将库返回的 base64 图像规范化为 PNG data URI。
// 裸 base64 自动补前缀；已带 PNG 前缀直接返回；
// 其余 data URI（含 SVG 或其它 MIME）一律拒绝，保证不会输出 SVG。
func normalizeDataURI(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errEmptyImage
	}
	if strings.HasPrefix(s, pngDataURIPrefix) {
		return s, nil
	}
	if strings.HasPrefix(strings.ToLower(s), "data:") {
		return "", errUnsupportedImage
	}
	return pngDataURIPrefix + s, nil
}
