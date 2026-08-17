package service

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "text/template"

    "fuzhan/internal/resources"
)

// DarwinInstaller macOS 服务安装器 (launchd)
type DarwinInstaller struct {
    opts InstallOptions
}

// launchd plist 模板（从 resources 嵌入）
var launchdPlistTemplate = resources.LaunchdPlistTemplate

// NewDarwinInstaller 创建 macOS 安装器
func NewDarwinInstaller(opts InstallOptions) *DarwinInstaller {
    return &DarwinInstaller{opts: opts}
}

// Install 安装服务
func (d *DarwinInstaller) Install() error {
    // 检查是否以 root 权限运行（launchd 服务需要 root 安装到系统目录）
    // 用户目录可以不需要 root
    isUserInstall := d.opts.User != ""

    if !isUserInstall && !IsAdmin() {
        return fmt.Errorf("安装系统服务需要 root 权限，请使用 sudo 运行")
    }

    // 生成 plist 内容
    plistContent, err := d.generatePlistContent()
    if err != nil {
        return fmt.Errorf("生成服务配置文件失败: %w", err)
    }

    // 确定 plist 路径
    var plistPath string
    if isUserInstall {
        // 用户目录安装
        homeDir, _ := os.UserHomeDir()
        launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
        os.MkdirAll(launchAgentsDir, 0755)
        plistPath = filepath.Join(launchAgentsDir, d.opts.Name+".plist")
    } else {
        // 系统目录安装
        plistPath = filepath.Join("/Library/LaunchDaemons", d.opts.Name+".plist")
    }

    // 检查是否已存在
    if _, err := os.Stat(plistPath); err == nil {
        return fmt.Errorf("服务已存在，如需重新安装请先卸载")
    }

    // 写入 plist 文件
    if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
        return fmt.Errorf("写入服务配置文件失败: %w", err)
    }

    // 加载服务
    loadCmd := exec.Command("launchctl", "load", plistPath)
    if err := loadCmd.Run(); err != nil {
        // 加载失败，但文件已创建，用户可以手动加载
        fmt.Printf("服务配置已创建，但加载失败: %v\n", err)
        fmt.Printf("请手动运行: sudo launchctl load %s\n", plistPath)
    }

    fmt.Printf("服务安装成功！服务名称: %s\n", d.opts.Name)
    fmt.Println("您可以使用以下命令管理服务:")
    fmt.Printf("  启动服务: sudo launchctl start %s\n", d.opts.Name)
    fmt.Printf("  停止服务: sudo launchctl stop %s\n", d.opts.Name)
    fmt.Printf("  查看状态: sudo launchctl list | grep %s\n", d.opts.Name)

    return nil
}

// Uninstall 卸载服务
func (d *DarwinInstaller) Uninstall() error {
    // 检查是否以 root 权限运行
    isUserInstall := d.opts.User != ""

    if !isUserInstall && !IsAdmin() {
        return fmt.Errorf("卸载系统服务需要 root 权限，请使用 sudo 运行")
    }

    // 确定 plist 路径
    var plistPath string
    if isUserInstall {
        homeDir, _ := os.UserHomeDir()
        launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
        plistPath = filepath.Join(launchAgentsDir, d.opts.Name+".plist")
    } else {
        plistPath = filepath.Join("/Library/LaunchDaemons", d.opts.Name+".plist")
    }

    // 检查是否存在
    if _, err := os.Stat(plistPath); os.IsNotExist(err) {
        return fmt.Errorf("服务不存在，无需卸载")
    }

    // 停止并卸载服务
    unloadCmd := exec.Command("launchctl", "unload", plistPath)
    unloadCmd.Run()

    // 删除 plist 文件
    if err := os.Remove(plistPath); err != nil {
        return fmt.Errorf("删除服务配置文件失败: %w", err)
    }

    fmt.Printf("服务卸载成功！\n")
    return nil
}

// Status 获取服务状态
func (d *DarwinInstaller) Status() (ServiceStatus, error) {
    status := ServiceStatus{}

    // 确定 plist 路径
    var plistPath string
    if d.opts.User != "" {
        homeDir, _ := os.UserHomeDir()
        launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
        plistPath = filepath.Join(launchAgentsDir, d.opts.Name+".plist")
    } else {
        plistPath = filepath.Join("/Library/LaunchDaemons", d.opts.Name+".plist")
    }

    // 检查 plist 是否存在
    if _, err := os.Stat(plistPath); os.IsNotExist(err) {
        status.Installed = false
        status.Running = false
        status.Message = "服务未安装"
        return status, nil
    }

    status.Installed = true

    // 查询服务状态
    cmd := exec.Command("launchctl", "list")
    output, err := cmd.Output()
    if err != nil {
        status.Message = "服务状态未知"
        return status, nil
    }

    // 解析输出查找服务
    lines := strings.Split(string(output), "\n")
    for _, line := range lines {
        if strings.Contains(line, d.opts.Name) {
            parts := strings.Fields(line)
            if len(parts) >= 3 {
                // 格式: PID Status Label
                status.Running = parts[0] != "-" && parts[0] != "PID"
                if status.Running {
                    status.Message = "服务正在运行"
                } else {
                    status.Message = "服务已停止"
                }
                return status, nil
            }
        }
    }

    status.Message = "服务已安装但未运行"
    return status, nil
}

// generatePlistContent 生成 plist 内容
func (d *DarwinInstaller) generatePlistContent() (string, error) {
    tmpl, err := template.New("plist").Parse(launchdPlistTemplate)
    if err != nil {
        return "", err
    }

    data := struct {
        Label      string
        BinaryPath string
        ConfigPath string
        Name       string
    }{
        Label:      "com.fuzhan." + d.opts.Name,
        BinaryPath: d.opts.BinaryPath,
        ConfigPath: d.opts.ConfigPath,
        Name:       d.opts.Name,
    }

    var sb strings.Builder
    if err := tmpl.Execute(&sb, data); err != nil {
        return "", err
    }

    return sb.String(), nil
}