package main

import (
	"fmt"
	"net"
	"sync"
	"time"
	"log"
	"strconv"
	"strings"

	irc "gopkg.in/sorcix/irc.v2"
	"math/rand"
)

type Server struct {
	created  int64
	clients  map[string]*Client
	channels map[string]*Channel

	*sync.RWMutex
}

func (s *Server) getAnonymousPrefix(i int) *irc.Prefix {
	prefix := anonymous
	if i > 1 {
		prefix.Name += fmt.Sprintf("%d", i)
	}
	return &prefix
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

func (s *Server) getClient(client string) *Client {
	if _, ok := s.clients[client]; ok {
		return s.clients[client]
	}

	return nil
}

func (s *Server) getClients(channel string) map[string]*Client {
	clients := make(map[string]*Client)
	if !s.channelExists(channel) {
		return clients
	}

	for clientname := range s.channels[channel].clients {
		cl := s.getClient(clientname)
		if cl != nil {
			clients[clientname] = cl
		}
	}

	return clients
}

func (s *Server) channelExists(channel string) bool {
	if _, ok := s.channels[channel]; ok {
		return true
	}

	return false
}

func (s *Server) inChannel(channel string, client string) bool {
	if s.channelExists(channel) {
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

	if !s.channelExists(channel) {
		s.channels[channel] = &Channel{time.Now().Unix(), make(map[string]int), make(map[string]string), "", 0, new(sync.RWMutex)}
	}
	s.channels[channel].Lock()
	s.channels[channel].clients[client] = len(s.channels[channel].clients) + 1
	s.channels[channel].Unlock()

	s.clients[client].writebuffer <- &irc.Message{nil, irc.JOIN, []string{channel}}

	s.sendNames(channel, client)
	s.updateUserCount(channel)
	s.sendTopic(channel, client, false)
}

func (s *Server) partChannel(channel string, client string) {
	if !s.inChannel(channel, client) {
		return // Not in channel
	}

	msgout := irc.Message{nil, irc.PART, []string{channel}}
	s.clients[client].writebuffer <- &msgout

	s.channels[channel].Lock()
	delete(s.channels[channel].clients, client)
	s.channels[channel].Unlock()

	s.updateUserCount(channel)
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
				msgout := irc.Message{s.getAnonymousPrefix(i), irc.JOIN, []string{channel}}
				s.clients[cclient].writebuffer <- &msgout
			}

			s.channels[channel].clients[cclient] = len(s.channels[channel].clients)
		} else if ccount > len(s.channels[channel].clients) {
			for i := ccount; i > len(s.channels[channel].clients); i-- {
				msgout := irc.Message{s.getAnonymousPrefix(i - 1), irc.PART, []string{channel}}
				s.clients[cclient].writebuffer <- &msgout
			}

			s.channels[channel].clients[cclient] = len(s.channels[channel].clients)
		}
	}
}

func (s *Server) sendNames(channel string, clientname string) {
	if s.inChannel(channel, clientname) {
		c := s.getClient(clientname)
		names := []string{}
		if c.capHostInNames {
			names = append(names, c.getPrefix().String())
		} else {
			names = append(names, c.nick)
		}

		for i := 1; i < len(s.channels[channel].clients); i++ {
			if c.capHostInNames {
				names = append(names, s.getAnonymousPrefix(i).String())
			} else {
				names = append(names, s.getAnonymousPrefix(i).Name)
			}
		}

		c.writebuffer <- &irc.Message{&anonirc, irc.RPL_NAMREPLY, []string{"=", channel, strings.Join(names, " ")}}
		c.writebuffer <- &irc.Message{&anonirc, irc.RPL_ENDOFNAMES, []string{channel, "End of /NAMES list."}}
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
			tprefix = anonirc
			tcommand = irc.RPL_TOPIC
		}
		msgout := irc.Message{&tprefix, tcommand, []string{channel, s.channels[channel].topic}}
		s.clients[client].writebuffer <- &msgout

		if !changed {
			msgout2 := irc.Message{&anonirc, strings.Join([]string{irc.RPL_TOPICWHOTIME, s.clients[client].nick, channel, "Anonymous", fmt.Sprintf("%d", s.channels[channel].topictime)}, " "), nil}
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
		s.channels[channel].Unlock()

		for sclient := range s.channels[channel].clients {
			s.sendTopic(channel, sclient, true)
		}
	} else {
		s.sendTopic(channel, client, false)
	}
}

func (s *Server) handleMode(c *Client, params []string) {
	if len(params) == 0 || params[0][0] != '#' {
		return // TODO: Send error
	}

	if len(params) > 1 && params[1][0] == '+' {
		s.channels[params[0]].Lock()
		s.channels[params[0]].addModes(params[1][1:])
		s.channels[params[0]].Unlock()
	}

	c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.RPL_CHANNELMODEIS, c.nick, params[0], s.channels[params[0]].printModes(nil)}, " "), []string{}}
	if len(params) == 1 {
		// Send channel creation time
		c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{"329", c.nick, params[0], fmt.Sprintf("%d", int32(s.channels[params[0]].created))}, " "), []string{}}
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
		if (msg.Command != irc.PING && msg.Command != irc.PONG) {
			fmt.Println(c.identifier, "<-", fmt.Sprintf("%s", msg))
		}
		if (msg.Command == irc.CAP && len(msg.Params) > 0 && msg.Params[0] == irc.CAP_LS) {
			c.writebuffer <- &irc.Message{&anonirc, irc.CAP, []string{msg.Params[0], "userhost-in-names"}}
		} else if (msg.Command == irc.CAP && len(msg.Params) > 0 && msg.Params[0] == irc.CAP_REQ) {
			if strings.Contains(msg.Trailing(), "userhost-in-names") {
				c.capHostInNames = true
			}
			c.writebuffer <- &irc.Message{&anonirc, irc.CAP, []string{irc.CAP_ACK, msg.Trailing()}}
		} else if (msg.Command == irc.CAP && len(msg.Params) > 0 && msg.Params[0] == irc.CAP_LIST) {
			caps := []string{}
			if c.capHostInNames {
				caps = append(caps, "userhost-in-names")
			}
			c.writebuffer <- &irc.Message{&anonirc, irc.CAP, []string{msg.Params[0], strings.Join(caps, " ")}}
		} else if (msg.Command == irc.PING) {
			c.writebuffer <- &irc.Message{&anonirc, irc.PONG, []string{msg.Params[0]}}
		} else if (msg.Command == irc.NICK && c.nick == "*" && msg.Params[0] != "" && msg.Params[0] != "*") {
			c.nick = strings.Trim(msg.Params[0], "\"")
		} else if (msg.Command == irc.USER && c.user == "" && len(msg.Params) >= 3 && msg.Params[0] != "" && msg.Params[2] != "") {
			c.user = strings.Trim(msg.Params[0], "\"")
			c.host = strings.Trim(msg.Params[2], "\"")

			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_WELCOME, []string{"Welcome to AnonIRC " + c.getPrefix().String()}}
			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_YOURHOST, []string{"Your host is AnonIRC, running version AnonIRCd"}}
			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_CREATED, []string{fmt.Sprintf("This server was created %s", time.Unix(s.created, 0).UTC())}}
			c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.RPL_MYINFO, c.nick, "AnonIRC AnonIRCd ", umodes, cmodes, cmodesarg}, " "), []string{}}

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
				c.writebuffer <- &irc.Message{&anonirc, motdcode, []string{"  " + motdmsg}}
			}

			s.joinChannel("#lobby", c.identifier)
		} else if (msg.Command == irc.JOIN && msg.Params[0][0] == '#') {
			s.joinChannel(msg.Params[0], c.identifier)
		} else if (msg.Command == irc.NAMES && msg.Params[0][0] == '#') {
			s.sendNames(msg.Params[0], c.identifier)
		} else if (msg.Command == irc.WHO && msg.Params[0][0] == '#') {
			if s.inChannel(msg.Params[0], c.identifier) {
				i := 0
				for _, cl := range s.getClients(msg.Params[0]) {
					var prfx *irc.Prefix
					if cl.identifier == c.identifier {
						prfx = c.getPrefix()
					} else {
						i++
						prfx = s.getAnonymousPrefix(i)
					}

					c.writebuffer <- &irc.Message{&anonirc, irc.RPL_WHOREPLY, []string{msg.Params[0], prfx.User, prfx.Host, "AnonIRC", prfx.Name, "H", "0 Anonymous"}}
				}
				c.writebuffer <- &irc.Message{&anonirc, irc.RPL_ENDOFWHO, []string{msg.Params[0], "End of /WHO list."}}
			}
		} else if (msg.Command == irc.MODE && msg.Params[0][0] == '#') {
			s.handleMode(c, msg.Params)
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

		addnick := false
		if _, err := strconv.Atoi(msg.Command); err == nil {
			addnick = true
		} else if msg.Command == irc.CAP {
			addnick = true
		}

		if addnick {
			msg.Params = append([]string{c.nick}, msg.Params...)
		}

		if (msg.Command != irc.PING && msg.Command != irc.PONG) {
			fmt.Println(c.identifier, "->", msg)
		}
		c.writer.Encode(msg)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	client := Client{randomIdentifier(), "*", "", "", conn, []string{}, make(chan *irc.Message), irc.NewDecoder(conn), irc.NewEncoder(conn), false, new(sync.RWMutex)}
	defer conn.Close()

	s.Lock()
	s.clients[client.identifier] = &client
	s.Unlock()

	go s.handleWrite(&client)
	s.handleRead(&client)
}
func (s *Server) pingClients() {
	for _, c := range s.clients {
		ping := fmt.Sprintf("anonirc%d%d", int32(time.Now().Unix()), rand.Intn(1000))
		c.pings = append(c.pings, ping)
		c.writebuffer <- &irc.Message{&anonirc, irc.PING, []string{ping}}
	}
	time.Sleep(15 * time.Second)
}

func (s *Server) listen() {
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
