package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"fuzhan/internal/utils"
)

// buildServerURL 构造客户端可访问的服务器地址。
// Host 头为攻击者可控，先做严格白名单校验（仅允许主机名/端口/IPv6 字面量字符），
// 非法值直接返回错误，避免被注入到安装脚本的 shell 上下文。
func buildServerURL(r *http.Request) (string, error) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host, err := validateServerHost(r.Host)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", scheme, host), nil
}

// validateServerHost 校验 Host 头仅包含主机名字符集（字母/数字/点/冒号/连字符/IPv6括号）
func validateServerHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("无效的服务器地址")
	}
	for _, r := range host {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == ':' || r == '-' || r == '[' || r == ']'
		if !ok {
			return "", errors.New("无效的服务器地址")
		}
	}
	return host, nil
}

// HandleInstallScript 生成安装脚本，将 fuzhan 命令行工具安装到用户 PATH
func HandleInstallScript(w http.ResponseWriter, r *http.Request) {
	utils.PrintRequestInfo(r)

	// 获取基础URL
	serverURL, err := buildServerURL(r)
	if err != nil {
		utils.Warn("install.sh 请求包含非法 Host", utils.String("host", r.Host))
		http.Error(w, "无效的服务器地址", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"install.sh\"")
	w.Header().Set("Cache-Control", "no-cache")

	script := generateInstallScript(serverURL)
	w.Write([]byte(script))
}

// HandleFuzhanScript 生成独立的 fuzhan 命令行工具脚本
func HandleFuzhanScript(w http.ResponseWriter, r *http.Request) {
	utils.PrintRequestInfo(r)

	// 获取基础URL
	serverURL, err := buildServerURL(r)
	if err != nil {
		utils.Warn("fuzhan.sh 请求包含非法 Host", utils.String("host", r.Host))
		http.Error(w, "无效的服务器地址", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"fuzhan.sh\"")
	w.Header().Set("Cache-Control", "no-cache")

	script := generateScript(serverURL)
	w.Write([]byte(script))
}

// generateInstallScript 生成安装脚本：把 fuzhan 工具脚本下载到用户 PATH 目录
func generateInstallScript(serverURL string) string {
	// 对 serverURL 进行 shell 安全的转义
	escapedServer := escapeShellString(serverURL)

	return fmt.Sprintf(`#!/bin/bash
# fuzhan CLI 安装脚本
# 服务器: %[1]s
#
# 用法:
#   bash <(curl -s %[1]s/cli/install.sh)
#   curl -s %[1]s/cli/install.sh | bash
#   bash install.sh [自定义目录]

set -e

SERVER="%[1]s"

if [ -n "${1}" ]; then
    DIR="${1}"
else
    DIR="$HOME/.local/bin"
    if [ ! -d "${DIR}" ]; then
        DIR="$HOME/bin"
    fi
fi

mkdir -p "${DIR}"

echo "正在从 ${SERVER}/cli/fuzhan.sh 下载 fuzhan 命令行工具..."
if ! curl -fsSL "${SERVER}/cli/fuzhan.sh" -o "${DIR}/fuzhan"; then
    echo "下载失败，请检查服务器地址与网络连接。" >&2
    exit 1
fi
chmod +x "${DIR}/fuzhan"

case ":${PATH}:" in
    *":${DIR}:"*) ;;
    *)
        echo "提示：${DIR} 不在 PATH 中，将其加入 PATH 后可直接使用 fuzhan 命令："
        echo "  export PATH=\"${DIR}:\${PATH}\""
        ;;
esac

echo "fuzhan 已安装到 ${DIR}"
echo "现在可在终端中直接运行：fuzhan help"
`, escapedServer)
}

// generateScript 生成独立的 fuzhan 命令行工具脚本内容
func generateScript(serverURL string) string {
	// 对 serverURL 进行 shell 安全的转义
	escapedServer := escapeShellString(serverURL)

	return fmt.Sprintf(`#!/bin/bash
# fuzhan CLI - 自动生成的命令行工具
# 生成时间: $(date)
# 服务器: %[1]s
#
# 安装到 PATH:
#   bash <(curl -s %[1]s/cli/install.sh)
# 下载独立命令:
#   curl -o fuzhan.sh %[1]s/cli/fuzhan.sh && chmod +x fuzhan.sh && ./fuzhan.sh

fuzhan_SERVER="%[1]s"

# 颜色定义
fuzhan_COLOR_GREEN="\033[32m"
fuzhan_COLOR_YELLOW="\033[33m"
fuzhan_COLOR_CYAN="\033[36m"
fuzhan_COLOR_RESET="\033[0m"

# 使用说明
fuzhan_help() {
    cat <<'EOF'
fuzhan CLI - 文件管理命令行工具

用法: fuzhan <command> [args]

命令:
  search, s <关键词...>   搜索文件（空格分隔多个关键词，AND 逻辑）
  list, l [路径]          浏览目录
  download, dl <路径或哈希值>  下载文件到当前目录（自动识别路径或哈希）
  help, h                 显示此帮助

选项:
  --url, -u               显示完整下载链接（默认只显示文件路径）

搜索示例:
  fuzhan search document         搜索文件名包含 document 的文件
  fuzhan search .pdf             搜索所有 PDF 文件
  fuzhan search document report  搜索文件名同时包含 document 和 report 的文件
  fuzhan search --url document   显示完整下载链接

浏览示例:
  fuzhan list                    列出所有根目录
  fuzhan list root/doc           列出 root/doc 目录下的内容

下载示例:
  fuzhan download root/doc/report.pdf  下载文件到当前目录
  fuzhan download A1B2C3D4             按哈希值下载文件

EOF
}

# 判断是否为最近（24小时内）的时间
fuzhan_is_recent() {
    local time_str="$1"
    if [[ "$(uname -s)" == "Darwin" ]]; then
        # macOS
        local file_epoch
        file_epoch=$(date -j -f "%%Y-%%m-%%d %%H:%%M:%%S" "$time_str" "+%%s" 2>/dev/null) || return 1
        local now_epoch
        now_epoch=$(date "+%%s")
        local diff=$(( now_epoch - file_epoch ))
        [ "$diff" -ge 0 ] && [ "$diff" -le 86400 ]
        return $?
    else
        # Linux
        local file_epoch
        file_epoch=$(date -d "$time_str" "+%%s" 2>/dev/null) || return 1
        local now_epoch
        now_epoch=$(date "+%%s")
        local diff=$(( now_epoch - file_epoch ))
        [ "$diff" -ge 0 ] && [ "$diff" -le 86400 ]
        return $?
    fi
}

# 格式化一行搜索结果
fuzhan_format_line() {
    local time_str="$1"
    local hash_str="$2"
    local url="$3"
    local show_url="$4"

    # 从 URL 中提取路径
    local path
    if [[ "$url" == */download/* ]]; then
        path="${url#*/download/}"
    elif [[ "$url" == */api/v1/download/* ]]; then
        path="${url#*/api/v1/download/}"
    elif [[ "$url" == */cli/list/* ]]; then
        path="${url#*/cli/list/}"
    else
        path="$url"
    fi

    # 判断是否为最近文件，应用颜色
    local time_display="$time_str"
    if fuzhan_is_recent "$time_str" 2>/dev/null; then
        time_display="${fuzhan_COLOR_GREEN}${time_str}${fuzhan_COLOR_RESET}"
    fi

    if [ "$show_url" -eq 1 ]; then
        echo -e "${time_display} ${hash_str} ${url}"
    else
        echo -e "${time_display} ${hash_str} ${path}"
    fi
}

# 格式化一行列表结果
fuzhan_format_list_line() {
    local time_str="$1"
    local hash_str="$2"
    local icon="$3"
    local url="$4"
    local show_url="$5"

    # 从 URL 中提取路径
    local path
    if [[ "$url" == */download/* ]]; then
        path="${url#*/download/}"
    elif [[ "$url" == */api/v1/download/* ]]; then
        path="${url#*/api/v1/download/}"
    elif [[ "$url" == */cli/list/* ]]; then
        path="${url#*/cli/list/}"
    else
        path="$url"
    fi

    # 判断是否为最近文件，应用颜色
    local time_display="$time_str"
    if fuzhan_is_recent "$time_str" 2>/dev/null; then
        time_display="${fuzhan_COLOR_GREEN}${time_str}${fuzhan_COLOR_RESET}"
    fi

    if [ "$show_url" -eq 1 ]; then
        echo -e "${time_display} ${hash_str} ${icon} ${url}"
    else
        echo -e "${time_display} ${hash_str} ${icon} ${path}"
    fi
}

# 搜索文件
fuzhan_search() {
    local show_url=0
    local args=()

    # 解析选项
    for arg in "$@"; do
        case "$arg" in
            --url|-u) show_url=1 ;;
            *) args+=("$arg") ;;
        esac
    done

    local query="${args[*]}"
    if [ -z "$query" ]; then
        echo "错误：搜索关键词不能为空" >&2
        echo "用法: fuzhan search <关键词...>" >&2
        return 1
    fi
    # 将空格转换为逗号（与 CLI 搜索语法一致）
    local encoded
    encoded="${query// /,}"
    # URL 编码特殊字符
    encoded="$(printf '%%s' "$encoded" | sed 's/ /%%20/g; s/#/%%23/g; s/&/%%26/g; s/?/%%3F/g')"

    # 获取原始输出并解析每一行
    local raw_output
    raw_output=$(curl -s "$fuzhan_SERVER/cli/search/$encoded")
    echo "$raw_output" | while IFS= read -r line; do
        # 跳过空行和提示信息行
        if [[ -z "$line" || "$line" == "---"* || "$line" == *"检索"* || "$line" == *"搜索完成"* || "$line" == *"中止"* ]]; then
            echo "$line"
            continue
        fi
        # 解析格式: "2026-06-06 12:12:12 HASH URL"
        if [[ "$line" =~ ^([0-9]{4}-[0-9]{2}-[0-9]{2}\ [0-9]{2}:[0-9]{2}:[0-9]{2})\ ([^\ ]+)\ (.+)$ ]]; then
            local time_str="${BASH_REMATCH[1]}"
            local hash_str="${BASH_REMATCH[2]}"
            local url="${BASH_REMATCH[3]}"
            fuzhan_format_line "$time_str" "$hash_str" "$url" "$show_url"
        else
            echo "$line"
        fi
    done
}

# 浏览目录
fuzhan_list() {
    local path=""
    local show_url=0
    local args=()

    # 解析选项
    for arg in "$@"; do
        case "$arg" in
            --url|-u) show_url=1 ;;
            *) args+=("$arg") ;;
        esac
    done

    path="${args[0]}"
    local encoded=""
    if [ -n "$path" ]; then
        encoded="$(printf '%%s' "$path" | sed 's/ /%%20/g; s/#/%%23/g; s/&/%%26/g; s/?/%%3F/g')"
    fi

    local raw_output
    if [ -z "$path" ]; then
        raw_output=$(curl -s "$fuzhan_SERVER/cli/list/")
    else
        raw_output=$(curl -s "$fuzhan_SERVER/cli/list/$encoded")
    fi

    echo "$raw_output" | while IFS= read -r line; do
        # 跳过空行和提示信息行
        if [[ -z "$line" || "$line" == "---"* || "$line" == *"路径"* || "$line" == *"条目"* ]]; then
            echo "$line"
            continue
        fi
        # 解析格式: "2026-06-06 12:12:12 HASH [目录] URL" 或 "2026-06-06 12:12:12 HASH [文件] URL"
        if [[ "$line" =~ ^([0-9]{4}-[0-9]{2}-[0-9]{2}\ [0-9]{2}:[0-9]{2}:[0-9]{2})\ ([^\ ]+)\ \[([^\]]+)\]\ (.+)$ ]]; then
            local time_str="${BASH_REMATCH[1]}"
            local hash_str="${BASH_REMATCH[2]}"
            local icon="[${BASH_REMATCH[3]}]"
            local url="${BASH_REMATCH[4]}"
            fuzhan_format_list_line "$time_str" "$hash_str" "$icon" "$url" "$show_url"
        else
            echo "$line"
        fi
    done
}

# 下载文件（自动识别路径或哈希）
fuzhan_download() {
    local target="${1}"

    if [ -z "$target" ]; then
        echo "错误：文件路径或哈希值不能为空" >&2
        echo "用法: fuzhan download <文件路径或哈希值>" >&2
        echo "示例: fuzhan download root/doc/report.pdf" >&2
        echo "      fuzhan download A1B2C3D4" >&2
        return 1
    fi

    local encoded
    encoded="$(printf '%%s' "$target" | sed 's/ /%%20/g; s/#/%%23/g; s/&/%%26/g; s/?/%%3F/g')"
    # -OJ 采用服务端 Content-Disposition 提供的真实文件名（哈希下载时也生效）
    curl -OJ "$fuzhan_SERVER/download/$encoded"
}

# 主函数
fuzhan() {
    if [ $# -eq 0 ]; then
        fuzhan_help
        return 0
    fi

    local cmd="$1"
    shift

    case "$cmd" in
        search|s)
            fuzhan_search "$@"
            ;;
        list|l)
            fuzhan_list "$@"
            ;;
        download|dl)
            fuzhan_download "$@"
            ;;
        help|h|--help|-h)
            fuzhan_help
            ;;
        *)
            echo "未知命令: $cmd" >&2
            echo "使用 'fuzhan help' 查看帮助" >&2
            return 1
            ;;
    esac
}

# 当直接执行本脚本时（./fuzhan.sh），调用主函数后退出；被 source 时仅定义函数
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    fuzhan "$@"
    exit $?
fi
`, escapedServer)
}

// escapeShellString 对字符串进行 shell 安全转义（双引号上下文专用）
// 生成的脚本将值放入 "..." 中，因此需转义 \、"、$、` 与换行，防止命令注入
func escapeShellString(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"$", "\\$",
		"`", "\\`",
		"\n", "\\n",
	)
	return r.Replace(s)
}
