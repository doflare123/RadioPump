package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

// libraryLock использует уже имеющийся SQLite для межпроцессного владения
// music.dir. Долгая EXCLUSIVE-транзакция живёт в отдельном lock-файле и не
// блокирует radio.db. ОС освобождает её и после аварийного завершения процесса.
// Без этого второй сервер мог бы принять активный upload первого за orphan.
type libraryLock struct {
	db   *sql.DB
	conn *sql.Conn
}

func acquireLibraryLock(dir string) (*libraryLock, error) {
	db, err := sql.Open("sqlite", filepath.Join(dir, ".radiopump-lock.db"))
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		_ = conn.Close()
		_ = db.Close()
		return nil, fmt.Errorf("музыкальная библиотека уже занята другим сервером или недоступна: %w", err)
	}
	return &libraryLock{db: db, conn: conn}, nil
}

// Close вызывается после остановки HTTP, фоновых задач и закрытия radio.db.
func (l *libraryLock) Close() error {
	_, err := l.conn.ExecContext(context.Background(), "ROLLBACK")
	return errors.Join(err, l.conn.Close(), l.db.Close())
}
