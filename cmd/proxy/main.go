package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/user/distributed-caching-go/internal/proxy"
)

func main() {
	port := flag.Int("port", 8080, "HTTP port to listen on")
	upstream := flag.String("upstream", "http://api:8081", "upstream API layer base URL")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p := proxy.NewProxy(*upstream)
	log.Printf("proxy: listening on :%d, upstream=%s", *port, *upstream)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: p.Handler(),
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy: server error: %v", err)
	}
}
