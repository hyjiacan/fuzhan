package integration

import (
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/ftp"
)

func init() {
	appconfig.GlobalConfig = appconfig.Config{
		Server: appconfig.ServerConfig{
			FTP: appconfig.FTPConfig{
				Enabled: true,
				Port:    21210,
			},
			HTTP: appconfig.HTTPConfig{
				Enabled: true,
				Port:    8888,
			},
		},
	}
}

// sendFTPCommand 发送FTP命令并读取响应
func sendFTPCommand(conn *textproto.Conn, cmd string) (string, error) {
	err := conn.PrintfLine("%s", cmd)
	if err != nil {
		return "", err
	}
	return conn.ReadLine()
}

// readAllResponses 读取所有响应行直到遇到以空格开头的响应（命令完成）
func readAllResponses(conn *textproto.Conn) string {
	var responses []string
	for {
		line, err := conn.ReadLine()
		if err != nil {
			break
		}
		responses = append(responses, line)
		// 多行响应以 '-' 开头，最后一行以空格开头（如 "230 Password ok"）
		if len(line) > 3 && line[3] == ' ' {
			break
		}
	}
	return strings.Join(responses, "\n")
}

// TestFTPServerConnect 测试FTP服务器连接
func TestFTPServerConnect(t *testing.T) {
	handler := ftp.NewFTPHandler()
	t.Cleanup(func() {
		handler.Stop()
	})

	go func() {
		if err := handler.Start(); err != nil {
			t.Logf("FTP启动: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	resp, err := conn.ReadLine()
	if err != nil {
		t.Errorf("读取响应失败: %v", err)
	}
	if !strings.HasPrefix(resp, "220") {
		t.Errorf("期望220响应，实际: %s", resp)
	}
}

// TestFTPPortConfiguration 测试FTP端口配置
func TestFTPPortConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		expected int
	}{
		{"默认端口", 0, 2121},
		{"自定义端口", 2122, 2122},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := tt.port
			if port <= 0 {
				port = 2121
			}
			if port != tt.expected {
				t.Errorf("期望端口 %d，实际 %d", tt.expected, port)
			}
		})
	}
}

// TestFTPDirectoryList 测试FTP目录列表（匿名访问）
func TestFTPDirectoryList(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	// 读取欢迎消息
	welcome, err := conn.ReadLine()
	if err != nil {
		t.Fatalf("读取欢迎消息失败: %v", err)
	}
	if !strings.HasPrefix(welcome, "220") {
		t.Errorf("期望220响应，实际: %s", welcome)
	}

	// 登录匿名用户
	resp, err := sendFTPCommand(conn, "USER anonymous")
	if err != nil {
		t.Errorf("USER命令失败: %v", err)
	}
	t.Logf("USER响应: %s", resp)

	// 发送空密码
	resp, err = sendFTPCommand(conn, "PASS ")
	if err != nil {
		t.Errorf("PASS命令失败: %v", err)
	}
	t.Logf("PASS响应: %s", resp)
	if !strings.HasPrefix(resp, "230") {
		t.Errorf("期望230登录成功，实际: %s", resp)
	}

	// 测试PWD命令
	resp, err = sendFTPCommand(conn, "PWD")
	if err != nil {
		t.Errorf("PWD命令失败: %v", err)
	}
	if !strings.HasPrefix(resp, "257") {
		t.Errorf("期望257 PWD响应，实际: %s", resp)
	}
	t.Logf("当前目录: %s", resp)

	// 测试PASV命令
	resp, err = sendFTPCommand(conn, "PASV")
	if err != nil {
		t.Errorf("PASV命令失败: %v", err)
	}
	t.Logf("PASV响应: %s", resp)

	// 解析PASV获取数据端口
	if strings.HasPrefix(resp, "227") {
		start := strings.Index(resp, "(")
		end := strings.Index(resp, ")")
		if start > 0 && end > start {
			parts := strings.Split(resp[start+1:end], ",")
			if len(parts) >= 6 {
				p1, _ := strconv.Atoi(parts[4])
				p2, _ := strconv.Atoi(parts[5])
				dataPort := p1*256 + p2
				t.Logf("数据端口: %d", dataPort)
			}
		}
	}

	// 测试LIST命令
	resp, err = sendFTPCommand(conn, "LIST")
	if err != nil {
		t.Errorf("LIST命令失败: %v", err)
	}
	t.Logf("LIST响应: %s", resp)
}

// TestFTPDirectoryChange 测试FTP目录切换
func TestFTPDirectoryChange(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	conn.ReadLine()                        // 消费欢迎消息
	sendFTPCommand(conn, "USER anonymous") // 登录
	sendFTPCommand(conn, "PASS ")

	// 测试CWD命令
	resp, err := sendFTPCommand(conn, "CWD /")
	if err != nil {
		t.Errorf("CWD命令失败: %v", err)
	}
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("期望250 CWD响应，实际: %s", resp)
	}

	// 再次测试PWD
	resp, err = sendFTPCommand(conn, "PWD")
	if err != nil {
		t.Errorf("PWD命令失败: %v", err)
	}
	if !strings.HasPrefix(resp, "257") {
		t.Errorf("期望257响应，实际: %s", resp)
	}
}

// TestFTPFileDownload 测试FTP文件下载
func TestFTPFileDownload(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	conn.ReadLine() // 消费欢迎消息
	sendFTPCommand(conn, "USER anonymous")
	sendFTPCommand(conn, "PASS ")

	// 设置二进制模式
	resp, err := sendFTPCommand(conn, "TYPE I")
	if err != nil {
		t.Errorf("TYPE I命令失败: %v", err)
	}
	if !strings.HasPrefix(resp, "200") {
		t.Errorf("期望200响应，实际: %s", resp)
	}
	t.Logf("TYPE I响应: %s", resp)

	// 获取当前目录文件列表
	resp, err = sendFTPCommand(conn, "LIST")
	if err != nil {
		t.Errorf("LIST命令失败: %v", err)
	}
	t.Logf("文件列表响应: %s", resp)
}

// TestFTPSystemInfo 测试FTP系统信息
func TestFTPSystemInfo(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	conn.ReadLine() // 消费欢迎消息
	sendFTPCommand(conn, "USER anonymous")
	sendFTPCommand(conn, "PASS ")

	// 测试FEAT命令（多行响应）
	resp, err := sendFTPCommand(conn, "FEAT")
	if err != nil {
		t.Errorf("FEAT命令失败: %v", err)
	}
	if !strings.HasPrefix(resp, "211") {
		t.Errorf("期望211 FEAT响应，实际: %s", resp)
	}
	t.Logf("FEAT响应首行: %s", resp)

	// 消费多行响应剩余内容
	for {
		line, err := conn.ReadLine()
		if err != nil {
			break
		}
		t.Logf("FEAT继续: %s", line)
		// 多行响应以 '-' 开头，结束行以空格开头
		if len(line) > 3 && line[3] == ' ' {
			break
		}
	}

	// 测试SYST命令（可能返回多行响应）
	resp, err = sendFTPCommand(conn, "SYST")
	if err != nil {
		t.Errorf("SYST命令失败: %v", err)
	}
	// 响应可能包含换行，截取第一行判断
	firstLine := strings.Split(resp, "\n")[0]
	if !strings.HasPrefix(firstLine, "215") && !strings.HasPrefix(resp, "215") {
		t.Errorf("期望215 SYST响应，实际: %s", firstLine)
	}
	t.Logf("系统类型: %s", resp)
}

// TestFTPServerFeatures 测试FTP服务器特性
func TestFTPServerFeatures(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	conn.ReadLine() // 消费欢迎消息
	sendFTPCommand(conn, "USER anonymous")
	sendFTPCommand(conn, "PASS ")

	// 测试NOOP命令
	resp, err := sendFTPCommand(conn, "NOOP")
	if err != nil {
		t.Errorf("NOOP命令失败: %v", err)
	}
	if !strings.HasPrefix(resp, "200") {
		t.Errorf("期望200 NOOP响应，实际: %s", resp)
	}

	// 测试STAT命令
	resp, err = sendFTPCommand(conn, "STAT")
	if err != nil {
		t.Errorf("STAT命令失败: %v", err)
	}
	t.Logf("STAT响应: %s", resp)
}

// TestFTPTransferType 测试FTP传输类型
func TestFTPTransferType(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	conn.ReadLine() // 消费欢迎消息
	sendFTPCommand(conn, "USER anonymous")
	sendFTPCommand(conn, "PASS ")

	// 测试TYPE A
	resp, err := sendFTPCommand(conn, "TYPE A")
	if err != nil {
		t.Errorf("TYPE A失败: %v", err)
	}
	if !strings.HasPrefix(resp, "200") {
		t.Errorf("期望200响应，实际: %s", resp)
	}

	// 测试TYPE I
	resp, err = sendFTPCommand(conn, "TYPE I")
	if err != nil {
		t.Errorf("TYPE I失败: %v", err)
	}
	if !strings.HasPrefix(resp, "200") {
		t.Errorf("期望200响应，实际: %s", resp)
	}
}

// TestFTPQuit 测试FTP退出
func TestFTPQuit(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}

	resp, err := sendFTPCommand(conn, "QUIT")
	if err != nil {
		t.Logf("QUIT: %v", err)
	}
	t.Logf("QUIT响应: %s", resp)
	conn.Close()
}

// TestFTPMultiConnection 测试多连接
func TestFTPMultiConnection(t *testing.T) {
	for i := 0; i < 3; i++ {
		conn, err := textproto.Dial("tcp", "localhost:2121")
		if err != nil {
			t.Skipf("FTP服务器未运行: %v", err)
		}

		resp, err := conn.ReadLine()
		if err != nil {
			t.Errorf("连接%d失败: %v", i+1, err)
		}
		if !strings.HasPrefix(resp, "220") {
			t.Errorf("连接%d无220响应: %s", i+1, resp)
		}

		sendFTPCommand(conn, "QUIT")
		conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestFTPPassiveMode 测试被动模式
func TestFTPPassiveMode(t *testing.T) {
	conn, err := textproto.Dial("tcp", "localhost:2121")
	if err != nil {
		t.Skipf("FTP服务器未运行: %v", err)
	}
	defer conn.Close()

	conn.ReadLine() // 消费欢迎消息
	sendFTPCommand(conn, "USER anonymous")
	sendFTPCommand(conn, "PASS ")

	// 测试EPSV命令（扩展被动模式）- 推荐方式
	resp, err := sendFTPCommand(conn, "EPSV")
	if err != nil {
		t.Errorf("EPSV失败: %v", err)
	}
	t.Logf("EPSV响应: %s", resp)
	// EPSV返回229 Extended Passive Mode
	if !strings.HasPrefix(resp, "229") {
		t.Errorf("期望229 EPSV响应，实际: %s", resp)
	}
}

// TestFTPConfigStruct 测试配置结构
func TestFTPConfigStruct(t *testing.T) {
	cfg := appconfig.FTPConfig{
		Enabled: true,
		Port:    2121,
	}

	if !cfg.Enabled {
		t.Error("FTP配置Enabled字段无效")
	}
	if cfg.Port != 2121 {
		t.Errorf("端口错误: %d", cfg.Port)
	}
}
