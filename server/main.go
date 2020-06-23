package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"
)

const (
	defaultListenAddr = "0.0.0.0:8080"
	natsSubject       = "k8schat/messages"
)

func main() {
	natsUrl := os.Getenv("NATS_URL")
	listenAddr := os.Getenv("K8SCHAT_LISTEN")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	server, err := New(natsUrl, natsSubject)
	defer server.Close()
	if err != nil {
		log.Printf("failed to start chat server: %v", err)
		return
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Printf("failed to start listener: %v", err)
		return
	}

	log.Printf("listening on %v", listenAddr)
	httpServer := &http.Server{
		Handler:      server,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- httpServer.Serve(listener)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	select {
	case err := <-errChan:
		log.Printf("failed to start server: %v", err)
	case <-signals:
		log.Print("caught interrupt signal")
	}

	httpServer.Shutdown(context.Background())
}
