package service

import (
    "fmt"
    "os"
    "os/exec"
    "runtime"
)

// ServiceInstaller 服务安装器接口
type ServiceInstaller interface {
    Install() error
    Uninstall() error
    Status() (ServiceStatus, error)
}

// ServiceStatus 服务状态
type ServiceStatus struct {
    Installed bool   // 是否已安装
    Running   bool   // 是否运行中
    Message   string // 状态信息
}

// InstallOptions 安装选项
type InstallOptions struct {
    BinaryPath string // 二进制文件路径
    ConfigPath string // 配置文件路径
    Name       string // 服务名称
    User       string // 运行用户（可选）
}

// NewServiceInstaller 创建适合当前平台的服务安装器
func NewServiceInstaller(opts InstallOptions) (ServiceInstaller, error) {
    if opts.BinaryPath == "" {
        // 获取当前可执行文件路径
        execPath, err := os.Executable()
        if err != nil {
            return nil, fmt.Errorf("无法获取可执行文件路径: %w", err)
        }
        opts.BinaryPath = execPath
    }

    if opts.Name == "" {
        opts.Name = "fuzhan"
    }

    switch runtime.GOOS {
    case "windows":
        return NewWindowsInstaller(opts), nil
    case "linux":
        return NewLinuxInstaller(opts), nil
    case "darwin":
        return NewDarwinInstaller(opts), nil
    default:
        return nil, fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
    }
}

// RunAsAdmin 以管理员权限运行命令（Windows）
func RunAsAdmin(cmd string, args ...string) error {
    if runtime.GOOS != "windows" {
        return runCommand(cmd, args...)
    }

    // Windows 需要以管理员权限运行
    execPath, err := os.Executable()
    if err != nil {
        return err
    }

    // 使用 runas 以管理员权限启动新进程
    runArgs := []string{"runas", "/user:Administrator", execPath, cmd}
    for _, arg := range args {
        runArgs = append(runArgs, arg)
    }

    cmdObj := exec.Command("cmd.exe", runArgs...)
    cmdObj.Stdin = os.Stdin
    cmdObj.Stdout = os.Stdout
    cmdObj.Stderr = os.Stderr
    return cmdObj.Run()
}

// IsAdmin 检查是否有管理员权限
func IsAdmin() bool {
    if runtime.GOOS != "windows" {
        // Unix 系统检查 EUID
        return os.Geteuid() == 0
    }

    // Windows 检查是否以管理员权限运行
    tryCmd := exec.Command("net", "session")
    err := tryCmd.Run()
    return err == nil
}

// runCommand 运行命令并返回结果
func runCommand(name string, args ...string) error {
    cmd := exec.Command(name, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

// runCommandWithOutput 运行命令并返回输出
func runCommandWithOutput(name string, args ...string) (string, error) {
    cmd := exec.Command(name, args...)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return string(output), err
    }
    return string(output), nil
}