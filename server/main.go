package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	defaultListenAddr = "0.0.0.0:8080"
	natsSubject       = "k8schat/messages"
)

func main() {
	natsUrl := os.Getenv("NATS_URL")
	if natsUrl == "" {
		natsUrl = nats.DefaultURL
	}

	nc, err := nats.Connect(natsUrl)
	if err != nil {
		log.Printf("failed to connect to nats: %v", err)
		return
	}

	defer nc.Drain()
	inChan := make(chan []byte, 100)
	outChan := make(chan []byte, 100)
	defer close(inChan)
	defer close(outChan)

	go func() {
		for msg := range outChan {
			nc.Publish(natsSubject, msg)
		}
	}()

	nc.Subscribe(natsSubject, func(m *nats.Msg) {
		select {
		case inChan <- m.Data:
		default:
			log.Print("throttled message")
		}
	})

	listenAddr := os.Getenv("K8SCHAT_LISTEN")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Printf("failed to start listener: %v", err)
		return
	}

	log.Printf("listening on %v", listenAddr)
	server := New(inChan, outChan)
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
		log.Printf("caught interrupt signal")
	}

	httpServer.Shutdown(context.Background())
}
