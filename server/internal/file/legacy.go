package file

import (
    "crypto/tls"
    "encoding/json"
    "fmt"
    "io"
    "mime"
    "net"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strings"
    "syscall"
    "time"

    "github.com/jlaffaye/ftp"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/response"
    "fuzhan/internal/utils"
)

// FTPFileInfo FTP 文件信息
type FTPFileInfo struct {
    FileName string
    FileSize int64
    ModTime  time.Time
}

// GetFileInfoFromURL 处理获取 http/https 地址文件信息的请求
func GetFileInfoFromURL(w http.ResponseWriter, r *http.Request) {
    utils.PrintRequestInfo(r)

    // 解析 JSON 请求体
    var req struct {
        URL string `json:"url"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        utils.Error("解析请求错误", utils.Err(err))
        response.EncodeResponse(w, nil, "解析请求错误", http.StatusBadRequest)
        return
    }

    downloadURL := strings.TrimSpace(req.URL)
    if downloadURL == "" {
        utils.Warn("未提供文件 URL")
        response.EncodeResponse(w, nil, "未提供文件 URL", http.StatusBadRequest)
        return
    }

    utils.Info("获取 URL 文件信息", utils.String("url", downloadURL))

    // 获取 SSRF 安全配置
    urlCfg := utils.URLUploadConfig{
        Enabled:         appconfig.GlobalConfig.Upload.URLUpload.Enabled,
        AllowedIPRanges: appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges,
    }

    // 检查 URL 协议
    isFTP := strings.HasPrefix(downloadURL, "ftp://") || strings.HasPrefix(downloadURL, "ftps://")
    isHTTP := strings.HasPrefix(downloadURL, "http://") || strings.HasPrefix(downloadURL, "https://")

    if !isHTTP && !isFTP {
        utils.Warn("不支持的 URL 协议")
        response.EncodeResponse(w, nil, "不支持的 URL 协议，仅支持 http/https/ftp/ftps", http.StatusBadRequest)
        return
    }

    // SSRF 安全校验
    if isFTP {
        // FTP URL 提取主机进行安全校验
        if parsedURL, parseErr := url.Parse(downloadURL); parseErr == nil && parsedURL.Host != "" {
            ftpHost := parsedURL.Host
            if hostOnly, _, portErr := net.SplitHostPort(ftpHost); portErr == nil {
                ftpHost = hostOnly
            }
            if err := utils.IsURLSafe("http://"+ftpHost+"/", urlCfg); err != nil {
                utils.Warn("FTP URL 安全验证失败", utils.String("url", downloadURL), utils.Err(err))
                response.EncodeResponse(w, nil, "FTP URL 安全验证失败: "+err.Error(), http.StatusBadRequest)
                return
            }
        }
    } else {
        if err := utils.IsURLSafe(downloadURL, urlCfg); err != nil {
            utils.Warn("HTTP URL 安全验证失败", utils.String("url", downloadURL), utils.Err(err))
            response.EncodeResponse(w, nil, "URL 安全验证失败: "+err.Error(), http.StatusBadRequest)
            return
        }
    }

    var filename string
    var fileSize int64

    if isFTP {
        // FTP/FTPS 协议：使用 FTP 客户端获取文件信息
        info, err := GetFTPFileInfo(downloadURL)
        if err != nil {
            msg := fmt.Sprintf("获取 FTP 文件信息失败: %v", err)
            utils.Error(msg)
            response.EncodeResponse(w, nil, msg, http.StatusBadRequest)
            return
        }
        filename = info.FileName
        fileSize = info.FileSize
    } else {
        // HTTP/HTTPS 协议使用 HEAD 请求获取文件信息（含 DNS 重绑定防护）
        urlCfg := utils.URLUploadConfig{
            Enabled:         appconfig.GlobalConfig.Upload.URLUpload.Enabled,
            AllowedIPRanges: appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges,
        }
        tr := &http.Transport{
            TLSClientConfig: &tls.Config{
                InsecureSkipVerify: appconfig.GlobalConfig.Upload.URLUpload.InsecureSkipVerify,
            },
        }
        // 添加 DNS 重绑定防护的 Dialer
        protectedDialer := &net.Dialer{
            Timeout:   30 * time.Second,
            KeepAlive: 30 * time.Second,
            Control: func(network, address string, c syscall.RawConn) error {
                host, _, err := net.SplitHostPort(address)
                if err != nil {
                    host = address
                }
                ip := net.ParseIP(host)
                if ip != nil {
                    return utils.CheckIPSafe(ip, urlCfg)
                }
                ips, lookupErr := net.LookupIP(host)
                if lookupErr != nil {
                    return lookupErr
                }
                for _, resolvedIP := range ips {
                    if err := utils.CheckIPSafe(resolvedIP, urlCfg); err != nil {
                        return err
                    }
                }
                return nil
            },
        }
        tr.DialContext = protectedDialer.DialContext
        client := &http.Client{
            Transport: tr,
            CheckRedirect: func(req *http.Request, via []*http.Request) error {
                downloadURL = req.URL.String()
                utils.Info("重定向", utils.String("url", downloadURL))
                return nil
            },
        }

        // 发送 HEAD 请求获取文件信息
        resp, err := client.Head(downloadURL)
        if err != nil {
            utils.Warn("HEAD 请求失败，将用 GET 重试", utils.Err(err))
            // 尝试使用 GET 请求获取文件信息
            resp, err = client.Get(downloadURL)
            if err != nil {
                msg := fmt.Sprintf("HEAD 和 GET 请求均获取文件信息失败: %v", err)
                utils.Error(msg)
                response.EncodeResponse(w, nil, msg, http.StatusBadRequest)
                return
            }
            defer resp.Body.Close()
        } else {
            defer resp.Body.Close()
        }

        // 检查响应状态码
        if resp.StatusCode != http.StatusOK {
            utils.Warn("获取文件信息失败", utils.Int("statusCode", resp.StatusCode))
            response.EncodeResponse(w, nil, fmt.Sprintf("获取文件信息失败，状态码: %d", resp.StatusCode), http.StatusBadRequest)
            return
        }

        // 尝试从响应头中获取文件名
        contentDisposition := resp.Header.Get("Content-Disposition")
        if contentDisposition != "" {
            _, params, err := mime.ParseMediaType(contentDisposition)
            if err == nil {
                if name, ok := params["filename"]; ok {
                    filename = name
                }
            }
        }

        // 如果响应头中没有文件名，从 URL 中提取
        if filename == "" {
            parsedURL, err := url.Parse(downloadURL)
            if err == nil {
                filename = filepath.Base(parsedURL.Path)
            }
        }

        fileSize = resp.ContentLength
    }

    // 构建响应数据
    respData := map[string]interface{}{
        "realUrl":  downloadURL,
        "filename": filename,
        "size":     fileSize,
    }

    // 返回响应
    response.EncodeResponse(w, respData, "", http.StatusOK)
}

// HandleRenameFile 重命名文件的处理器
func HandleRenameFile(w http.ResponseWriter, r *http.Request) {
    utils.PrintRequestInfo(r)

    // 解析请求体
    var requestData struct {
        Path    string `json:"path"`
        NewName string `json:"newName"`
    }

    if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
        response.EncodeResponse(w, nil, "请求数据格式错误", http.StatusBadRequest)
        return
    }

    if requestData.Path == "" || requestData.NewName == "" {
        response.EncodeResponse(w, nil, "路径和新文件名不能为空", http.StatusBadRequest)
        return
    }

    // 获取文件的完整路径
    oldPath, err := getFullPath(requestData.Path)
    if err != nil {
        response.EncodeResponse(w, nil, err.Error(), http.StatusBadRequest)
        return
    }

    // 检查文件是否存在
    if _, err := os.Stat(oldPath); os.IsNotExist(err) {
        response.EncodeResponse(w, nil, "文件不存在: "+oldPath, http.StatusNotFound)
        return
    }

    // 构造新文件路径
    newPath := filepath.Join(filepath.Dir(oldPath), requestData.NewName)

    // 检查新文件名是否已存在
    if _, err := os.Stat(newPath); err == nil {
        response.EncodeResponse(w, nil, "同名文件已存在", http.StatusBadRequest)
        return
    }

    // 重命名文件
    if err := os.Rename(oldPath, newPath); err != nil {
        response.EncodeResponse(w, nil, "重命名失败: "+err.Error(), http.StatusInternalServerError)
        return
    }

    utils.Info("文件重命名", utils.String("oldPath", oldPath), utils.String("newPath", newPath))
    response.EncodeResponse(w, nil, "", http.StatusOK)
}

// HandleMoveFile 移动文件的处理器
func HandleMoveFile(w http.ResponseWriter, r *http.Request) {
    utils.PrintRequestInfo(r)

    // 解析请求体
    var requestData struct {
        Path    string `json:"path"`
        NewPath string `json:"newPath"`
    }

    if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
        response.EncodeResponse(w, nil, "请求数据格式错误", http.StatusBadRequest)
        return
    }

    if requestData.Path == "" || requestData.NewPath == "" {
        response.EncodeResponse(w, nil, "源路径和目标路径不能为空", http.StatusBadRequest)
        return
    }

    // 获取源文件的完整路径
    srcPath, err := getFullPath(requestData.Path)
    if err != nil {
        response.EncodeResponse(w, nil, err.Error(), http.StatusBadRequest)
        return
    }

    // 检查源文件是否存在
    if _, err := os.Stat(srcPath); os.IsNotExist(err) {
        response.EncodeResponse(w, nil, "源文件不存在: "+srcPath, http.StatusNotFound)
        return
    }

    // 获取目标文件的完整路径
    dstPath, err := getFullPath(requestData.NewPath)
    if err != nil {
        response.EncodeResponse(w, nil, err.Error(), http.StatusBadRequest)
        return
    }

    // 检查目标路径是否已存在同名文件
    if _, err := os.Stat(dstPath); err == nil {
        response.EncodeResponse(w, nil, "目标路径已存在同名文件", http.StatusBadRequest)
        return
    }

    // 确保目标目录存在
    dstDir := filepath.Dir(dstPath)
    if err := os.MkdirAll(dstDir, 0755); err != nil {
        response.EncodeResponse(w, nil, "创建目标目录失败: "+err.Error(), http.StatusInternalServerError)
        return
    }

    // 移动文件
    if err := os.Rename(srcPath, dstPath); err != nil {
        response.EncodeResponse(w, nil, "移动文件失败: "+err.Error(), http.StatusInternalServerError)
        return
    }

    utils.Info("文件移动", utils.String("srcPath", srcPath), utils.String("dstPath", dstPath))
    response.EncodeResponse(w, nil, "", http.StatusOK)
}

// getFullPath 根据相对路径获取完整路径
func getFullPath(relPath string) (string, error) {
    // 分割路径获取根目录名
    parts := strings.SplitN(relPath, "/", 2)
    if len(parts) < 1 {
        return "", fmt.Errorf("无效的路径格式")
    }

    rootName := parts[0]
    rootPath, exists := appconfig.RootNames[rootName]
    if !exists {
        return "", fmt.Errorf("根目录 %s 不存在", rootName)
    }

    // 构造完整路径
    var fullPath string
    if len(parts) == 1 {
        fullPath = rootPath
    } else {
        fullPath = filepath.Join(rootPath, parts[1])
    }

    // 检查路径是否越权
    absRootPath, err := filepath.Abs(rootPath)
    if err != nil {
        utils.Error("获取根目录绝对路径失败", utils.String("rootPath", rootPath), utils.Err(err))
        return "", fmt.Errorf("路径解析失败")
    }
    absTargetPath, err := filepath.Abs(fullPath)
    if err != nil {
        utils.Error("获取目标路径绝对路径失败", utils.String("fullPath", fullPath), utils.Err(err))
        return "", fmt.Errorf("路径解析失败")
    }
    if !strings.HasPrefix(absTargetPath, absRootPath) {
        return "", fmt.Errorf("路径越权")
    }

    return fullPath, nil
}

// ===== FTP 相关函数 =====

// parseFTPURL 解析 FTP/FTPS URL，返回 host, user, password, path
func parseFTPURL(rawURL string) (host string, user string, password string, path string, isTLS bool, err error) {
    parsed, parseErr := url.Parse(rawURL)
    if parseErr != nil {
        err = fmt.Errorf("解析 FTP URL 失败: %w", parseErr)
        return
    }

    if parsed.Scheme == "ftps" {
        isTLS = true
    } else if parsed.Scheme == "ftp" {
        isTLS = false
    } else {
        err = fmt.Errorf("不支持的协议: %s", parsed.Scheme)
        return
    }

    host = parsed.Host
    if !strings.Contains(host, ":") {
        host = host + ":21"
    }

    user = "anonymous"
    password = "anonymous@"
    if parsed.User != nil {
        u := parsed.User.Username()
        if u != "" {
            user = u
        }
        p, hasPass := parsed.User.Password()
        if hasPass {
            password = p
        }
    }

    path = parsed.Path
    if path == "" {
        path = "/"
    }

    return
}

// dialFTP 建立 FTP/FTPS 连接并登录
func dialFTP(host string, user string, password string, isTLS bool) (*ftp.ServerConn, error) {
    var c *ftp.ServerConn
    var err error

    opts := []ftp.DialOption{
        ftp.DialWithTimeout(30 * time.Second),
    }
    if isTLS {
        insecure := appconfig.GlobalConfig.Upload.URLUpload.InsecureSkipVerify
        opts = append(opts, ftp.DialWithExplicitTLS(&tls.Config{
            InsecureSkipVerify: insecure,
        }))
    }

    c, err = ftp.Dial(host, opts...)
    if err != nil {
        return nil, fmt.Errorf("连接 FTP 服务器失败: %w", err)
    }

    if err = c.Login(user, password); err != nil {
        c.Quit()
        return nil, fmt.Errorf("FTP 登录失败: %w", err)
    }

    return c, nil
}

// GetFTPFileInfo 获取 FTP 文件信息
func GetFTPFileInfo(rawURL string) (*FTPFileInfo, error) {
    host, user, password, path, isTLS, err := parseFTPURL(rawURL)
    if err != nil {
        return nil, err
    }

    c, err := dialFTP(host, user, password, isTLS)
    if err != nil {
        return nil, err
    }
    defer c.Quit()

    info := &FTPFileInfo{
        FileName: filepath.Base(path),
    }

    size, err := c.FileSize(path)
    if err != nil {
        utils.Warn("FTP 获取文件大小失败", utils.String("path", path), utils.Err(err))
    } else {
        info.FileSize = size
    }

    modTime, err := c.GetTime(path)
    if err != nil {
        utils.Warn("FTP 获取文件修改时间失败", utils.String("path", path), utils.Err(err))
    } else {
        info.ModTime = modTime
    }

    return info, nil
}

// DownloadFromFTP 从 FTP/FTPS 下载文件到本地
// progressCallback 每下载 1MB 回调一次，参数为已下载字节数
func DownloadFromFTP(rawURL string, localPath string, progressCallback func(downloaded int64)) error {
    host, user, password, path, isTLS, err := parseFTPURL(rawURL)
    if err != nil {
        return err
    }

    c, err := dialFTP(host, user, password, isTLS)
    if err != nil {
        return err
    }
    defer c.Quit()

    resp, err := c.Retr(path)
    if err != nil {
        return fmt.Errorf("FTP 下载文件失败: %w", err)
    }
    defer resp.Close()

    outFile, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE, 0644)
    if err != nil {
        return fmt.Errorf("打开本地文件失败: %w", err)
    }
    defer outFile.Close()

    buf := make([]byte, 32*1024)
    var downloaded int64
    var lastProgressUpdate int64

    for {
        n, readErr := resp.Read(buf)
        if n > 0 {
            if _, writeErr := outFile.Write(buf[:n]); writeErr != nil {
                return fmt.Errorf("写入本地文件失败: %w", writeErr)
            }
            downloaded += int64(n)

            if downloaded-lastProgressUpdate >= 1*1024*1024 {
                lastProgressUpdate = downloaded
                if progressCallback != nil {
                    progressCallback(downloaded)
                }
            }
        }
        if readErr == io.EOF {
            break
        }
        if readErr != nil {
            return fmt.Errorf("读取 FTP 数据失败: %w", readErr)
        }
    }

    if progressCallback != nil {
        progressCallback(downloaded)
    }

    return nil
}