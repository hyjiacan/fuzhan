// searchdemo 是 internal/search 检索模块的独立演示入口。
//
// 串联演示：初始化索引 -> 写入若干测试数据 -> 自动补全 -> 拼写纠错 -> 优雅关闭。
// 运行：
//   go run ./cmd/searchdemo
// 演示索引默认写入 ./data/bluge_index 目录。
package main

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "fuzhan/internal/search"
)

func main() {
    // 1. 初始化：索引存放于 data/bluge_index（工作目录 data 子目录）
    indexDir := filepath.Join("data", "bluge_index")
    fmt.Println("==> 初始化索引：", indexDir)
    idx, err := search.OpenIndex(indexDir)
    if err != nil {
        fatal("初始化索引失败", err)
    }

    // 2. 写入测试数据（中英文混合，模拟真实文件名）
    testFiles := []struct {
        id       int64
        fileName string
    }{
        {1, "README.md"},
        {2, "年度财务报表2024.pdf"},
        {3, "华为Mate60Pro评测.pdf"},
        {4, "README_EN.md"},
        {5, "reade me 说明.txt"}, // 英文拼写易错场景
        {6, "ISO9001质量体系文档.docx"},
        {7, "产品需求PRD_v2.3_release.xlsx"},
        {8, "年度预算表.xlsx"},
        {9, "年度总结报告.docx"},
        {10, "Openssl证书备份.tar.gz"},
    }
    fmt.Println("==> 写入索引数据（", len(testFiles), " 条）")
    for _, tf := range testFiles {
        if err := idx.IndexFile(tf.id, tf.fileName); err != nil {
            fmt.Println("    写入失败 id=", tf.id, err)
        }
    }

    // 3. 自动补全：用户输入前缀，返回联想文件名
    fmt.Println("\n========== 自动补全（Prefix 前缀查询）==========")
    autocomplete(idx, "read")
    autocomplete(idx, "README")
    autocomplete(idx, "Mate")
    autocomplete(idx, "年度")
    autocomplete(idx, "财务")

    // 4. 拼写纠错：用户拼错，返回相似文件名推荐
    fmt.Println("\n========== 拼写纠错（Fuzzy 模糊查询）==========")
    spellcheck(idx, "REEDME", 5)    // 期望 -> README.md / README_EN.md
    spellcheck(idx, "Mata60", 5)    // 期望 -> 华为Mate60Pro评测.pdf
    spellcheck(idx, "年渡报表", 5)  // 中文近似
    spellcheck(idx, "ISO9000", 5)   // 数字版本近似

    // 5. 优雅关闭：刷盘并释放资源
    fmt.Println("\n==> 优雅关闭 Writer ...")
    if err := idx.CloseWriter(); err != nil {
        fmt.Println("    关闭失败:", err)
    }
    fmt.Println("==> 完成")
}

// autocomplete 演示自动补全
func autocomplete(idx *search.SearchIndex, prefix string) {
    fmt.Printf("[补全] 前缀 %q -> ", prefix)
    hits, err := idx.AutoComplete(prefix, 10)
    if err != nil {
        fmt.Println("查询失败:", err)
        return
    }
    if len(hits) == 0 {
        fmt.Println("(无匹配)")
        return
    }
    fmt.Println(strings.Join(hits, "  |  "))
}

// spellcheck 演示拼写纠错
func spellcheck(idx *search.SearchIndex, word string, limit int) {
    fmt.Printf("[纠错] 输入 %q -> 建议: ", word)
    hits, err := idx.SpellCheck(word, limit)
    if err != nil {
        fmt.Println("查询失败:", err)
        return
    }
    if len(hits) == 0 {
        fmt.Println("(无候选)")
        return
    }
    fmt.Println(strings.Join(hits, "  |  "))
}

// fatal 打印错误并退出
func fatal(msg string, err error) {
    fmt.Fprintln(os.Stderr, "错误:", msg, err)
    os.Exit(1)
}