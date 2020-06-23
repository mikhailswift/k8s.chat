package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"nhooyr.io/websocket"
)

type ChatServer struct {
	subscribers     map[chan []byte]struct{}
	subscriberMutex *sync.Mutex

	mux http.ServeMux

	natsConn   *nats.Conn
	msgOutChan chan []byte
}

func New(natsUrl, natsSubject string) (*ChatServer, error) {
	if natsUrl == "" {
		natsUrl = nats.DefaultURL
	}

	nc, err := nats.Connect(natsUrl)
	if err != nil {
		return nil, err
	}

	server := &ChatServer{
		subscribers:     make(map[chan []byte]struct{}),
		subscriberMutex: &sync.Mutex{},
		natsConn:        nc,
		msgOutChan:      make(chan []byte, 100),
	}

	server.natsConn.Subscribe(natsSubject, func(m *nats.Msg) {
		server.publish(m.Data)
	})

	go func() {
		for msg := range server.msgOutChan {
			server.natsConn.Publish(natsSubject, msg)
		}
	}()

	server.mux.HandleFunc("/subscribe", server.subscribeHandler)
	return server, nil
}

func (s *ChatServer) Close() error {
	close(s.msgOutChan)
	return s.natsConn.Drain()
}

func (s *ChatServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *ChatServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("error handling websocket: %v", err)
		return
	}

	log.Print("client subscribed")
	err = s.subscribe(r.Context(), conn)
	log.Printf("client left %v", err)
	conn.Close(websocket.StatusInternalError, "")
}

func (s *ChatServer) publish(msg []byte) {
	s.subscriberMutex.Lock()
	for sub := range s.subscribers {
		select {
		case sub <- msg:
		default:
			log.Print("skipping slow client")
		}
	}
	s.subscriberMutex.Unlock()
}

func (s *ChatServer) subscribe(ctx context.Context, conn *websocket.Conn) error {
	sub := make(chan []byte, 1)
	s.subscriberMutex.Lock()
	s.subscribers[sub] = struct{}{}
	s.subscriberMutex.Unlock()
	defer func() {
		s.subscriberMutex.Lock()
		delete(s.subscribers, sub)
		s.subscriberMutex.Unlock()
	}()

	closeChan := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				closeChan <- err
				return
			}

			select {
			case s.msgOutChan <- msg:
			default:
				log.Print("received message throttled")
			}
		}
	}()

	for {
		select {
		case msg := <-sub:
			if err := writeTimeout(ctx, 5*time.Second, conn, msg); err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		case err := <-closeChan:
			return err
		}
	}
}

func writeTimeout(ctx context.Context, timeout time.Duration, conn *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, msg)
}
