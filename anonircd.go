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
	"strconv"
)

type Channel struct {
	clients   map[string]int
	topic     string
	topictime int64

	*sync.RWMutex
}

type Client struct {
	identifier  string
	nick        string
	user        string

	conn        net.Conn
	pings       []string
	writebuffer chan *irc.Message

	reader      *irc.Decoder
	writer      *irc.Encoder

	*sync.RWMutex
}

type Server struct {
	clients  map[string]*Client
	channels map[string]*Channel

	*sync.RWMutex
}

var anonymous = irc.Prefix{"Anonymous", "", "AnonIRC"}
var anonircd = irc.Prefix{Name:"AnonIRCd"}

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

func (c *Client) getPrefix() *irc.Prefix {
	return &irc.Prefix{Name:c.nick, User:c.user, Host:"AnonIRC"}
}

func (s *Server) getChannels(client string) map[string]*Channel {
	channels := make(map[string]*Channel)
	for channelname, channel := range s.channels {
		if s.inChannel(channelname, client) {
			channels[channelname] = channel
		}
	}
	return channels
}

func (s *Server) inChannel(channel string, client string) bool {
	if _, ok := s.channels[channel]; ok {
		if _, ok := s.channels[channel].clients[client]; ok {
			return true
		}
	}

	return false
}

func (s *Server) joinChannel(channel string, client string) {
	if s.inChannel(channel, client) {
		return // Already in channel
	}

	if _, ok := s.channels[channel]; !ok {
		s.channels[channel] = &Channel{make(map[string]int), "", 0, new(sync.RWMutex)}
	}
	s.channels[channel].Lock()
	s.channels[channel].clients[client] = 1

	msgout := irc.Message{nil, irc.JOIN, []string{channel}}
	s.clients[client].writebuffer <- &msgout

	s.updateUserCount(channel)
	s.sendTopic(channel, client, false)
	s.channels[channel].Unlock()
}

func (s *Server) partChannel(channel string, client string) {
	if !s.inChannel(channel, client) {
		return // Not in channel
	}

	msgout := irc.Message{nil, irc.PART, []string{channel}}
	s.clients[client].writebuffer <- &msgout

	s.channels[channel].Lock()
	delete(s.channels[channel].clients, client)
	s.updateUserCount(channel)
	s.channels[channel].Unlock()
}

func (s *Server) partAllChannels(client string) {
	for channelname := range s.getChannels(client) {
		s.partChannel(channelname, client)
	}
}

func (s *Server) updateUserCount(channel string) {
	for cclient, ccount := range s.channels[channel].clients {
		if ccount < len(s.channels[channel].clients) {
			for i := ccount; i < len(s.channels[channel].clients); i++ {
				prefix := anonymous
				if i > 1 {
					prefix.Name += fmt.Sprintf("%d", i)
				}
				msgout := irc.Message{&prefix, irc.JOIN, []string{channel}}
				s.clients[cclient].writebuffer <- &msgout
			}

			s.channels[channel].clients[cclient] = len(s.channels[channel].clients)
		} else if ccount > len(s.channels[channel].clients) {
			for i := ccount; i > len(s.channels[channel].clients); i-- {
				prefix := anonymous
				if i > 1 {
					prefix.Name += fmt.Sprintf("%d", i)
				}
				msgout := irc.Message{&prefix, irc.PART, []string{channel}}
				s.clients[cclient].writebuffer <- &msgout
			}

			s.channels[channel].clients[cclient] = len(s.channels[channel].clients)
		}
	}
}

func (s *Server) sendTopic(channel string, client string, changed bool) {
	if !s.inChannel(channel, client) {
		return // Not in channel  TODO: Send error instead
	}

	if s.channels[channel].topic != "" {
		tprefix := anonymous
		tcommand := irc.TOPIC
		if !changed {
			tprefix = anonircd
			tcommand = irc.RPL_TOPIC
		}
		msgout := irc.Message{&tprefix, tcommand, []string{channel, s.channels[channel].topic}}
		s.clients[client].writebuffer <- &msgout

		if !changed {
			msgout2 := irc.Message{&anonircd, strings.Join([]string{irc.RPL_TOPICWHOTIME, s.clients[client].nick, channel, "Anonymous", fmt.Sprintf("%d", s.channels[channel].topictime)}, " "), nil}
			s.clients[client].writebuffer <- &msgout2
		}
	}
}

func (s *Server) handleTopic(channel string, client string, topic string) {
	if !s.inChannel(channel, client) {
		return // Not in channel TODO: Send error
	}

	if topic != "" {
		s.channels[channel].Lock()
		s.channels[channel].topic = topic
		s.channels[channel].topictime = time.Now().Unix()

		for sclient := range s.channels[channel].clients {
			s.sendTopic(channel, sclient, true)
		}
		s.channels[channel].Unlock()
	} else {
		s.sendTopic(channel, client, false)
	}
}

func (s *Server) msgChannel(channel string, client string, message string) {
	if !s.inChannel(channel, client) {
		return // Not in channel  TODO: Send error message
	}

	for sclient := range s.channels[channel].clients {
		if s.clients[sclient].identifier != client {
			msgout := irc.Message{&anonymous, irc.PRIVMSG, []string{channel, message}}
			s.clients[sclient].writebuffer <- &msgout
		}
	}
}

func (s *Server) handleRead(c *Client) {
	for {
		c.conn.SetDeadline(time.Now().Add(300 * time.Second))
		msg, err := s.clients[c.identifier].reader.Decode()
		if err != nil {
			fmt.Println("Unable to read from client:", err)
			s.partAllChannels(c.identifier)
			return
		}
		fmt.Println("Read: " + fmt.Sprintf("%v", msg))
		if (msg.Command == irc.CAP && len(msg.Params) > 0 && (msg.Params[0] == irc.CAP_LS || msg.Params[0] == irc.CAP_LIST)) {
			response := irc.Message{&anonircd, irc.CAP, []string{msg.Params[0], ""}}
			c.writebuffer <- &response
		} else if (msg.Command == irc.PING) {
			c.writebuffer <- &irc.Message{&anonircd, irc.PONG, []string{msg.Params[0]}}
		} else if (msg.Command == irc.NICK && c.nick == "*" && msg.Params[0] != "" && msg.Params[0] != "*") {
			c.nick = msg.Params[0]
		} else if (msg.Command == irc.USER && c.user == "" && msg.Params[0] != "") {
			c.user = msg.Params[0]

			c.writebuffer <- &irc.Message{&anonircd, irc.RPL_WELCOME, []string{"Welcome to AnonIRC."}}
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
				c.writebuffer <- &irc.Message{&anonircd, motdcode, []string{"  " + motdmsg}}
			}

			s.joinChannel("#lobby", c.identifier)
		} else if (msg.Command == irc.JOIN && msg.Params[0][0] == '#') {
			s.joinChannel(msg.Params[0], c.identifier)
		} else if (msg.Command == irc.TOPIC) {
			s.handleTopic(msg.Params[0], c.identifier, msg.Trailing())
		} else if (msg.Command == irc.PRIVMSG) {
			s.msgChannel(msg.Params[0], c.identifier, msg.Trailing())
		} else if (msg.Command == irc.PART && msg.Params[0][0] == '#') {
			s.partChannel(msg.Params[0], c.identifier)
		} else if (msg.Command == irc.QUIT) {
			s.partAllChannels(c.identifier)
		}
	}
}

func (s *Server) handleWrite(c *Client) {
	for msg := range c.writebuffer {
		if msg.Prefix == nil && c.nick != "" {
			msg.Prefix = c.getPrefix()
		}
		if _, err := strconv.Atoi(msg.Command); err == nil {
			msg.Params = append([]string{c.nick}, msg.Params...)
		}
		fmt.Println("Writing...", msg)
		c.writer.Encode(msg)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	client := Client{randomIdentifier(), "*", "", conn, []string{}, make(chan *irc.Message), irc.NewDecoder(conn), irc.NewEncoder(conn), new(sync.RWMutex)}
	defer conn.Close()

	s.Lock()
	s.clients[client.identifier] = &client
	s.Unlock()

	go s.handleWrite(&client)
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

func (s *Server) listen() {
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
			fmt.Println("Error accepting connection:", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func main() {
	server := Server{make(map[string]*Client), make(map[string]*Channel), new(sync.RWMutex)}
	server.listen()
}
