package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const maxHTTPConnections = 1024

// newHTTPServer ограничивает чтение, простой и запись всех ответов, включая
// файлы, обложки и ошибки router. 120 секунд включают до 60 секунд чтения upload.
// Только успешная подписка эфира заменяет общий deadline своим скользящим.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// connectionLimit ограничивает все принятые соединения, в том числе ещё не
// приславшие заголовки и keep-alive. Лишний сокет закрывается до запуска HTTP
// goroutine: ответ 503 сам мог бы заблокироваться на медленном получателе.
type connectionLimit struct {
	net.Listener
	slots chan struct{}
}

func limitConnections(listener net.Listener, count int) net.Listener {
	return &connectionLimit{Listener: listener, slots: make(chan struct{}, count)}
}

// Accept возвращает только соединение с зарезервированным слотом; закрытие
// исходного listener прерывает ожидание Accept и сохраняет штатный Shutdown.
func (l *connectionLimit) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			return &limitedConnection{Conn: conn, release: func() { <-l.slots }}, nil
		default:
			_ = conn.Close()
		}
	}
}

// limitedConnection возвращает слот ровно один раз, даже если Shutdown и
// обработчик одновременно закрывают соединение после ошибки записи.
type limitedConnection struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConnection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
