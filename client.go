package main

import (
	"fmt"
	"golang.org/x/net/websocket"
	"io"
	"os"
)

func run() error {
	conn, err := websocket.Dial("ws://localhost:14902/listen/test", "", "http://localhost")
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Fprintf(os.Stderr, "Connected to %s\n", conn.RemoteAddr())

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "MSG: %s\n", string(buf[:n]))
	}
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
}
