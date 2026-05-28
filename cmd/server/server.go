package main

import (
	"RadioPump/internal/config"
	store "RadioPump/internal/db"
	"database/sql"
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

type Server struct {
	cfg     *config.Config
	storage *store.Storage
}

func NewServer() (*Server, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatal("Загрузка конфигурации не удалась: ")
	}
	db, err := sql.Open("sqlite", "./data/radio.db")
	if err != nil {
		log.Fatal("Подключение к базе данных не удалось: ", err)
		return nil, err
	}
	if err := db.Ping(); err != nil {
		log.Fatal("Пинг базы данных не удался: ", err)
		return nil, err
	}
	Storage := store.NewStorage(db)
	s := &Server{cfg: cfg, storage: Storage}
	s.setupRouter()
	return s, nil
}

func (s *Server) Run(addr string) error {
	return http.ListenAndServe(":8080", nil)
}
