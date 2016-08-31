package main

import (
	"net"
	"log"
	"sync"
	"math/rand"
	irc "gopkg.in/sorcix/irc.v2"
	"fmt"
	"time"
	"strings"
)

type Channel struct {
	clients Client
}

type Client struct {
	identifier  string
	conn        net.Conn
	pings       []string

	writebuffer chan *irc.Message

	reader      *irc.Decoder
	writer      *irc.Encoder

	*sync.RWMutex
}

type Server struct {
	sync.Mutex
	channels map[string]Channel
	clients  map[string]*Client
}

var anonymous = irc.Prefix{"Anonymous", "", "AnonIRC"}

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const motd = `
  _|_|                                  _|_|_|  _|_|_|      _|_|_|
_|    _|  _|_|_|      _|_|    _|_|_|      _|    _|    _|  _|
_|_|_|_|  _|    _|  _|    _|  _|    _|    _|    _|_|_|    _|
_|    _|  _|    _|  _|    _|  _|    _|    _|    _|    _|  _|
_|    _|  _|    _|    _|_|    _|    _|  _|_|_|  _|    _|    _|_|_|
`

func randomIdentifier() string {
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func (s *Server) handleRead(c *Client) {
	for {
		c.conn.SetDeadline(time.Now().Add(300 * time.Second))
		msg, err := s.clients[c.identifier].reader.Decode()
		if err != nil {
			fmt.Println("Unable to read from client:", err)
			return
		}
		fmt.Println(fmt.Sprintf("%v", msg))

		fmt.Println("PREFIX: " + fmt.Sprintf("%v", msg.Prefix))
		fmt.Println("COMMAND: " + fmt.Sprintf("%v", msg.Command))
		fmt.Println("PARAMS: " + fmt.Sprintf("%v", msg.Params))
		if (msg.Command == irc.CAP && len(msg.Params) > 0 && (msg.Params[0] == irc.CAP_LS || msg.Params[0] == irc.CAP_LIST)) {
			response := irc.Message{nil, irc.CAP, []string{"*", msg.Params[0], ""}}
			c.writebuffer <- &response
		} else if (msg.Command == irc.PING) {
			c.writebuffer <- &irc.Message{nil, irc.PONG, []string{msg.Params[0]}}
		}

		if (msg.Command == irc.PRIVMSG) {
			for _, sclient := range s.clients {
				msgout := irc.Message{&anonymous, irc.PRIVMSG, []string{"#test", msg.Trailing()}}
				sclient.writebuffer <- &msgout
			}
		}
	}
}

func (s *Server) handleWrite(c *Client) {
	for msg := range c.writebuffer {
		fmt.Println("Writing...", msg)
		c.writer.Encode(msg)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	client := Client{randomIdentifier(), conn, []string{}, make(chan *irc.Message), irc.NewDecoder(conn), irc.NewEncoder(conn), new(sync.RWMutex)}
	defer conn.Close()

	s.Lock()
	s.clients[client.identifier] = &client
	s.Unlock()

	go s.handleWrite(&client)

	client.writebuffer <- &irc.Message{&irc.Prefix{Name:"anonircd"}, irc.RPL_WELCOME, []string{"                              Welcome to"}}
	motdsplit := strings.Split(motd, "\n")
	for i, motdmsg := range motdsplit {
		var motdcode string
		if (i == 0) {
			motdcode = irc.RPL_MOTDSTART
		} else if (i < len(motdsplit) - 1) {
			motdcode = irc.RPL_MOTD
		} else {
			motdcode = irc.RPL_ENDOFMOTD
		}
		client.writebuffer <- &irc.Message{&irc.Prefix{Name:"anonircd"}, motdcode, []string{"  " + motdmsg}}
	}

	client.writebuffer <- &irc.Message{&anonymous, irc.JOIN, []string{"#lobby"}}

	s.handleRead(&client)
}
func (s *Server) pingClients() {
	for _, c := range s.clients {
		ping := fmt.Sprintf("anonircd%d%d", int32(time.Now().Unix()), rand.Intn(1000))
		c.pings = append(c.pings, ping)
		c.writebuffer <- &irc.Message{&irc.Prefix{Name: "anonircd"}, irc.PING, []string{ping}}
	}
	time.Sleep(15 * time.Second)
}

func (s *Server) listen(server *Server) {
	rand.Seed(time.Now().UTC().UnixNano())

	listener, err := net.Listen("tcp", ":6667")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	go s.pingClients()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: %s", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func main() {
	server := Server{
		clients:  make(map[string]*Client),
		channels: make(map[string]Channel)}
	server.listen(&server)
}