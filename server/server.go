package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type ChatServer struct {
	subscribers     map[chan []byte]struct{}
	subscriberMutex *sync.Mutex
	mux             http.ServeMux

	outgoingMsgChan chan<- []byte
}

func New(incomingMsgChan <-chan []byte, outgoingMsgChan chan<- []byte) *ChatServer {
	server := &ChatServer{
		subscribers:     make(map[chan []byte]struct{}),
		subscriberMutex: &sync.Mutex{},
		outgoingMsgChan: outgoingMsgChan,
	}

	server.mux.HandleFunc("/subscribe", server.subscribeHandler)

	go func() {
		for msg := range incomingMsgChan {
			server.publish(msg)
		}
	}()

	return server
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
			log.Printf("skipping slow client")
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

			s.outgoingMsgChan <- msg
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
