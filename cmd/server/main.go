package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	server, err := NewServer()
	if err != nil {
		log.Fatalf("не удалось создать сервер: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.RunContext(ctx, ""); err != nil {
		log.Fatalf("ошибка запуска сервера: %v", err)
	}
}
