package main

import (
	"fmt"
	"golang.org/x/net/websocket"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"time"
)

type Pub struct {
	Key     string
	Message []byte
}

type Sub struct {
	Key      string
	Messages chan []byte
}

type Brolly struct {
	pubs    chan Pub
	subs    chan *Sub
	rubs    chan *Sub
	cleanup chan struct{}
	done    chan struct{}
}

func NewBrolly() *Brolly {
	return &Brolly{
		pubs:    make(chan Pub),
		subs:    make(chan *Sub),
		rubs:    make(chan *Sub),
		cleanup: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (b *Brolly) Run(addr string) error {
	sock, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Listening on %s\n", sock.Addr())

	mux := http.NewServeMux()
	mux.Handle("/broadcast/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()
		if req.Method != "POST" {
			http.Error(w, "Must use POST to publish data", http.StatusMethodNotAllowed)
			return
		}
		payload, err := ioutil.ReadAll(req.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Could not read request body: %s", err), http.StatusBadRequest)
			return
		}
		b.pubs <- Pub{
			Key:     req.URL.Path[len("/broadcast"):],
			Message: payload,
		}
	}))
	mux.Handle("/listen/", websocket.Handler(func(conn *websocket.Conn) {
		sub := &Sub{
			Key:      conn.Request().URL.Path[len("/listen"):],
			Messages: make(chan []byte),
		}
		b.subs <- sub
		for msg := range sub.Messages {
			_, err := conn.Write(msg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Dropping subscription: %s\n", err)
				b.rubs <- sub
				return
			}
		}
	}))

	go func() {
		// since our mutable state is only visible to this
		// goroutine there's no need for locking
		subs := map[string]map[*Sub]bool{}
		for {
			select {
			case pub := <-b.pubs:
				fmt.Fprintf(os.Stderr, "Got message on %s\n", pub.Key)
				ksubs := subs[pub.Key]
				fmt.Fprintf(os.Stderr, "Sending to %d subscribers\n", len(ksubs))
				for sub := range ksubs {
					// there's a goroutine waiting behind each of
					// these channels so this loop won't be held
					// up by slow clients
					sub.Messages <- pub.Message
				}
			case sub := <-b.subs:
				ksubs, ok := subs[sub.Key]
				if !ok {
					ksubs = map[*Sub]bool{}
					subs[sub.Key] = ksubs
				}
				ksubs[sub] = true
				fmt.Fprintf(os.Stderr, "Got subscriber for %s\n", sub.Key)
			case sub := <-b.rubs:
				fmt.Fprintf(os.Stderr, "Removing subscriber for %s\n", sub.Key)
				close(sub.Messages)
				delete(subs[sub.Key], sub)
			case <-b.cleanup:
				fmt.Fprintf(os.Stderr, "Running cleanup\n")
				// By "sending" zero bytes we're testing the liveness
				// of the connection; any that fail will be cleaned up
				// by the websocket goroutine
				for _, ksubs := range subs {
					for sub := range ksubs {
						sub.Messages <- []byte{}
					}
				}
			}
		}
		close(b.done)
	}()

	go func() {
		for {
			select {
			case <-b.done:
				return
			default:
				time.Sleep(time.Second)
				b.cleanup <- struct{}{}
			}
		}
	}()

	fmt.Fprintf(os.Stderr, "Serving HTTP\n")

	return http.Serve(sock, mux)
}

func main() {
	brolly := NewBrolly()
	err := brolly.Run(":14902")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
}
