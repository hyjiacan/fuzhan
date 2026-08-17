package cli

import (
	"fmt"
	"net/http"
	"strings"

	"fuzhan/internal/utils"
)

// HandleInstallScript 生成 shell 脚本，封装 CLI 的访问
func HandleInstallScript(w http.ResponseWriter, r *http.Request) {
	utils.PrintRequestInfo(r)

	// 获取基础URL
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	serverURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"fuzhan.sh\"")
	w.Header().Set("Cache-Control", "no-cache")

	script := generateScript(serverURL)
	w.Write([]byte(script))
}

// generateScript 生成 shell 脚本内容
func generateScript(serverURL string) string {
	// 对 serverURL 进行 shell 安全的转义
	escapedServer := escapeShellString(serverURL)

	return fmt.Sprintf(`#!/bin/bash
# fuzhan CLI - 自动生成的命令行工具
# 生成时间: $(date)
# 服务器: %[1]s
#
# 安装:
#   source <(curl -s %[1]s/cli/install.sh)
# 下载:
#   curl -o fuzhan.sh %[1]s/cli/install.sh && chmod +x fuzhan.sh && ./fuzhan.sh

fuzhan_SERVER="%[1]s"

# 颜色定义
fuzhan_COLOR_GREEN="\033[32m"
fuzhan_COLOR_YELLOW="\033[33m"
fuzhan_COLOR_CYAN="\033[36m"
fuzhan_COLOR_RESET="\033[0m"

# 当直接运行时，执行 fuzhan 函数后退出
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    fuzhan "$@"
    exit $?
fi

# 使用说明
fuzhan_help() {
    cat <<'EOF'
fuzhan CLI - 文件管理命令行工具

用法: fuzhan <command> [args]

命令:
  search, s <关键词...>   搜索文件（空格分隔多个关键词，AND 逻辑）
  list, l [路径]          浏览目录
  download, dl <路径>     下载文件（支持 --hash <hash> 按哈希下载）
  help, h                 显示此帮助

选项:
  --url, -u               显示完整下载链接（默认只显示文件路径）
  --hash <hash>           使用哈希值下载文件（仅 download 命令）

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
  fuzhan download --hash A1B2C3D4     按哈希值下载文件

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
    if [[ "$url" == */api/v1/download/* ]]; then
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
    if [[ "$url" == */api/v1/download/* ]]; then
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

# 下载文件
fuzhan_download() {
    local path=""
    local hash=""
    local hash_mode=0
    local args=()

    # 解析选项
    for arg in "$@"; do
        case "$arg" in
            --hash) hash_mode=1 ;;
            *) args+=("$arg") ;;
        esac
    done

    if [ "$hash_mode" = "1" ]; then
        hash="${args[0]}"
        if [ -z "$hash" ]; then
            echo "错误：哈希值不能为空" >&2
            echo "用法: fuzhan download --hash <哈希值>" >&2
            return 1
        fi
        local encoded
        encoded="$(printf '%%s' "$hash" | sed 's/ /%%20/g; s/#/%%23/g; s/&/%%26/g; s/?/%%3F/g')"
        curl -OJ "$fuzhan_SERVER/api/v1/download/by-hash/$encoded"
    else
        path="${args[0]}"
        if [ -z "$path" ]; then
            echo "错误：文件路径不能为空" >&2
            echo "用法: fuzhan download <文件路径>" >&2
            echo "示例: fuzhan download root/doc/report.pdf" >&2
            echo "      fuzhan download --hash A1B2C3D4" >&2
            return 1
        fi
        local encoded
        encoded="$(printf '%%s' "$path" | sed 's/ /%%20/g; s/#/%%23/g; s/&/%%26/g; s/?/%%3F/g')"
        curl -O "$fuzhan_SERVER/api/v1/download/$encoded"
    fi
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
`, escapedServer)
}

// escapeShellString 对字符串进行 shell 安全转义
func escapeShellString(s string) string {
	// 替换单引号为 '\'' (shell 安全的单引号转义)
	s = strings.ReplaceAll(s, "'", "'\\''")
	return s
}