package main

import "log"

func main() {
	server, err := NewServer()
	if err != nil {
		log.Fatalf("не удалось создать сервер: %v", err)
	}

	if err := server.Run(""); err != nil {
		log.Fatalf("ошибка запуска сервера: %v", err)
	}
}
