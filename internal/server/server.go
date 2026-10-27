// Package server 管理 HTTP 监听器的生命周期：
// SIGINT/SIGTERM 优雅停机，SIGHUP 经 tableflip 零停机热重启
// Package server manages the HTTP listener lifecycle: graceful shutdown on
// SIGINT/SIGTERM and zero-downtime re-exec on SIGHUP via tableflip
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudflare/tableflip"
)

// shutdownTimeout 限制停机与热重启交接时的连接排空时长
// shutdownTimeout bounds connection draining on shutdown and handover
const shutdownTimeout = 30 * time.Second

// Run 监听 addr 并提供服务，直到收到退出信号或被热重启的新进程接管
// Run serves handler on addr until a termination signal or a hot-restart handover
func Run(addr string, handler http.Handler) error {
	upg, err := tableflip.New(tableflip.Options{})
	if err != nil {
		return fmt.Errorf("init upgrade manager: %w", err)
	}
	defer upg.Stop()

	// SIGHUP 触发零停机重启：fork+exec 新进程并继承监听 socket
	// SIGHUP triggers a zero-downtime restart: fork+exec inherits the listener
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP)
		for range sig {
			log.Printf("Received SIGHUP, starting hot restart")
			if err := upg.Upgrade(); err != nil {
				log.Printf("Hot restart failed: %v", err)
			}
		}
	}()

	// Listen 必须在 Ready 之前调用；热重启的子进程会继承同一个 listener
	// Listen must precede Ready; an upgraded child inherits the same listener
	ln, err := upg.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{Handler: handler}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	if err := upg.Ready(); err != nil {
		return fmt.Errorf("signal readiness: %w", err)
	}
	log.Printf("Server ready on %s", ln.Addr())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-upg.Exit():
		// 接替进程已接管监听，排空存量连接后退出
		// The replacement process holds the listener; drain and exit
		log.Printf("Handing over to new process, draining for up to %s", shutdownTimeout)
	case s := <-quit:
		log.Printf("Received %v, draining for up to %s", s, shutdownTimeout)
	case err := <-serveErr:
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}
