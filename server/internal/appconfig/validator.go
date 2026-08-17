package appconfig

import (
    "fmt"
    "reflect"
    "strings"

    "github.com/go-playground/validator/v10"
    "fuzhan/pkg/pathutils"
)

// Validator 封装验证器
type Validator struct {
    validate *validator.Validate
}

// NewValidator 创建验证器实例
func NewValidator() *Validator {
    v := validator.New()

    // 注册自定义验证器
    v.RegisterValidation("filepath", validateFilePath)
    v.RegisterValidation("quota", validateQuota)
    v.RegisterValidation("filetype", validateFileType)

    // 注册自定义字段名映射
    v.RegisterTagNameFunc(func(fld reflect.StructField) string {
        name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
        if name == "-" {
            return ""
        }
        return name
    })

    return &Validator{validate: v}
}

// ValidationError 验证错误
type ValidationError struct {
    Field   string `json:"field"`
    Message string `json:"message"`
}

// ValidationErrors 验证错误集合
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
    var msgs []string
    for _, err := range ve {
        msgs = append(msgs, fmt.Sprintf("%s: %s", err.Field, err.Message))
    }
    return strings.Join(msgs, "; ")
}

// TranslateError 转换验证错误
func (v *Validator) TranslateError(err error) ValidationErrors {
    var errs ValidationErrors

    if validationErrors, ok := err.(validator.ValidationErrors); ok {
        for _, e := range validationErrors {
            field := e.Field()
            tag := e.Tag()

            var message string
            switch tag {
            case "required":
                message = "此字段为必填项"
            case "filepath":
                message = "文件路径格式不正确"
            case "quota":
                message = "配额格式不正确"
            case "filetype":
                message = "文件类型不被允许"
            case "max":
                message = fmt.Sprintf("值不能大于 %s", e.Param())
            case "min":
                message = fmt.Sprintf("值不能小于 %s", e.Param())
            default:
                message = fmt.Sprintf("验证失败 [%s]", tag)
            }

            errs = append(errs, ValidationError{
                Field:   field,
                Message: message,
            })
        }
    }

    return errs
}

// 自定义验证器：文件路径验证
func validateFilePath(fl validator.FieldLevel) bool {
    path := fl.Field().String()
    // 简单验证：不为空且不包含非法字符
    if path == "" {
        return false
    }

    // 检查是否包含非法字符（根据实际需求调整）
    illegalChars := []string{"..", "<", ">", "|", "?", "*", "\""}
    for _, char := range illegalChars {
        if strings.Contains(path, char) {
            return false
        }
    }

    return true
}

// 自定义验证器：配额验证
func validateQuota(fl validator.FieldLevel) bool {
    quota := fl.Field().String()
    if quota == "" {
        return true // 空值允许
    }

    // 使用配置包中的解析函数进行验证
    _, err := ParseQuotaString(quota)
    return err == nil
}

// 自定义验证器：文件类型验证
func validateFileType(fl validator.FieldLevel) bool {
    filename := fl.Field().String()
    if filename == "" {
        return true // 空值允许
    }

    // 使用 utils 包中的文件类型检查函数
    return pathutils.IsFileAllowed(GlobalConfig.Storage.AllowedExtensions, filename)
}