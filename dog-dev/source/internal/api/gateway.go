package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghpp/ghpp/web"
)

// 飞牛统一网关入口（fnOS gatewaySocket）。
//
// 应用在 ui/config 的入口条目里声明 gatewaySocket / gatewayPrefix 后，
// 平台会把 http://<NAS>:5666/app/<appname>/* 通过应用目录下的 unix socket
// 转发进来；桌面上点图标打开的就是这个地址，与控制台端口解耦
// （控制台端口被用户改掉也不会影响桌面入口）。
//
// 实测要点：
//   - 平台不剥前缀，socket 上收到的路径仍然是 /app/<appname>/...；
//   - 平台把 gatewaySocket 解析为 ${TRIM_APPDEST}/target/<文件名>，
//     而 /var/apps/<appname>/target 指向应用载荷目录，
//     所以 socket 文件建在载荷根目录即可（fpk/cmd/main 传绝对路径）。

// gatewayServer 保存网关监听，供 Stop 时一并关闭。
type gatewayServer struct {
	srv        *http.Server
	socketPath string
	prefix     string
}

// GatewayHandler 返回统一网关入口处理器：剥掉前缀后交给内部路由。
//
// 无论 socket 上的请求是否带前缀都能正确处理（带则剥、不带则原样），
// 同时把前缀放进请求上下文，供 index.html 资源地址与 /ca 页链接重写。
func (s *Server) GatewayHandler(prefix string) http.Handler {
	inner := s.Handler()
	base := strings.TrimSuffix(prefix, "/")
	if base == "" {
		base = "/app/dog-dev"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(web.WithPrefix(r.Context(), base))

		p := r.URL.Path
		switch {
		case p == base:
			// 无尾斜杠访问：等价于首页。
			r2 := cloneRequest(r)
			r2.URL.Path = "/"
			r2.URL.RawPath = ""
			r = r2
		case strings.HasPrefix(p, base+"/"):
			r2 := cloneRequest(r)
			r2.URL.Path = strings.TrimPrefix(p, base)
			r2.URL.RawPath = ""
			r = r2
		}
		inner.ServeHTTP(w, r)
	})
}

// cloneRequest 复制请求，避免改动共享的 URL 指针。
func cloneRequest(r *http.Request) *http.Request {
	r2 := new(http.Request)
	*r2 = *r
	u := new(url.URL)
	*u = *r.URL
	r2.URL = u
	return r2
}

// StartGateway 在 unix socket 上提供统一网关入口。
//
// socketPath 由 fpk/cmd/main 传入（${TRIM_APPDEST}/<name>.sock），平台侧的
// /var/apps/<appname>/target 符号链接正好指向载荷根目录，两边是同一个文件。
func (s *Server) StartGateway(socketPath, prefix string) error {
	if socketPath == "" {
		return nil
	}
	if dir := filepath.Dir(socketPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// 进程被强杀时会残留 socket 文件，不清理会 bind 失败。
	if fi, err := os.Stat(socketPath); err == nil && fi.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(socketPath)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	// 平台以 root 连接；权限放宽到 0666 便于局域网内其它本地用户调试，
	// 控制台本身默认免登录，不因权限位引入新的安全边界。
	_ = os.Chmod(socketPath, 0o666)

	base := strings.TrimSuffix(prefix, "/")
	if base == "" {
		base = "/app/dog-dev"
	}
	srv := &http.Server{
		Handler:           s.GatewayHandler(base),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.gateway = &gatewayServer{srv: srv, socketPath: socketPath, prefix: base}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("error", "统一网关入口退出: %v", err)
		}
	}()

	s.logf("info", "统一网关入口已就绪: %s（前缀 %s）", socketPath, base)
	return nil
}

// stopGateway 关闭网关监听并删除 socket 文件。
func (s *Server) stopGateway(ctx context.Context) {
	g := s.gateway
	if g == nil {
		return
	}
	s.gateway = nil
	_ = g.srv.Shutdown(ctx)
	_ = os.Remove(g.socketPath)
}
