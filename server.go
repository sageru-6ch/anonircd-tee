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
	"crypto/tls"
	"reflect"
)

type Server struct {
	config   *Config
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
		s.channels[channel] = &Channel{Entity{ENTITY_CHANNEL, time.Now().Unix(), make(map[string]string), new(sync.RWMutex)}, make(map[string]int), "", 0}
	} else if s.channels[channel].hasMode("z") && !s.clients[client].ssl {
		s.clients[client].sendNotice("Unable to join " + channel + ": SSL connections only (channel mode +z)")
		return
	}
	s.channels[channel].Lock()
	var ccount int
	if s.clients[client].hasMode("c") || s.channels[channel].hasMode("c") {
		ccount = 2
	} else {
		ccount = len(s.channels[channel].clients) + 1
	}
	s.channels[channel].clients[client] = ccount
	s.channels[channel].Unlock()

	s.clients[client].writebuffer <- &irc.Message{s.clients[client].getPrefix(), irc.JOIN, []string{channel}}

	s.sendNames(channel, client)
	s.updateUserCount(channel)
	s.sendTopic(channel, client, false)
}

func (s *Server) partChannel(channel string, client string, reason string) {
	if !s.inChannel(channel, client) {
		return
	}

	s.clients[client].writebuffer <- &irc.Message{s.clients[client].getPrefix(), irc.PART, []string{channel, reason}}

	s.channels[channel].Lock()
	delete(s.channels[channel].clients, client)
	s.channels[channel].Unlock()

	s.updateUserCount(channel)
}

func (s *Server) partAllChannels(client string) {
	for channelname := range s.getChannels(client) {
		s.partChannel(channelname, client, "")
	}
}

func (s *Server) enforceModes(channel string) {
	if s.channels[channel].hasMode("z") {
		for client := range s.channels[channel].clients {
			if !s.clients[client].ssl {
				s.partChannel(channel, client, "Only SSL connections are allowed in this channel")
			}
		}
	}
}

func (s *Server) updateUserCount(channel string) {
	for cclient, ccount := range s.channels[channel].clients {
		var chancount int
		if s.clients[cclient].hasMode("c") || s.channels[channel].hasMode("c") {
			chancount = 2 // Hide user count
		} else {
			chancount = len(s.channels[channel].clients)
		}

		if ccount < chancount {
			s.channels[channel].Lock()
			for i := ccount; i < chancount; i++ {
				s.clients[cclient].writebuffer <- &irc.Message{s.getAnonymousPrefix(i), irc.JOIN, []string{channel}}
			}

			s.channels[channel].clients[cclient] = chancount
			s.channels[channel].Unlock()
		} else if ccount > chancount {
			s.channels[channel].Lock()
			for i := ccount; i > chancount; i-- {
				s.clients[cclient].writebuffer <- &irc.Message{s.getAnonymousPrefix(i - 1), irc.PART, []string{channel}}
			}

			s.channels[channel].clients[cclient] = chancount
			s.channels[channel].Unlock()
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

		var ccount int
		if s.clients[clientname].hasMode("c") || s.channels[channel].hasMode("c") {
			ccount = 2
		} else {
			ccount = len(s.channels[channel].clients)
		}

		for i := 1; i < ccount; i++ {
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
		return
	}

	if s.channels[channel].topic != "" {
		tprefix := anonymous
		tcommand := irc.TOPIC
		if !changed {
			tprefix = anonirc
			tcommand = irc.RPL_TOPIC
		}
		s.clients[client].writebuffer <- &irc.Message{&tprefix, tcommand, []string{channel, s.channels[channel].topic}}

		if !changed {
			s.clients[client].writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.RPL_TOPICWHOTIME, s.clients[client].nick, channel, "Anonymous", fmt.Sprintf("%d", s.channels[channel].topictime)}, " "), nil}
		}
	}
}

func (s *Server) handleTopic(channel string, client string, topic string) {
	if !s.inChannel(channel, client) {
		s.clients[client].sendNotice("Invalid use of TOPIC")
		return
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
	if len(params) == 0 || len(params[0]) == 0 {
		c.sendNotice("Invalid use of MODE")
		return
	}

	if params[0][0] == '#' {
		if !s.channelExists(params[0]) {
			return
		}

		channel := s.channels[params[0]]
		if len(params) == 1 {
			c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.RPL_CHANNELMODEIS, c.nick, params[0], channel.printModes(nil)}, " "), []string{}}

			// Send channel creation time
			c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{"329", c.nick, params[0], fmt.Sprintf("%d", int32(channel.created))}, " "), []string{}}
		} else if len(params) > 1 && len(params[1]) > 0  && (params[1][0] == '+' || params[1][0] == '-') {
			lastmodes := make(map[string]string)
			for mode, modevalue := range channel.modes {
				lastmodes[mode] = modevalue
			}

			channel.Lock()
			if params[1][0] == '+' {
				channel.addModes(params[1][1:])
			} else {
				channel.removeModes(params[1][1:])
			}
			s.enforceModes(params[0])
			channel.Unlock()

			if !reflect.DeepEqual(channel.modes, lastmodes) {
				for sclient := range channel.clients {
					s.clients[sclient].writebuffer <- &irc.Message{&anonymous, irc.MODE, []string{params[0], channel.printModes(lastmodes)}}
				}

				s.updateUserCount(params[0])
			}
		}
	} else {
		if len(params) == 1 {
			c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.RPL_UMODEIS, c.nick, c.printModes(nil)}, " "), []string{}}
			return
		}

		lastmodes := make(map[string]string)
		for mode, modevalue := range c.modes {
			lastmodes[mode] = modevalue
		}

		forcedisplay := true
		if len(params) > 1 && len(params[1]) > 0  && (params[1][0] == '+' || params[1][0] == '-') {
			forcedisplay = false

			c.Lock()
			if params[1][0] == '+' {
				c.addModes(params[1][1:])
			} else {
				c.removeModes(params[1][1:])
			}
			c.Unlock()
		}

		if forcedisplay || !reflect.DeepEqual(c.modes, lastmodes) {
			printmodes := lastmodes
			if forcedisplay {
				printmodes = nil
			}

			c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.MODE, c.nick}, " "), []string{c.printModes(printmodes)}}
		}
	}
}

func (s *Server) handlePrivmsg(channel string, client string, message string) {
	if !s.inChannel(channel, client) {
		return // Not in channel
	}

	for sclient := range s.channels[channel].clients {
		if s.clients[sclient].identifier != client {
			s.clients[sclient].writebuffer <- &irc.Message{&anonymous, irc.PRIVMSG, []string{channel, message}}
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
		if (msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_LS) {
			c.writebuffer <- &irc.Message{&anonirc, irc.CAP, []string{msg.Params[0], "userhost-in-names"}}
		} else if (msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_REQ) {
			if strings.Contains(msg.Trailing(), "userhost-in-names") {
				c.capHostInNames = true
			}
			c.writebuffer <- &irc.Message{&anonirc, irc.CAP, []string{irc.CAP_ACK, msg.Trailing()}}
		} else if (msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_LIST) {
			caps := []string{}
			if c.capHostInNames {
				caps = append(caps, "userhost-in-names")
			}
			c.writebuffer <- &irc.Message{&anonirc, irc.CAP, []string{msg.Params[0], strings.Join(caps, " ")}}
		} else if (msg.Command == irc.PING) {
			c.writebuffer <- &irc.Message{&anonirc, irc.PONG + " AnonIRC", []string{msg.Trailing()}}
		} else if (msg.Command == irc.NICK && c.nick == "*" && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] != "" && msg.Params[0] != "*") {
			c.nick = strings.Trim(msg.Params[0], "\"")
		} else if (msg.Command == irc.USER && c.user == "" && len(msg.Params) >= 3 && msg.Params[0] != "" && msg.Params[2] != "") {
			c.user = strings.Trim(msg.Params[0], "\"")
			c.host = strings.Trim(msg.Params[2], "\"")

			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_WELCOME, []string{"Welcome to AnonIRC " + c.getPrefix().String()}}
			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_YOURHOST, []string{"Your host is AnonIRC, running version AnonIRCd"}}
			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_CREATED, []string{fmt.Sprintf("This server was created %s", time.Unix(s.created, 0).UTC())}}
			c.writebuffer <- &irc.Message{&anonirc, strings.Join([]string{irc.RPL_MYINFO, c.nick, "AnonIRC AnonIRCd", CLIENT_MODES, CHANNEL_MODES, CHANNEL_MODES_ARG}, " "), []string{}}

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

			s.joinChannel("#", c.identifier)
		} else if (msg.Command == irc.LIST) {
			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_LISTSTART, []string{"Channel", "Users Name"}}
			for channelname, channel := range s.channels {
				var ccount int
				if c.hasMode("c") || channel.hasMode("c") {
					ccount = 2
				} else {
					ccount = len(channel.clients)
				}
				c.writebuffer <- &irc.Message{&anonirc, irc.RPL_LIST, []string{channelname, strconv.Itoa(ccount), "[" + channel.printModes(nil) + "] " + channel.topic}}
			}
			c.writebuffer <- &irc.Message{&anonirc, irc.RPL_LISTEND, []string{"End of /LIST"}}
		} else if (msg.Command == irc.JOIN && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#') {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.joinChannel(channel, c.identifier)
			}
		} else if (msg.Command == irc.NAMES && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#') {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.sendNames(channel, c.identifier)
			}
		} else if (msg.Command == irc.WHO && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#') {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				if s.inChannel(channel, c.identifier) {
					var ccount int
					if c.hasMode("c") || s.channels[channel].hasMode("c") {
						ccount = 2
					} else {
						ccount = len(s.channels[channel].clients)
					}

					for i := 0; i < ccount; i++ {
						var prfx *irc.Prefix
						if i == 0 {
							prfx = c.getPrefix()
						} else {
							prfx = s.getAnonymousPrefix(i)
						}

						c.writebuffer <- &irc.Message{&anonirc, irc.RPL_WHOREPLY, []string{channel, prfx.User, prfx.Host, "AnonIRC", prfx.Name, "H", "0 Anonymous"}}
					}
					c.writebuffer <- &irc.Message{&anonirc, irc.RPL_ENDOFWHO, []string{channel, "End of /WHO list."}}
				}
			}
		} else if (msg.Command == irc.MODE) {
			if len(msg.Params) == 2 && msg.Params[0][0] == '#' && msg.Params[1] == "b" {
				c.writebuffer <- &irc.Message{&anonirc, irc.RPL_ENDOFBANLIST, []string{msg.Params[0], "End of Channel Ban List"}}
			} else {
				s.handleMode(c, msg.Params)
			}
		} else if (msg.Command == irc.TOPIC && len(msg.Params) > 0 && len(msg.Params[0]) > 0) {
			s.handleTopic(msg.Params[0], c.identifier, msg.Trailing())
		} else if (msg.Command == irc.PRIVMSG && len(msg.Params) > 0 && len(msg.Params[0]) > 0) {
			s.handlePrivmsg(msg.Params[0], c.identifier, msg.Trailing())
		} else if (msg.Command == irc.PART && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#') {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.partChannel(channel, c.identifier, "")
			}
		} else if (msg.Command == irc.QUIT) {
			s.partAllChannels(c.identifier)
		}
	}
}

func (s *Server) handleWrite(c *Client) {
	for msg := range c.writebuffer {
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

func (s *Server) handleConnection(conn net.Conn, ssl bool) {
	defer conn.Close()

	var identifier string
	for {
		identifier = randomIdentifier()
		if _, ok := s.clients[identifier]; !ok {
			break
		}
	}

	client := Client{Entity{ENTITY_CLIENT, time.Now().Unix(), make(map[string]string), new(sync.RWMutex)}, identifier, ssl, "*", "", "", conn, []string{}, make(chan *irc.Message), irc.NewDecoder(conn), irc.NewEncoder(conn), false}

	s.Lock()
	s.clients[client.identifier] = &client
	s.Unlock()

	go s.handleWrite(&client)
	s.handleRead(&client)
}

func (s *Server) listenPlain() {
	listen, err := net.Listen("tcp", ":6667")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listen.Close()

	for {
		conn, err := listen.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		go s.handleConnection(conn, false)
	}
}

func (s *Server) listenSSL() {
	if s.config.SSLCert == "" {
		return // SSL is disabled
	}

	cert, err := tls.LoadX509KeyPair(s.config.SSLCert, s.config.SSLKey)
	if err != nil {
		log.Fatalf("Failed to load SSL certificate: %v", err)
	}

	listen, err := tls.Listen("tcp", ":6697", &tls.Config{Certificates:[]tls.Certificate{cert}})
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listen.Close()

	for {
		conn, err := listen.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		go s.handleConnection(conn, true)
	}
}

func (s *Server) pingClients() {
	for {
		for _, c := range s.clients {
			ping := fmt.Sprintf("anonirc%d%d", int32(time.Now().Unix()), rand.Intn(1000))
			//c.pings = append(c.pings, ping)
			c.writebuffer <- &irc.Message{nil, irc.PING, []string{ping}}
		}
		time.Sleep(15 * time.Second)
	}
}

func (s *Server) listen() {
	go s.listenPlain()
	go s.listenSSL()

	s.pingClients()
}
