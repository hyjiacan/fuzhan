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
    "time"

    "fuzhan/internal/appconfig"
    "fuzhan/internal/response"
    "fuzhan/internal/resources"
    "fuzhan/internal/utils"
)

// FileInfo 文件信息结构体
type FileInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Path         string `json:"path"`
	ModifiedTime string `json:"modifiedTime"`
	Size         int64  `json:"size"`
	Quota        int64  `json:"quota,omitempty"`
	Used         int64  `json:"used,omitempty"`
	Xxh3Hash     string `json:"xxh3Hash,omitempty"`
}

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

// outputItems 输出排序后的文件和目录信息
func outputItems(w http.ResponseWriter, baseUrl string, cliPrefix string, items []FileInfo) {
	var totalCount int
	for _, item := range items {
		totalCount++
		filePath := strings.ReplaceAll(item.Path, "\\", "/")
		filePath = strings.ReplaceAll(filePath, " ", "%20")
		var fullUrl string
		var icon string
		if item.Type == "directory" {
			// 目录路径追加到 /cli/list/ 后
			fullUrl = fmt.Sprintf("%s%s/list/%s", baseUrl, cliPrefix, filePath)
			icon = "[目录]"
		} else {
			// 文件使用 download 路径
			fullUrl = fmt.Sprintf("%s/download/%s", baseUrl, filePath)
			icon = "[文件]"
		}
		// 格式化修改时间
		modTime := formatModTime(item.ModifiedTime)
		hash := item.Xxh3Hash
		if hash == "" {
			hash = "-"
		}
		fmt.Fprintf(w, "%s %s %s %s\n", modTime, hash, icon, fullUrl)
	}
	// 发送完成消息
	dataMsg := fmt.Sprintf("\n目录列表获取完成，共找到 %d 个条目", totalCount)
	fmt.Fprintf(w, "%s\n", dataMsg)
}

// formatModTime 将修改时间格式化为 2006-01-02 15:04:05
func formatModTime(modifiedTime string) string {
	t, err := time.Parse(time.RFC3339, modifiedTime)
	if err != nil {
		return modifiedTime
	}
	return t.Format("2006-01-02 15:04:05")
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
	resultChan := make(chan FileInfo)

	// 为每个根目录启动搜索协程
	for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		rootName := filepath.Base(rootDir.Path)
		wg.Add(1)
		go func(ctx context.Context, dir, q, name string) {
			defer wg.Done()
			dirResultChan := make(chan FileInfo)
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
        var allItems []FileInfo
        for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
            rootName = filepath.Base(rootDir.Path)
            items := GetDirectoryItems(rootDir.Path, rootName, hashMap)
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
    items := GetDirectoryItems(targetPath, rootName, hashMap)
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

// searchInDirectory 在目录中搜索文件，通过通道实时返回结果
// 支持使用逗号分隔多个关键词，最后一个关键词可使用 .xxx 指定扩展名
// hashMap 为可选的文件路径到哈希值的映射，用于在搜索结果中附加哈希信息
func searchInDirectory(ctx context.Context, rootDir, query, rootName string, resultChan chan<- FileInfo, hashMap map[string]string) {
    defer close(resultChan)
    query = strings.TrimSpace(query)
    query, _ = url.QueryUnescape(query)

    // 用逗号分割多个关键词
    keywords := strings.Split(query, ",")
    var ext string
    var hasExtension bool

    // 检查最后一个关键词是否包含扩展名
    lastIdx := len(keywords) - 1
    lastKw := strings.TrimSpace(keywords[lastIdx])
    if strings.Contains(lastKw, ".") {
        parts := strings.SplitN(lastKw, ".", 2)
        ext = strings.ToLower(parts[1])
        hasExtension = true
        keywords[lastIdx] = parts[0]
    }

    // 清理关键词，移除空字符串并转为小写
    var cleanKeywords []string
    for _, kw := range keywords {
        kw = strings.ToLower(strings.TrimSpace(kw))
        if kw != "" {
            cleanKeywords = append(cleanKeywords, kw)
        }
    }

    err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            if err != nil {
                return err
            }
            // 指定了扩展名时跳过目录
            if hasExtension && info.IsDir() {
                return nil
            }

            filename := info.Name()
            fileNameLower := strings.ToLower(filename)

            // 匹配所有关键词（AND 逻辑）
            for _, kw := range cleanKeywords {
                if !strings.Contains(fileNameLower, kw) {
                    return nil
                }
            }

            // 匹配扩展名
            if hasExtension {
                fileExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
                if fileExt != ext {
                    return nil
                }
                if !isFileAllowed(filename) {
                    return nil
                }
            }

            // 所有条件匹配成功，发送结果
            item := getFileInfo(path, rootName)
            if hashMap != nil {
                if h, ok := hashMap[item.Path]; ok {
                    item.Xxh3Hash = h
                }
            }
            select {
            case <-ctx.Done():
                return ctx.Err()
            case resultChan <- item:
            }
            return nil
        }
    })
    if err != nil && err != context.Canceled {
        utils.Error("搜索文件时出错", utils.String("rootDir", rootDir), utils.Err(err))
    }
}

// isFileAllowed 检查文件类型是否允许
func isFileAllowed(filename string) bool {
    if len(appconfig.GlobalConfig.Storage.AllowedExtensions) == 0 {
        return true
    }
    ext := strings.ToLower(filepath.Ext(filename))
    if ext == "" || len(ext) <= 1 {
        return false
    }
    ext = ext[1:] // 移除开头的点
    for _, allowedExt := range appconfig.GlobalConfig.Storage.AllowedExtensions {
        if allowedExt == ext {
            return true
        }
    }
    return false
}

// getFileInfo 获取文件信息（不含根目录大小计算，由索引表提供）
func getFileInfo(itemPath, rootName string) FileInfo {
    info, err := os.Stat(itemPath)
    if err != nil {
        return FileInfo{
            Name:         filepath.Base(itemPath),
            Type:         "unknown",
            Path:         "",
            ModifiedTime: "",
            Size:         0,
        }
    }
    itemType := "file"
    if info.IsDir() {
        itemType = "directory"
    }
    relPath, _ := filepath.Rel(appconfig.RootNames[rootName], itemPath)
    relativePath := filepath.Join(rootName, relPath)
    if relativePath != "" {
        relativePath = strings.ReplaceAll(relativePath, "\\", "/")
    }
    modifiedTime := info.ModTime().Format(time.RFC3339)
    var fileSize int64
    if itemType == "file" {
        fileSize = info.Size()
    }

    return FileInfo{
        Name:         filepath.Base(itemPath),
        Type:         itemType,
        Path:         relativePath,
        ModifiedTime: modifiedTime,
        Size:         fileSize,
    }
}

// GetDirectoryItems 获取目录下的文件信息（不含根目录大小计算）
// hashMap 为可选的文件路径到哈希值的映射，用于在结果中附加哈希信息
func GetDirectoryItems(dirPath, rootName string, hashMap map[string]string) []FileInfo {
    items := []FileInfo{}
    files, err := os.ReadDir(dirPath)
    if err != nil {
        utils.Error("获取目录内容时出错", utils.String("path", dirPath), utils.Err(err))
        return items
    }
    for _, file := range files {
        info, err := file.Info()
        if err != nil {
            utils.Error("获取文件信息失败", utils.Err(err))
            continue
        }
        if !info.IsDir() && strings.HasPrefix(file.Name(), ".") && strings.HasSuffix(file.Name(), ".uploading") {
            continue
        }
        itemPath := filepath.Join(dirPath, file.Name())
        itemInfo := getFileInfo(itemPath, rootName)
        if hashMap != nil {
            if h, ok := hashMap[itemInfo.Path]; ok {
                itemInfo.Xxh3Hash = h
            }
        }
        if itemInfo.Type == "file" && !isFileAllowed(itemInfo.Name) {
            continue
        }
        items = append(items, itemInfo)
    }
    return items
}

// EncodeResponse 编码并发送JSON响应（用于标准 http.ResponseWriter）
// 委托给 response 包
func EncodeResponse(w http.ResponseWriter, data interface{}, err string, statusCode int) {
    response.EncodeResponse(w, data, err, statusCode)
}

// AddUploadRecord 添加上传记录
// 委托给 response 包
func AddUploadRecord(ipAddress string, filename string, filePath string) {
    response.AddUploadRecord(ipAddress, filename, filePath)
}

// CheckDirectoryQuota 检查共享目录配额
// 委托给 response 包
func CheckDirectoryQuota(rootPath string, quota int64, additionalSize int64) error {
    return response.CheckDirectoryQuota(rootPath, quota, additionalSize)
}