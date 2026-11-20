package main

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"fuzhan/internal/utils"
)

// earlyHTTPServer 在 bootstrap 初始化阶段提前占用 HTTP 端口并对外返回 503。
//
// 背景：bootstrap 是同步的（迁移、去重、回填、检索索引构建），端口要等全部完成才监听，
// 数据量大时可能持续数分钟没有响应端口。作为 Windows 服务（nssm）运行时，
// 监控/探活与用户访问都会表现为"启动无响应"。
//
// 生命周期：
//   - bootstrap 阶段（配置加载之后，数据库之前）创建并立即 Serve 占位响应；
//   - 主循环首轮把 handler 切换为正式路由，此后由该轮 HTTP worker 负责优雅关闭；
//   - 配置热重启的后续轮次按常规方式重新监听（地址可能已变更）。
type earlyHTTPServer struct {
	ln  net.Listener
	srv *http.Server

	mu      sync.RWMutex
	handler http.Handler
}

// initializingHandler 初始化期间的占位响应：所有请求统一返回 503 并提示稍后重试。
var initializingHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Retry-After", "5")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"code":503,"message":"服务正在初始化，请稍候","data":null}`))
})

// ServeHTTP 始终转发到当前 handler（初始化期间为占位 503，切换后为正式路由）。
func (e *earlyHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.RLock()
	h := e.handler
	e.mu.RUnlock()
	h.ServeHTTP(w, r)
}

// SetHandler 切换为正式路由（主循环首轮取得控制权时调用）。
func (e *earlyHTTPServer) SetHandler(h http.Handler) {
	e.mu.Lock()
	e.handler = h
	e.mu.Unlock()
}

// Shutdown 优雅关闭提前占用的服务；超时则强制关闭，避免关闭流程被长连接拖住。
func (e *earlyHTTPServer) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.srv.Shutdown(ctx); err != nil {
		utils.Warn("HTTP 服务器优雅关闭超时，强制关闭", utils.Err(err))
		_ = e.srv.Close()
	}
}

// startEarlyHTTPServer 在 addr 上提前监听并开始返回占位 503。
// 监听失败（多为端口被占用）直接返回错误，由调用方决定是否终止启动。
func startEarlyHTTPServer(addr string) (*earlyHTTPServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	e := &earlyHTTPServer{ln: ln, handler: initializingHandler}
	e.srv = &http.Server{Handler: e}
	go func() {
		if serr := e.srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			utils.Error("HTTP 占位服务异常退出", utils.String("address", addr), utils.Err(serr))
		}
	}()
	utils.Info("HTTP 端口已提前监听，初始化期间返回 503", utils.String("address", addr))
	return e, nil
}
