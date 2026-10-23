package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"fuzhan/internal/utils/service"

	"github.com/gin-gonic/gin"
)

func main() {
	// 命令行参数解析
	createAdmin := flag.String("create-admin", "", "创建管理员用户，格式: username:password")
	configPath := flag.String("c", "", "配置文件路径")
	extractWeb := flag.Bool("extract-web", false, "将嵌入的 web 资源释放到可执行文件所在目录")

	// 服务安装参数（使用 Bool 检测是否存在，Name 参数在后面）
	var installFlag, uninstallFlag, statusFlag bool
	flag.BoolVar(&installFlag, "install", false, "安装服务")
	flag.BoolVar(&uninstallFlag, "uninstall", false, "卸载服务")
	flag.BoolVar(&statusFlag, "status", false, "查看服务状态")
	serviceName := flag.String("name", "fuzhan", "服务名称")
	serviceUser := flag.String("user", "", "服务运行用户 (Linux/macOS)")

	flag.Parse()

	// 提取 web 资源到可执行文件所在目录
	if *extractWeb {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "获取可执行文件路径失败: %v\n", err)
			os.Exit(1)
		}
		targetDir := filepath.Dir(exe)
		if err := extractWebAssets(webAssets, targetDir, "web", "scalar"); err != nil {
			fmt.Fprintf(os.Stderr, "提取失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Web 与 Scalar 资源已释放到 %s 目录\n", targetDir)
		os.Exit(0)
		return
	}

	if installFlag || uninstallFlag || statusFlag {
		installer, err := service.NewServiceInstaller(service.InstallOptions{
			BinaryPath: "",
			ConfigPath: *configPath,
			Name:       *serviceName,
			User:       *serviceUser,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}

		if installFlag {
			if err := installer.Install(); err != nil {
				fmt.Fprintf(os.Stderr, "安装失败: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
			return
		} else if uninstallFlag {
			if err := installer.Uninstall(); err != nil {
				fmt.Fprintf(os.Stderr, "卸载失败: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
			return
		} else if statusFlag {
			status, err := installer.Status()
			if err != nil {
				fmt.Fprintf(os.Stderr, "查询状态失败: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("服务状态: %s\n", status.Message)
			os.Exit(0)
			return
		}
	}

	// 设置为发布模式以提高性能
	gin.SetMode(gin.ReleaseMode)

	// -create-admin：完成基础设施初始化后创建/更新管理员并退出
	if *createAdmin != "" {
		bootstrap(*configPath, *createAdmin)
		return
	}

	// 正常启动（配置加载 → 服务组装 → 路由 → 主循环热重启）
	runServer(*configPath)
}
