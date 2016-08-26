package main

import (
	"net"
	"log"
	"sync"
	"math/rand"
	irc "gopkg.in/sorcix/irc.v2"
	"fmt"
	"time"
)

const (
	MSG_RAW = 0
	MSG_COMMAND = 1
	MSG_PING = 2
)

type Channel struct {
	clients Client
}

type Client struct {
	identifier  string
	conn        net.Conn

	writebuffer chan *irc.Message

	reader      *irc.Decoder
	writer      *irc.Encoder
}
/*
func (c *Client) Send(message Message) {
	c.conn.wr <- message
}*/

type Server struct {
	sync.Mutex
	channels map[string]Channel
	clients  map[string]*Client
}

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randomIdentifier() string {
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func handleRead(c *Client, server *Server) {
	for {
		c.conn.SetDeadline(time.Now().Add(300 * time.Second))
		msg, err := c.reader.Decode()
		if err != nil {
			//return c.Reconnect()
		}
		fmt.Println("%#v", msg)

		fmt.Println("PREFIX: " + fmt.Sprintf("%v", msg.Prefix))
		fmt.Println("COMMAND: " + fmt.Sprintf("%v", msg.Command))
		fmt.Println("PARAMS: " + fmt.Sprintf("%v", msg.Params))
		if (msg.Command == irc.CAP && len(msg.Params) > 0 && (msg.Params[0] == irc.CAP_LS || msg.Params[0] == irc.CAP_LIST)) {
			fmt.Println("WAS CAP")
			response := irc.Message{nil, irc.CAP, []string{"*", msg.Params[0], ""}}
			c.writebuffer <- &response
		}

		prfx := irc.Prefix{Name: "tee"}
		for _, sclient := range server.clients {
			msgout := irc.Message{&prfx, irc.PRIVMSG, []string{"#test", msg.Trailing()}}
			sclient.writebuffer <- &msgout
		}
	}
}

func handleWrite(c *Client, server *Server) {
	for msg := range c.writebuffer {
		c.writer.Encode(msg)
	}
}

func handleConnection(conn net.Conn, server *Server) {
	messages := make(chan *irc.Message)
	client := Client{randomIdentifier(), conn, messages, irc.NewDecoder(conn), irc.NewEncoder(conn)}

	server.Lock()
	server.clients[client.identifier] = &client
	server.Unlock()

	defer conn.Close()
	go handleRead(&client, server)
	handleWrite(&client, server)
}

func listen(server *Server) {
	ln, err := net.Listen("tcp", ":6667")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn, server)
	}
}

func main() {
	server := Server{
		clients:  make(map[string]*Client),
		channels: make(map[string]Channel)}
	listen(&server)
}