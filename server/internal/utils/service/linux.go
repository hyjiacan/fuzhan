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

// LinuxInstaller Linux 服务安装器 (systemd)
type LinuxInstaller struct {
	opts InstallOptions
}

// systemd 服务模板（从 resources 嵌入）
var systemdServiceTemplate = resources.SystemdServiceTemplate

// NewLinuxInstaller 创建 Linux 安装器
func NewLinuxInstaller(opts InstallOptions) *LinuxInstaller {
	return &LinuxInstaller{opts: opts}
}

// Install 安装服务
func (l *LinuxInstaller) Install() error {
	// 检查是否以 root 权限运行
	if !IsAdmin() {
		return fmt.Errorf("安装服务需要 root 权限，请使用 sudo 运行")
	}

	// 检查 systemd 是否可用
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemd 不可用，此系统不支持服务安装")
	}

	// 检查服务是否已存在
	status, _ := l.Status()
	if status.Installed {
		return fmt.Errorf("服务已存在，如需重新安装请先卸载")
	}

	// 创建服务文件
	serviceContent, err := l.generateServiceContent()
	if err != nil {
		return fmt.Errorf("生成服务文件失败: %w", err)
	}

	// 写入服务文件
	servicePath := filepath.Join("/etc/systemd/system", l.opts.Name+".service")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("写入服务文件失败: %w", err)
	}

	// 重新加载 systemd 配置
	reloadCmd := exec.Command("systemctl", "daemon-reload")
	if err := reloadCmd.Run(); err != nil {
		return fmt.Errorf("重新加载 systemd 配置失败: %w", err)
	}

	// 启用服务
	enableCmd := exec.Command("systemctl", "enable", l.opts.Name)
	if err := enableCmd.Run(); err != nil {
		return fmt.Errorf("启用服务失败: %w", err)
	}

	// 启动服务
	startCmd := exec.Command("systemctl", "start", l.opts.Name)
	startOutput, startErr := startCmd.CombinedOutput()
	if startErr != nil {
		fmt.Printf("服务创建成功，但启动失败: %s\n", string(startOutput))
	}

	fmt.Printf("服务安装成功！服务名称: %s\n", l.opts.Name)
	fmt.Println("您可以使用以下命令管理服务:")
	fmt.Printf("  启动服务: sudo systemctl start %s\n", l.opts.Name)
	fmt.Printf("  停止服务: sudo systemctl stop %s\n", l.opts.Name)
	fmt.Printf("  查看状态: sudo systemctl status %s\n", l.opts.Name)
	fmt.Printf("  查看日志: sudo journalctl -u %s -f\n", l.opts.Name)

	return nil
}

// Uninstall 卸载服务
func (l *LinuxInstaller) Uninstall() error {
	// 检查是否以 root 权限运行
	if !IsAdmin() {
		return fmt.Errorf("卸载服务需要 root 权限，请使用 sudo 运行")
	}

	// 检查服务是否存在
	status, _ := l.Status()
	if !status.Installed {
		return fmt.Errorf("服务不存在，无需卸载")
	}

	// 停止服务
	stopCmd := exec.Command("systemctl", "stop", l.opts.Name)
	stopCmd.Run()

	// 禁用服务
	disableCmd := exec.Command("systemctl", "disable", l.opts.Name)
	disableCmd.Run()

	// 删除服务文件
	servicePath := filepath.Join("/etc/systemd/system", l.opts.Name+".service")
	if err := os.Remove(servicePath); err != nil {
		return fmt.Errorf("删除服务文件失败: %w", err)
	}

	// 重新加载 systemd 配置
	reloadCmd := exec.Command("systemctl", "daemon-reload")
	reloadCmd.Run()

	fmt.Printf("服务卸载成功！\n")
	return nil
}

// Status 获取服务状态
func (l *LinuxInstaller) Status() (ServiceStatus, error) {
	status := ServiceStatus{}

	// 检查服务文件是否存在
	servicePath := filepath.Join("/etc/systemd/system", l.opts.Name+".service")
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		status.Installed = false
		status.Running = false
		status.Message = "服务未安装"
		return status, nil
	}

	status.Installed = true

	// 查询服务状态
	cmd := exec.Command("systemctl", "is-active", l.opts.Name)
	output, err := cmd.Output()
	if err != nil {
		status.Message = "服务状态未知"
		return status, nil
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "active" {
		status.Running = true
		status.Message = "服务正在运行"
	} else {
		status.Running = false
		status.Message = fmt.Sprintf("服务状态: %s", outputStr)
	}

	return status, nil
}

// generateServiceContent 生成服务文件内容
func (l *LinuxInstaller) generateServiceContent() (string, error) {
	tmpl, err := template.New("service").Parse(systemdServiceTemplate)
	if err != nil {
		return "", err
	}

	data := struct {
		BinaryPath string
		ConfigPath string
		User       string
	}{
		BinaryPath: l.opts.BinaryPath,
		ConfigPath: l.opts.ConfigPath,
		User:       l.opts.User,
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", err
	}

	return sb.String(), nil
}
