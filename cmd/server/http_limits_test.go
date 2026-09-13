package main

import (
	"RadioPump/internal/api/handlers"
	"RadioPump/internal/media"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// pipeListener передаёт настоящему net/http соединения без буфера ОС, чтобы
// клиент, не читающий ответ, гарантированно блокировал запись даже малой обложки.
type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }

// Тестовый каталог возвращает синтетические байты без личной библиотеки и БД.
type deadlineCover []byte

func (c deadlineCover) GetCover(uint) ([]byte, error) { return []byte(c), nil }

// Реальные файловый handler и handler обложек должны вернуться при блокировке
// записи, освобождая свои локальные ресурсы без отмены со стороны клиента.
func TestOrdinaryResponsesStopBlockedWrites(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("synthetic"), 16384)
	if err := os.WriteFile(filepath.Join(dir, "tone.wav"), data, 0600); err != nil {
		t.Fatal(err)
	}
	files, err := media.NewTrackFileStorage(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Handle("/music/*", http.StripPrefix("/music/", files))
	router.Get("/api/covers/{id}", handlers.CoverHandler(deadlineCover(data)))
	router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(dir))))
	for _, path := range []string{"/music/tone.wav", "/api/covers/1", "/static/tone.wav", "/missing"} {
		t.Run(path, func(t *testing.T) {
			finished := make(chan struct{})
			closed := make(chan struct{})
			srv := newHTTPServer("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(finished)
				router.ServeHTTP(w, r)
			}))
			srv.WriteTimeout = 50 * time.Millisecond
			srv.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					close(closed)
				}
			}
			listener := newPipeListener()
			go srv.Serve(listener)
			defer srv.Close()
			serverConn, client := net.Pipe()
			defer client.Close()
			listener.connections <- serverConn
			client.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := fmt.Fprintf(client, "GET %s HTTP/1.1\r\nHost: test\r\n\r\n", path); err != nil {
				t.Fatal(err)
			}
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("handler retained resources")
			}
			// Малые ответы могут остаться в буфере net/http после handler: проверяем
			// также финальный flush и закрытие соединения самим сервером.
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("connection retained after write timeout")
			}
		})
	}
	// Range и повторные обычные запросы остаются работоспособными с теми же лимитами.
	srv := httptest.NewUnstartedServer(router)
	srv.Config = newHTTPServer("", router)
	srv.Start()
	defer srv.Close()
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", srv.URL+"/music/tone.wav", nil)
		req.Header.Set("Range", "bytes=10-19")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 206 || !bytes.Equal(body, data[10:20]) {
			t.Fatalf("range: %d %q %v", resp.StatusCode, body, err)
		}
	}
}

// Переполнение закрывает лишний сокет; повторный Close не освобождает чужой
// слот, а после освобождения новый клиент принимается и Shutdown не зависает.
func TestConnectionLimitRejectsAndReleases(t *testing.T) {
	base := newPipeListener()
	listener := limitConnections(base, 1)
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	accept := func() { conn, _ := listener.Accept(); accepted <- conn }
	go accept()
	a, clientA := net.Pipe()
	defer clientA.Close()
	base.connections <- a
	first := <-accepted
	go accept()
	b, clientB := net.Pipe()
	defer clientB.Close()
	base.connections <- b
	clientB.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := clientB.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("excess connection: %v", err)
	}
	first.Close()
	first.Close()
	c, clientC := net.Pipe()
	defer clientC.Close()
	base.connections <- c
	select {
	case conn := <-accepted:
		conn.Close()
	case <-time.After(time.Second):
		t.Fatal("slot was not released")
	}
	go accept()
	listener.Close()
	select {
	case conn := <-accepted:
		if conn != nil {
			t.Fatal("accepted after close")
		}
	case <-time.After(time.Second):
		t.Fatal("Accept blocked shutdown")
	}
}
