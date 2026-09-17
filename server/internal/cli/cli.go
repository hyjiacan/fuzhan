package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/filedir"
	"fuzhan/internal/resources"
	"fuzhan/internal/utils"
)

// getCLIPrefix 根据请求路径获取 CLI 前缀（兼容 /cli 和 /api/v1/cli）
func getCLIPrefix(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/v1/cli") {
		return "/api/v1/cli"
	}
	return "/cli"
}

// HandleCli 处理CLI请求
func HandleCli(w http.ResponseWriter, r *http.Request) {
	cliPrefix := getCLIPrefix(r)
	// 若访问了 /cli 或 /api/v1/cli 而不是加 / 结尾则自动重定向
	if r.URL.Path == cliPrefix {
		http.Redirect(w, r, cliPrefix+"/", http.StatusFound)
		return
	}

	// 返回 cli/search 和 cli/list 的帮助信息
	utils.PrintRequestInfo(r)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// 获取基础URL (http://host:port)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseUrl := fmt.Sprintf("%s://%s", scheme, r.Host)
	cliBase := baseUrl + cliPrefix

	heading := fmt.Sprintf("%s - CLI 帮助 (通过浏览器访问 %s 体验更佳)", appconfig.GlobalConfig.App.Name, baseUrl)
	splitLine := strings.Repeat("-", len(heading)-1)
	headerInfo := fmt.Sprintf("%s\n%s\n\n", splitLine, heading)

	helpMsg := fmt.Sprintf(resources.CliHelpTpl, headerInfo, cliBase, cliBase, cliBase, cliBase, cliBase)

	w.Write([]byte(helpMsg))
}

// CliSearch 处理命令行搜索请求
func CliSearch(w http.ResponseWriter, r *http.Request, hashMap map[string]string) {
	cliPrefix := getCLIPrefix(r)
	// 若访问了 /cli/search 或 /api/v1/cli/search 而不是加 / 结尾则自动重定向
	if r.URL.Path == cliPrefix+"/search" {
		http.Redirect(w, r, cliPrefix+"/search/", http.StatusFound)
		return
	}

	utils.PrintRequestInfo(r)
	query := strings.TrimPrefix(r.URL.Path, cliPrefix+"/search/")
	query, _ = url.QueryUnescape(query)
	utils.Info("CLI 搜索请求", utils.String("query", query))

	// 设置 event-stream 响应头
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 禁用响应压缩，避免因压缩导致消息延迟
	w.Header().Set("Content-Encoding", "identity")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 获取基础URL (http://host:port)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseUrl := fmt.Sprintf("%s://%s", scheme, r.Host)
	cliBase := baseUrl + cliPrefix

	// 程序头信息
	heading := fmt.Sprintf("%s - CLI 检索 (通过浏览器访问 %s 体验更佳)", appconfig.GlobalConfig.App.Name, baseUrl)
	splitLine := strings.Repeat("-", len(heading)-1)
	headerInfo := fmt.Sprintf("%s\n%s\n\n", splitLine, heading)

	// 检查关键字是否为空
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%s\n\n", headerInfo)
		fmt.Fprintf(w, `错误：搜索关键字不能为空

使用方法:
    %s/search/<关键字>      搜索文件
示例:
    %s/search/document      搜索文件名包含'document'的文件
    %s/search/document.exe  搜索文件名包含 document，扩展名为 exe 的文件
    %s/search/.pdf          搜索所有扩展名为 pdf 的文件
    %s/search/document,.pdf 搜索文件名包含 document 且扩展名为 pdf 的文件
`, cliBase, cliBase, cliBase, cliBase, cliBase)
		flusher.Flush()
		return
	}

	// 使用请求 context 监听客户端断开连接（替代已弃用的 CloseNotifier）
	ctx, cancel := context.WithCancel(r.Context())
	go func() {
		<-r.Context().Done()
		cancel() // 客户端断开连接时取消 context
	}()

	fmt.Fprintf(w, "%s\n正在检索...\n\n", headerInfo)
	flusher.Flush()

	var wg sync.WaitGroup
	resultChan := make(chan appconfig.FileInfo)

	// 为每个根目录启动搜索协程
	for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		rootName := filepath.Base(rootDir.Path)
		wg.Add(1)
		go func(ctx context.Context, dir, q, name string) {
			defer wg.Done()
			dirResultChan := make(chan appconfig.FileInfo)
			// 将 context 传递给 searchInDirectory
			go searchInDirectory(ctx, dir, q, name, dirResultChan, hashMap) // 此处添加 go 关键字
			for item := range dirResultChan {
				select {
				case <-ctx.Done():
					return
				case resultChan <- item:
				}
			}
		}(ctx, rootDir.Path, query, rootName)
	}

	// 启动一个协程，在所有搜索完成后关闭通道
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var totalCount int
	// 从通道接收结果并通过 event-stream 发送
	for {
		select {
		case <-ctx.Done():
			// 客户端断开连接或搜索取消，直接返回
			return
		case item, ok := <-resultChan:
			if !ok {
				// 通道已关闭，发送搜索完成消息
				data := fmt.Sprintf("搜索完成，共找到 %d 个结果", totalCount)
				fmt.Fprintf(w, "\n%s\n", data)
				flusher.Flush()
				return
			}
			totalCount++
			if totalCount > 1000 {
				cancel() // 超过 1000 条结果，取消搜索
				data := "检索结果超过 1000，中止检索"
				fmt.Fprintf(w, "\n%s\n", data)
				flusher.Flush()
				return
			}
			filePath := strings.ReplaceAll(item.Path, "\\", "/")
			filePath = strings.ReplaceAll(filePath, " ", "%20")
			fullUrl := fmt.Sprintf("%s/download/%s", baseUrl, filePath)
			modTime := formatModTime(item.ModifiedTime)
			hash := item.Xxh3Hash
			if hash == "" {
				hash = "-"
			}
			fmt.Fprintf(w, "%s %s %s\n", modTime, hash, fullUrl)
			flusher.Flush()
		}
	}
}

// CliList 处理命令行列出目录请求
func CliList(w http.ResponseWriter, r *http.Request, hashMap map[string]string) {
	cliPrefix := getCLIPrefix(r)
	utils.PrintRequestInfo(r)

	// 获取基础URL (http://host:port)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseUrl := fmt.Sprintf("%s://%s", scheme, r.Host)

	// 获取要列出的目录相对路径
	relativePath := strings.TrimPrefix(r.URL.Path, cliPrefix+"/list/")
	relativePath, _ = url.QueryUnescape(relativePath)

	// 程序头信息
	heading := fmt.Sprintf("%s - CLI 浏览 (通过浏览器访问 %s 体验更佳)", appconfig.GlobalConfig.App.Name, baseUrl)
	splitLine := strings.Repeat("-", len(heading)-1)
	headerInfo := fmt.Sprintf("%s\n%s\n\n路径: /%s\n\n", splitLine, heading, relativePath)

	var targetPath, rootName string
	if relativePath == "" {
		// 没有指定路径，列出所有根目录
		var allItems []appconfig.FileInfo
		for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
			rootName = filepath.Base(rootDir.Path)
			items := filedir.GetDirectoryItems(rootDir.Path, rootName, hashMap)
			allItems = append(allItems, items...)
		}
		// 排序，目录在前，文件在后
		sort.Slice(allItems, func(i, j int) bool {
			if allItems[i].Type == "directory" && allItems[j].Type != "directory" {
				return true
			}
			if allItems[i].Type != "directory" && allItems[j].Type == "directory" {
				return false
			}
			return allItems[i].Name < allItems[j].Name
		})
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(headerInfo))
		outputItems(w, baseUrl, cliPrefix, allItems)
		return
	}

	// 解析路径
	parts := strings.SplitN(relativePath, "/", 2)
	if len(parts) < 2 {
		utils.Warn("路径格式不正确", utils.String("path", relativePath))
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(headerInfo))
		// 手动写入错误信息，避免 http.Error 重复设置头
		w.Write([]byte("错误：指定路径格式不正确，应为 '根目录名/子路径'\n"))
		return
	}
	rootName = parts[0]
	subPath := parts[1]
	if _, exists := appconfig.RootNames[rootName]; !exists {
		utils.Warn("根目录不存在", utils.String("rootName", rootName))
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(headerInfo))
		// 手动写入错误信息，避免 http.Error 重复设置头
		w.Write([]byte("错误：指定的根目录名不存在\n"))
		return
	}
	targetPath = filepath.Join(appconfig.RootNames[rootName], subPath)
	info, err := os.Stat(targetPath)
	if err != nil || !info.IsDir() {
		utils.Warn("路径不存在或非目录", utils.String("path", targetPath))
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(headerInfo))
		w.Write([]byte("错误：指定路径不存在或非目录\n"))
		return
	}

	// 使用 PathValidator 验证路径
	validator := utils.NewPathValidatorWithRoots(rootName, appconfig.RootNames)
	if validator == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(headerInfo))
		w.Write([]byte("错误：无效的根目录\n"))
		return
	}
	if err := validator.Validate(targetPath); err != nil {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(headerInfo))
		w.Write([]byte("错误：路径越权\n"))
		return
	}

	// 获取目录内容
	items := filedir.GetDirectoryItems(targetPath, rootName, hashMap)
	// 排序，目录在前，文件在后
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type == "directory" && items[j].Type != "directory" {
			return true
		}
		if items[i].Type != "directory" && items[j].Type == "directory" {
			return false
		}
		return items[i].Name < items[j].Name
	})
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(headerInfo))
	outputItems(w, baseUrl, cliPrefix, items)
}
