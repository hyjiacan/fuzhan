package integration

import (
    "fmt"
    "io"
    "net"
    "net/textproto"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
    "time"

    "fuzhan/internal/appconfig"
    "fuzhan/internal/ftp"
)

// FTP test port — use a high port to avoid conflicts
const ftpTestPort = 21210

func init() {
    appconfig.GlobalConfig = appconfig.Config{
        Server: appconfig.ServerConfig{
            Host: "127.0.0.1",
            FTP: appconfig.FTPConfig{
                Enabled: true,
                Port:    ftpTestPort,
            },
            HTTP: appconfig.HTTPConfig{
                Enabled: true,
                Port:    8888,
            },
        },
    }
}

// ftpSend sends an FTP command and reads the response line.
func ftpSend(conn *textproto.Conn, cmd string) (string, error) {
    if err := conn.PrintfLine("%s", cmd); err != nil {
        return "", err
    }
    return conn.ReadLine()
}

// ftpReadAll reads all response lines from a multi-line FTP response.
func ftpReadAll(conn *textproto.Conn) string {
    var responses []string
    for {
        line, err := conn.ReadLine()
        if err != nil {
            break
        }
        responses = append(responses, line)
        if len(line) > 3 && line[3] == ' ' {
            break
        }
    }
    return strings.Join(responses, "\n")
}

// ftpPasv sends PASV and returns the data channel address (host:port).
func ftpPasv(conn *textproto.Conn) (string, error) {
    resp, err := ftpSend(conn, "PASV")
    if err != nil {
        return "", fmt.Errorf("PASV command failed: %w", err)
    }
    if !strings.HasPrefix(resp, "227") {
        return "", fmt.Errorf("PASV unexpected response: %s", resp)
    }

    // Parse 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2)
    start := strings.Index(resp, "(")
    end := strings.Index(resp, ")")
    if start < 0 || end < 0 || end <= start {
        return "", fmt.Errorf("cannot parse PASV response: %s", resp)
   }
    parts := strings.Split(resp[start+1:end], ",")
    if len(parts) < 6 {
        return "", fmt.Errorf("PASV response has %d parts, expected 6: %s", len(parts), resp)
    }
    host := strings.Join(parts[:4], ".")
    p1, _ := strconv.Atoi(parts[4])
    p2, _ := strconv.Atoi(parts[5])
    port := p1*256 + p2
    return fmt.Sprintf("%s:%d", host, port), nil
}

// startFTPTestServer starts the FTP server with test directories and files.
// Returns a cleanup function.
func startFTPTestServer(t *testing.T, rootName string) string {
    t.Helper()

    rootDir := t.TempDir()
    appconfig.RootNames[rootName] = rootDir

    handler := ftp.NewFTPHandler()
    t.Cleanup(func() {
        handler.Stop()
        delete(appconfig.RootNames, rootName)
    })

    go func() {
        if err := handler.Start(); err != nil {
            t.Logf("FTP server exited: %v", err)
        }
    }()

    // Wait for server to be ready
    time.Sleep(500 * time.Millisecond)
    return rootDir
}

// ftpConnect connects to the test FTP server and logs in anonymously.
func ftpConnect(t *testing.T) *textproto.Conn {
    t.Helper()

    conn, err := textproto.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ftpTestPort))
    if err != nil {
        t.Fatalf("FTP connection failed: %v", err)
    }
    t.Cleanup(func() { conn.Close() })

    // Read welcome
    welcome, err := conn.ReadLine()
    if err != nil {
        t.Fatalf("read welcome failed: %v", err)
    }
    if !strings.HasPrefix(welcome, "220") {
        t.Fatalf("expected 220 welcome, got: %s", welcome)
    }

    // Login anonymous
    resp, err := ftpSend(conn, "USER anonymous")
    if err != nil {
        t.Fatalf("USER failed: %v", err)
    }
    if !strings.HasPrefix(resp, "331") && !strings.HasPrefix(resp, "230") {
        t.Fatalf("expected 331 or 230 for USER, got: %s", resp)
    }

    resp, err = ftpSend(conn, "PASS test@test.com")
    if err != nil {
        t.Fatalf("PASS failed: %v", err)
    }
    if !strings.HasPrefix(resp, "230") {
        t.Fatalf("expected 230 for PASS, got: %s", resp)
    }

    return conn
}

// ---- Tests ----

func TestFTPE2E_ConnectAndWelcome(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    resp, err := ftpSend(conn, "NOOP")
    if err != nil {
        t.Fatalf("NOOP failed: %v", err)
    }
    if !strings.HasPrefix(resp, "200") {
        t.Errorf("expected 200 for NOOP, got: %s", resp)
    }
}

func TestFTPE2E_RootListing(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // PASV for data transfer
    dataAddr, err := ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }

    dataConn, err := net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }
    defer dataConn.Close()

    // LIST on root — should show "public"
    resp, err := ftpSend(conn, "LIST")
    if err != nil {
        t.Fatalf("LIST failed: %v", err)
    }
    if !strings.HasPrefix(resp, "150") && !strings.HasPrefix(resp, "125") {
        t.Fatalf("expected 150 or 125 for LIST, got: %s", resp)
    }

    listing, _ := io.ReadAll(dataConn)
    listStr := string(listing)

    if !strings.Contains(listStr, "public") {
        t.Errorf("root listing should contain 'public', got: %s", listStr)
    }
}

func TestFTPE2E_PublicDirListing(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // Change to /public
    resp, err := ftpSend(conn, "CWD /public")
    if err != nil {
        t.Fatalf("CWD failed: %v", err)
    }
    if !strings.HasPrefix(resp, "250") {
        t.Errorf("expected 250 for CWD, got: %s", resp)
    }

    // PWD should confirm
    resp, err = ftpSend(conn, "PWD")
    if err != nil {
        t.Fatalf("PWD failed: %v", err)
    }
    t.Logf("PWD: %s", resp)

    // List public contents
    dataAddr, err := ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }

    dataConn, err := net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }
    defer dataConn.Close()

    resp, err = ftpSend(conn, "LIST")
    if err != nil {
        t.Fatalf("LIST failed: %v", err)
    }
    t.Logf("LIST response: %s", resp)

    listing, _ := io.ReadAll(dataConn)
    t.Logf("Public listing: %s", string(listing))
}

func TestFTPE2E_DownloadFile(t *testing.T) {
    rootDir := startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // Create a test file on disk
    testFileName := "hello.txt"
    testContent := "Hello FTP E2E!"
    dir := filepath.Join(rootDir, testFileName)
    if err := os.WriteFile(dir, []byte(testContent), 0644); err != nil {
        t.Fatalf("create test file failed: %v", err)
    }

    // Download via FTP: RETR public/files/hello.txt
    filePath := fmt.Sprintf("public/files/%s", testFileName)

    dataAddr, err := ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }

    dataConn, err := net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }
    defer dataConn.Close()

    resp, err := ftpSend(conn, fmt.Sprintf("RETR %s", filePath))
    if err != nil {
        t.Fatalf("RETR failed: %v", err)
    }
    if !strings.HasPrefix(resp, "150") && !strings.HasPrefix(resp, "125") {
        t.Fatalf("expected 150 or 125 for RETR, got: %s", resp)
    }

    downloaded, _ := io.ReadAll(dataConn)
    if string(downloaded) != testContent {
        t.Errorf("content mismatch: got %q, want %q", string(downloaded), testContent)
    }
}

func TestFTPE2E_UploadFile(t *testing.T) {
    rootDir := startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // Upload via FTP: STOR public/files/uploaded.txt
    content := "uploaded via FTP"
    filePath := "public/files/uploaded.txt"

    dataAddr, err := ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }

    dataConn, err := net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }

    resp, err := ftpSend(conn, fmt.Sprintf("STOR %s", filePath))
    if err != nil {
        t.Fatalf("STOR failed: %v", err)
    }
    if !strings.HasPrefix(resp, "150") && !strings.HasPrefix(resp, "125") {
        t.Fatalf("expected 150 or 125 for STOR, got: %s", resp)
    }

    // Send data and close
    dataConn.Write([]byte(content))
    dataConn.Close()

    // Read transfer complete response
    resp, err = conn.ReadLine()
    if err != nil {
        t.Fatalf("read STOR complete response failed: %v", err)
    }
    if !strings.HasPrefix(resp, "226") {
        t.Errorf("expected 226 for STOR complete, got: %s", resp)
    }

    // Verify on disk
    data, err := os.ReadFile(filepath.Join(rootDir, "uploaded.txt"))
    if err != nil {
        t.Fatalf("read uploaded file failed: %v", err)
    }
    if string(data) != content {
        t.Errorf("uploaded content mismatch: got %q, want %q", string(data), content)
    }
}

func TestFTPE2E_FileContentRoundTrip(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    original := "Round-trip content via FTP!"

    // Upload
    dataAddr, err := ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }
    dataConn, err := net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }
    resp, err := ftpSend(conn, "STOR public/files/roundtrip.txt")
    if err != nil {
        t.Fatalf("STOR failed: %v", err)
    }
    if !strings.HasPrefix(resp, "150") && !strings.HasPrefix(resp, "125") {
        t.Fatalf("expected 150 or 125 for STOR, got: %s", resp)
    }
    dataConn.Write([]byte(original))
    dataConn.Close()
    conn.ReadLine() // 226 Transfer complete

    // Download
    dataAddr, err = ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }
    dataConn, err = net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }
    resp, err = ftpSend(conn, "RETR public/files/roundtrip.txt")
    if err != nil {
        t.Fatalf("RETR failed: %v", err)
    }
    if !strings.HasPrefix(resp, "150") && !strings.HasPrefix(resp, "125") {
        t.Fatalf("expected 150 or 125 for RETR, got: %s", resp)
    }

    downloaded, _ := io.ReadAll(dataConn)
    dataConn.Close()
    conn.ReadLine() // 226 Transfer complete

    if string(downloaded) != original {
        t.Errorf("round-trip content mismatch: got %q, want %q", string(downloaded), original)
    }
}

func TestFTPE2E_FileSizeAndType(t *testing.T) {
    rootDir := startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // Create binary test file
    binaryContent := make([]byte, 1024)
    for i := range binaryContent {
        binaryContent[i] = byte(i % 256)
    }
    os.WriteFile(filepath.Join(rootDir, "data.bin"), binaryContent, 0644)

    // Set binary mode
    resp, err := ftpSend(conn, "TYPE I")
    if err != nil {
        t.Fatalf("TYPE I failed: %v", err)
    }
    if !strings.HasPrefix(resp, "200") {
        t.Errorf("expected 200 for TYPE I, got: %s", resp)
    }

    // Download binary file
    dataAddr, err := ftpPasv(conn)
    if err != nil {
        t.Fatalf("PASV failed: %v", err)
    }
    dataConn, err := net.Dial("tcp", dataAddr)
    if err != nil {
        t.Fatalf("data connection failed: %v", err)
    }
    resp, err = ftpSend(conn, "RETR public/files/data.bin")
    if err != nil {
        t.Fatalf("RETR failed: %v", err)
    }
    if !strings.HasPrefix(resp, "150") && !strings.HasPrefix(resp, "125") {
        t.Fatalf("expected 150 or 125 for RETR, got: %s", resp)
    }

    downloaded, _ := io.ReadAll(dataConn)
    dataConn.Close()

    if len(downloaded) != 1024 {
        t.Errorf("expected 1024 bytes, got %d", len(downloaded))
    }
    for i, b := range downloaded {
        if b != byte(i%256) {
            t.Errorf("byte %d mismatch: got %d, want %d", i, b, byte(i%256))
            break
        }
    }
}

func TestFTPE2E_PassiveMode(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // EPSV (Extended Passive Mode)
    resp, err := ftpSend(conn, "EPSV")
    if err != nil {
        t.Fatalf("EPSV failed: %v", err)
    }
    if !strings.HasPrefix(resp, "229") {
        t.Errorf("expected 229 for EPSV, got: %s", resp)
    }
}

func TestFTPE2E_SystemInfo(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    // SYST
    resp, err := ftpSend(conn, "SYST")
    if err != nil {
        t.Fatalf("SYST failed: %v", err)
    }
    t.Logf("SYST: %s", resp)

    // FEAT
    resp, err = ftpSend(conn, "FEAT")
    if err != nil {
        t.Fatalf("FEAT failed: %v", err)
    }
    t.Logf("FEAT: %s", resp)
    ftpReadAll(conn) // consume rest
}

func TestFTPE2E_Quit(t *testing.T) {
    _ = startFTPTestServer(t, "files")
    conn := ftpConnect(t)

    resp, err := ftpSend(conn, "QUIT")
    if err != nil {
        t.Logf("QUIT error (expected): %v", err)
    } else {
        t.Logf("QUIT: %s", resp)
    }
}
