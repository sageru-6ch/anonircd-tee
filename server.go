package main

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto/tls"
	"github.com/BurntSushi/toml"
	irc "gopkg.in/sorcix/irc.v2"
	"math/rand"
	"os"
	"reflect"
)

type Config struct {
	SSLCert string
	SSLKey  string
}

type Server struct {
	config   *Config
	created  int64
	clients  map[string]*Client
	channels map[string]*Channel

	restartplain chan bool
	restartssl   chan bool

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
		s.channels[channel] = &Channel{Entity{ENTITY_CHANNEL, channel, time.Now().Unix(), make(map[string]string), new(sync.RWMutex)}, make(map[string]int), "", 0}
	} else if s.channels[channel].hasMode("z") && !s.clients[client].ssl {
		s.clients[client].sendNotice("Unable to join " + channel + ": SSL connections only (channel mode +z)")
		return
	}
	s.channels[channel].Lock()
	s.channels[channel].clients[client] = s.getClientCount(channel, client)
	s.channels[channel].Unlock()

	s.clients[client].write(&irc.Message{s.clients[client].getPrefix(), irc.JOIN, []string{channel}})

	s.sendNames(channel, client)
	s.updateClientCount(channel, "")
	s.sendTopic(channel, client, false)
}

func (s *Server) partChannel(channel string, client string, reason string) {
	if !s.inChannel(channel, client) {
		return
	}

	s.clients[client].write(&irc.Message{s.clients[client].getPrefix(), irc.PART, []string{channel, reason}})

	s.channels[channel].Lock()
	delete(s.channels[channel].clients, client)
	s.channels[channel].Unlock()

	s.updateClientCount(channel, "")
}

func (s *Server) partAllChannels(client string) {
	for channelname := range s.getChannels(client) {
		s.partChannel(channelname, client, "")
	}
}

func (s *Server) enforceModes(channel string) {
	if s.channels[channel].hasMode("z") {
		for client := range s.getClients(channel) {
			if !s.clients[client].ssl {
				s.partChannel(channel, client, "Only SSL connections are allowed in this channel")
			}
		}
	}
}

func (s *Server) getClientCount(channel string, client string) int {
	if s.clients[client].hasMode("c") || s.channels[channel].hasMode("c") {
		return 2
	}

	return len(s.channels[channel].clients)
}

func (s *Server) updateClientCount(channel string, client string) {
	clients := make(map[string]int)
	if client != "" {
		clients[client] = s.channels[channel].clients[client]
	} else {
		clients = s.channels[channel].clients
	}
	for cclient, ccount := range clients {
		chancount := s.getClientCount(channel, cclient)
		if ccount < chancount {
			s.channels[channel].Lock()
			for i := ccount; i < chancount; i++ {
				s.clients[cclient].write(&irc.Message{s.getAnonymousPrefix(i), irc.JOIN, []string{channel}})
			}

			s.channels[channel].clients[cclient] = chancount
			s.channels[channel].Unlock()
		} else if ccount > chancount {
			s.channels[channel].Lock()
			for i := ccount; i > chancount; i-- {
				s.clients[cclient].write(&irc.Message{s.getAnonymousPrefix(i - 1), irc.PART, []string{channel}})
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

		ccount := s.getClientCount(channel, clientname)
		for i := 1; i < ccount; i++ {
			if c.capHostInNames {
				names = append(names, s.getAnonymousPrefix(i).String())
			} else {
				names = append(names, s.getAnonymousPrefix(i).Name)
			}
		}

		c.write(&irc.Message{&anonirc, irc.RPL_NAMREPLY, []string{"=", channel, strings.Join(names, " ")}})
		c.write(&irc.Message{&anonirc, irc.RPL_ENDOFNAMES, []string{channel, "End of /NAMES list."}})
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
		s.clients[client].write(&irc.Message{&tprefix, tcommand, []string{channel, s.channels[channel].topic}})

		if !changed {
			s.clients[client].write(&irc.Message{&anonirc, strings.Join([]string{irc.RPL_TOPICWHOTIME, s.clients[client].nick, channel, "Anonymous", fmt.Sprintf("%d", s.channels[channel].topictime)}, " "), nil})
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
		if len(params) == 1 || params[1] == "" {
			c.write(&irc.Message{&anonirc, strings.Join([]string{irc.RPL_CHANNELMODEIS, c.nick, params[0], channel.printModes(channel.modes, nil)}, " "), []string{}})

			// Send channel creation time
			c.write(&irc.Message{&anonirc, strings.Join([]string{"329", c.nick, params[0], fmt.Sprintf("%d", int32(channel.created))}, " "), []string{}})
		} else if len(params) > 1 && len(params[1]) > 0 && (params[1][0] == '+' || params[1][0] == '-') {
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
			channel.Unlock()
			s.enforceModes(params[0])

			if !reflect.DeepEqual(channel.modes, lastmodes) {
				// TODO: Check if local modes were set/unset, only send changes to local client
				addedmodes, removedmodes := channel.diffModes(lastmodes)

				resendusercount := false
				if _, ok := addedmodes["c"]; ok {
					resendusercount = true
				}
				if _, ok := removedmodes["c"]; ok {
					resendusercount = true
				}

				if len(addedmodes) == 0 && len(removedmodes) == 0 {
					addedmodes = c.modes
				}

				for sclient := range channel.clients {
					s.clients[sclient].write(&irc.Message{&anonymous, irc.MODE, []string{params[0], channel.printModes(addedmodes, removedmodes)}})
				}

				if resendusercount {
					s.updateClientCount(params[0], "")
				}
			}
		}
	} else {
		if len(params) == 1 || params[1] == "" {
			c.write(&irc.Message{&anonirc, strings.Join([]string{irc.RPL_UMODEIS, c.nick, c.printModes(c.modes, nil)}, " "), []string{}})
			return
		}

		lastmodes := make(map[string]string)
		for mode, modevalue := range c.modes {
			lastmodes[mode] = modevalue
		}

		if len(params) > 1 && len(params[1]) > 0 && (params[1][0] == '+' || params[1][0] == '-') {
			c.Lock()
			if params[1][0] == '+' {
				c.addModes(params[1][1:])
			} else {
				c.removeModes(params[1][1:])
			}
			c.Unlock()
		}

		if !reflect.DeepEqual(c.modes, lastmodes) {
			addedmodes, removedmodes := c.diffModes(lastmodes)

			resendusercount := false
			if _, ok := addedmodes["c"]; ok {
				resendusercount = true
			}
			if _, ok := removedmodes["c"]; ok {
				resendusercount = true
			}

			if len(addedmodes) == 0 && len(removedmodes) == 0 {
				addedmodes = c.modes
			}

			c.write(&irc.Message{&anonirc, strings.Join([]string{irc.MODE, c.nick}, " "), []string{c.printModes(addedmodes, removedmodes)}})

			if resendusercount {
				for ch := range s.getChannels(c.identifier) {
					s.updateClientCount(ch, c.identifier)
				}
			}
		}
	}
}

func (s *Server) handlePrivmsg(channel string, client string, message string) {
	if !s.inChannel(channel, client) {
		return // Not in channel
	}

	for sclient := range s.channels[channel].clients {
		if s.clients[sclient].identifier != client {
			s.clients[sclient].write(&irc.Message{&anonymous, irc.PRIVMSG, []string{channel, message}})
		}
	}
}

func (s *Server) handleRead(c *Client) {
	for {
		c.conn.SetDeadline(time.Now().Add(300 * time.Second))
		msg, err := s.clients[c.identifier].reader.Decode()
		if err != nil {
			log.Println("Unable to read from client:", err)
			s.partAllChannels(c.identifier)
			return
		}
		if len(msg.Command) >= 4 && msg.Command[0:4] != irc.PING && msg.Command[0:4] != irc.PONG {
			log.Println(c.identifier, "<-", fmt.Sprintf("%s", msg))
		}
		if msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_LS {
			c.write(&irc.Message{&anonirc, irc.CAP, []string{msg.Params[0], "userhost-in-names"}})
		} else if msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_REQ {
			if strings.Contains(msg.Trailing(), "userhost-in-names") {
				c.capHostInNames = true
			}
			c.write(&irc.Message{&anonirc, irc.CAP, []string{irc.CAP_ACK, msg.Trailing()}})
		} else if msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_LIST {
			caps := []string{}
			if c.capHostInNames {
				caps = append(caps, "userhost-in-names")
			}
			c.write(&irc.Message{&anonirc, irc.CAP, []string{msg.Params[0], strings.Join(caps, " ")}})
		} else if msg.Command == irc.PING {
			c.write(&irc.Message{&anonirc, irc.PONG + " AnonIRC", []string{msg.Trailing()}})
		} else if msg.Command == irc.NICK && c.nick == "*" && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] != "" && msg.Params[0] != "*" {
			c.nick = strings.Trim(msg.Params[0], "\"")
		} else if msg.Command == irc.USER && c.user == "" && len(msg.Params) >= 3 && msg.Params[0] != "" && msg.Params[2] != "" {
			c.user = strings.Trim(msg.Params[0], "\"")
			c.host = strings.Trim(msg.Params[2], "\"")

			c.write(&irc.Message{&anonirc, irc.RPL_WELCOME, []string{"Welcome to AnonIRC " + c.getPrefix().String()}})
			c.write(&irc.Message{&anonirc, irc.RPL_YOURHOST, []string{"Your host is AnonIRC, running version AnonIRCd"}})
			c.write(&irc.Message{&anonirc, irc.RPL_CREATED, []string{fmt.Sprintf("This server was created %s", time.Unix(s.created, 0).UTC())}})
			c.write(&irc.Message{&anonirc, strings.Join([]string{irc.RPL_MYINFO, c.nick, "AnonIRC AnonIRCd", CLIENT_MODES, CHANNEL_MODES, CHANNEL_MODES_ARG}, " "), []string{}})

			motdsplit := strings.Split(motd, "\n")
			for i, motdmsg := range motdsplit {
				var motdcode string
				if i == 0 {
					motdcode = irc.RPL_MOTDSTART
				} else if i < len(motdsplit)-1 {
					motdcode = irc.RPL_MOTD
				} else {
					motdcode = irc.RPL_ENDOFMOTD
				}
				c.write(&irc.Message{&anonirc, motdcode, []string{"  " + motdmsg}})
			}

			s.joinChannel("#", c.identifier)
		} else if msg.Command == irc.LIST {
			var ccount int
			chans := make(map[string]int)
			for channelname, channel := range s.channels {
				if !channel.hasMode("p") && !channel.hasMode("s") {
					ccount = s.getClientCount(channelname, c.identifier)
					chans[channelname] = ccount
				}
			}

			c.write(&irc.Message{&anonirc, irc.RPL_LISTSTART, []string{"Channel", "Users Name"}})
			for _, pl := range sortMapByValues(chans) {
				c.write(&irc.Message{&anonirc, irc.RPL_LIST, []string{pl.Key, strconv.Itoa(pl.Value), "[" + s.channels[pl.Key].printModes(s.channels[pl.Key].modes, nil) + "] " + s.channels[pl.Key].topic}})
			}
			c.write(&irc.Message{&anonirc, irc.RPL_LISTEND, []string{"End of /LIST"}})
		} else if msg.Command == irc.JOIN && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#' {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.joinChannel(channel, c.identifier)
			}
		} else if msg.Command == irc.NAMES && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#' {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.sendNames(channel, c.identifier)
			}
		} else if msg.Command == irc.WHO && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#' {
			var ccount int
			for _, channel := range strings.Split(msg.Params[0], ",") {
				if s.inChannel(channel, c.identifier) {
					ccount = s.getClientCount(channel, c.identifier)
					for i := 0; i < ccount; i++ {
						var prfx *irc.Prefix
						if i == 0 {
							prfx = c.getPrefix()
						} else {
							prfx = s.getAnonymousPrefix(i)
						}

						c.write(&irc.Message{&anonirc, irc.RPL_WHOREPLY, []string{channel, prfx.User, prfx.Host, "AnonIRC", prfx.Name, "H", "0 Anonymous"}})
					}
					c.write(&irc.Message{&anonirc, irc.RPL_ENDOFWHO, []string{channel, "End of /WHO list."}})
				}
			}
		} else if msg.Command == irc.MODE {
			if len(msg.Params) == 2 && msg.Params[0][0] == '#' && msg.Params[1] == "b" {
				c.write(&irc.Message{&anonirc, irc.RPL_ENDOFBANLIST, []string{msg.Params[0], "End of Channel Ban List"}})
			} else {
				s.handleMode(c, msg.Params)
			}
		} else if msg.Command == irc.TOPIC && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			s.handleTopic(msg.Params[0], c.identifier, msg.Trailing())
		} else if msg.Command == irc.PRIVMSG && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			s.handlePrivmsg(msg.Params[0], c.identifier, msg.Trailing())
		} else if msg.Command == irc.PART && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0][0] == '#' {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.partChannel(channel, c.identifier, "")
			}
		} else if msg.Command == irc.QUIT {
			s.partAllChannels(c.identifier)
		}
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

	client := Client{Entity{ENTITY_CLIENT, identifier, time.Now().Unix(), make(map[string]string), new(sync.RWMutex)}, ssl, "*", "", "", conn, make(chan *irc.Message), irc.NewDecoder(conn), irc.NewEncoder(conn), false}

	s.Lock()
	s.clients[client.identifier] = &client
	s.Unlock()

	go client.handleWrite()
	s.handleRead(&client)
}

func (s *Server) listenPlain() {
	for {
		listen, err := net.Listen("tcp", ":6667")
		if err != nil {
			log.Println("Failed to listen: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}
		log.Println("Listening on 6667")

	accept:
		for {
			select {
			case _ = <-s.restartplain:
				break accept
			default:
				conn, err := listen.Accept()
				if err != nil {
					log.Println("Error accepting connection:", err)
					continue
				}
				go s.handleConnection(conn, true)
			}
		}
		listen.Close()
	}
}

func (s *Server) listenSSL() {
	for {
		if s.config.SSLCert == "" {
			time.Sleep(1 * time.Minute)
			return // SSL is disabled
		}

		cert, err := tls.LoadX509KeyPair(s.config.SSLCert, s.config.SSLKey)
		if err != nil {
			log.Println("Failed to load SSL certificate: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}

		listen, err := tls.Listen("tcp", ":6697", &tls.Config{Certificates: []tls.Certificate{cert}})
		if err != nil {
			log.Println("Failed to listen: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}
		log.Println("Listening on +6697")

	accept:
		for {
			select {
			case _ = <-s.restartssl:
				break accept
			default:
				conn, err := listen.Accept()
				if err != nil {
					log.Println("Error accepting connection:", err)
					continue
				}
				go s.handleConnection(conn, true)
			}
		}
		listen.Close()
	}
}

func (s *Server) pingClients() {
	for {
		s.Lock()
		for _, c := range s.clients {
			c.write(&irc.Message{nil, irc.PING, []string{fmt.Sprintf("anonirc%d%d", int32(time.Now().Unix()), rand.Intn(1000))}})
		}
		s.Unlock()
		time.Sleep(90 * time.Second)
	}
}

func (s *Server) loadConfig() {
	s.Lock()
	if _, err := os.Stat("anonircd.conf"); err == nil {
		if _, err := toml.DecodeFile("anonircd.conf", &s.config); err != nil {
			log.Fatalf("Failed to read anonircd.conf: %v", err)
		}
	}
	s.Unlock()
}

func (s *Server) reload() {
	log.Println("Reloading configuration")
	s.loadConfig()
	s.restartplain <- true
	s.restartssl <- true
}

func (s *Server) listen() {
	go s.listenPlain()
	go s.listenSSL()

	s.pingClients()
}
