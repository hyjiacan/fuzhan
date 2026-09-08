package service

import (
	"fmt"
	"os/exec"
	"strings"
)

// WindowsInstaller Windows 服务安装器
type WindowsInstaller struct {
	opts InstallOptions
}

// NewWindowsInstaller 创建 Windows 安装器
func NewWindowsInstaller(opts InstallOptions) *WindowsInstaller {
	return &WindowsInstaller{opts: opts}
}

// Install 安装服务
func (w *WindowsInstaller) Install() error {
	// 检查是否以管理员权限运行
	if !IsAdmin() {
		return fmt.Errorf("安装服务需要管理员权限，请右键选择\"以管理员身份运行\"")
	}

	// 检查服务是否已存在
	status, _ := w.Status()
	if status.Installed {
		return fmt.Errorf("服务已存在，如需重新安装请先卸载")
	}

	// 构建 sc create 命令
	displayName := "Fuzhan File Sharing Service"
	binaryPath := w.opts.BinaryPath
	if w.opts.ConfigPath != "" {
		binaryPath = fmt.Sprintf(`"%s" -c "%s"`, binaryPath, w.opts.ConfigPath)
	}

	// 创建服务
	cmd := exec.Command("sc", "create", w.opts.Name, "binPath=", binaryPath, "DisplayName=", displayName, "start=", "auto")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建服务失败: %s", string(output))
	}

	// 设置服务描述
	descCmd := exec.Command("sc", "description", w.opts.Name, "轻量级文件共享服务")
	descCmd.Run()

	// 启动服务
	startCmd := exec.Command("sc", "start", w.opts.Name)
	startOutput, startErr := startCmd.CombinedOutput()
	if startErr != nil {
		// 服务创建成功但启动失败，不算错误
		fmt.Printf("服务创建成功，但启动失败: %s\n", string(startOutput))
	}

	fmt.Printf("服务安装成功！服务名称: %s\n", w.opts.Name)
	fmt.Println("您可以使用以下命令管理服务:")
	fmt.Printf("  启动服务: sc start %s\n", w.opts.Name)
	fmt.Printf("  停止服务: sc stop %s\n", w.opts.Name)
	fmt.Printf("  查看状态: sc query %s\n", w.opts.Name)

	return nil
}

// Uninstall 卸载服务
func (w *WindowsInstaller) Uninstall() error {
	// 检查是否以管理员权限运行
	if !IsAdmin() {
		return fmt.Errorf("卸载服务需要管理员权限，请右键选择\"以管理员身份运行\"")
	}

	// 检查服务是否存在
	status, _ := w.Status()
	if !status.Installed {
		return fmt.Errorf("服务不存在，无需卸载")
	}

	// 停止服务（如果正在运行）
	stopCmd := exec.Command("sc", "stop", w.opts.Name)
	stopCmd.Run()

	// 删除服务
	deleteCmd := exec.Command("sc", "delete", w.opts.Name)
	output, err := deleteCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("删除服务失败: %s", string(output))
	}

	fmt.Printf("服务卸载成功！\n")
	return nil
}

// Status 获取服务状态
func (w *WindowsInstaller) Status() (ServiceStatus, error) {
	status := ServiceStatus{}

	// 查询服务状态
	cmd := exec.Command("sc", "query", w.opts.Name)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		// 服务不存在
		if strings.Contains(outputStr, "does not exist") || strings.Contains(outputStr, "不存在") {
			status.Installed = false
			status.Running = false
			status.Message = "服务未安装"
			return status, nil
		}
		return status, fmt.Errorf("查询服务状态失败: %s", outputStr)
	}

	status.Installed = true

	// 检查是否运行
	if strings.Contains(outputStr, "RUNNING") || strings.Contains(outputStr, "运行") {
		status.Running = true
		status.Message = "服务正在运行"
	} else if strings.Contains(outputStr, "STOPPED") || strings.Contains(outputStr, "停止") {
		status.Running = false
		status.Message = "服务已停止"
	} else if strings.Contains(outputStr, "PAUSED") || strings.Contains(outputStr, "暂停") {
		status.Running = false
		status.Message = "服务已暂停"
	} else {
		status.Message = "服务状态未知"
	}

	return status, nil
}
