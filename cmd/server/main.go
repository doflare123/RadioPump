package main

import (
	"log"
)

func main() {
	server, err := NewServer()
	if err != nil {
		log.Fatal("Ошибка при создании сервера: ", err)
		return
	}
	if err := server.Run(":8080"); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
