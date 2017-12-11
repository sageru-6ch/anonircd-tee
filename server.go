package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
	"golang.org/x/crypto/sha3"
	"gopkg.in/sorcix/irc.v2"
)

const (
	COMMAND_HELP = "HELP"
	COMMAND_INFO = "INFO"

	// User commands
	COMMAND_REGISTER = "REGISTER"
	COMMAND_IDENTIFY = "IDENTIFY"
	COMMAND_TOKEN    = "TOKEN"
	COMMAND_USERNAME = "USERNAME"
	COMMAND_PASSWORD = "PASSWORD"

	// Channel/server commands
	COMMAND_FOUND  = "FOUND"
	COMMAND_DROP   = "DROP"
	COMMAND_GRANT  = "GRANT"
	COMMAND_REVEAL = "REVEAL"
	COMMAND_KICK   = "KICK"
	COMMAND_BAN    = "BAN"

	// Server admins only
	COMMAND_KILL    = "KILL"
	COMMAND_STATS   = "STATS"
	COMMAND_REHASH  = "REHASH"
	COMMAND_UPGRADE = "UPGRADE"
)

var serverCommands = []string{COMMAND_KILL, COMMAND_STATS, COMMAND_REHASH, COMMAND_UPGRADE}

const (
	PERMISSION_CLIENT     = 0
	PERMISSION_REGISTERED = 1
	PERMISSION_VIP        = 2
	PERMISSION_MODERATOR  = 3
	PERMISSION_ADMIN      = 4
	PERMISSION_SUPERADMIN = 5
)

var permissionLabels = map[int]string{
	PERMISSION_CLIENT:     "Client",
	PERMISSION_REGISTERED: "Registered",
	PERMISSION_VIP:        "VIP",
	PERMISSION_MODERATOR:  "Moderator",
	PERMISSION_ADMIN:      "Administrator",
	PERMISSION_SUPERADMIN: "Super Administrator",
}

var commandRestrictions = map[int][]string{
	PERMISSION_REGISTERED: {COMMAND_TOKEN, COMMAND_USERNAME, COMMAND_PASSWORD, COMMAND_FOUND},
	PERMISSION_MODERATOR:  {COMMAND_REVEAL, COMMAND_KICK, COMMAND_BAN},
	PERMISSION_ADMIN:      {COMMAND_GRANT},
	PERMISSION_SUPERADMIN: {COMMAND_DROP, COMMAND_KILL, COMMAND_STATS, COMMAND_REHASH, COMMAND_UPGRADE}}

var helpDuration = "Duration can be 0 to never expire, or e.g. 30m, 1h, 2d, 3w"
var commandUsage = map[string][]string{
	COMMAND_HELP: {"[command]",
		"Print info regarding all commands or a specific command"},
	COMMAND_INFO: {"[channel]",
		"When a channel is specified, prints info including whether it is registered",
		"Without a channel, server info is printed"},
	COMMAND_REGISTER: {"<username> <password>",
		"Create an account, allowing you to found channels and moderate existing channels",
		"See IDENTIFY, FOUND, GRANT"},
	COMMAND_IDENTIFY: {"[username] <password>",
		"Identify to a previously registered account",
		"If username is omitted, it will be replaced with your current nick",
		"Note that you may automatically identify when connecting by specifying a server password of your username and password separated by a colon - Example:  admin:hunter2"},
	COMMAND_TOKEN: {"<channel>",
		"Returns a token which can be used by channel administrators to grant special access to your account"},
	COMMAND_USERNAME: {"<username> <password> <new username> <confirm new username>",
		"Change your username"},
	COMMAND_PASSWORD: {"<username> <password> <new password> <confirm new password>",
		"Change your password"},
	COMMAND_FOUND: {"<channel>",
		"Register a channel"},
	COMMAND_GRANT: {"<channel> [account] [updated access]",
		"When an account token isn't specified, all permissions are listed",
		"View or update a user's access level by specifying their account token",
		"To remove an account, set their access level to User"},
	COMMAND_REVEAL: {"<channel> [page] [full]",
		"Print channel log, allowing KICK/BAN to be used",
		fmt.Sprintf("Results start at page 1, %d per page", CHANNEL_LOGS_PER_PAGE),
		"All log entries are returned when viewing page -1",
		"By default joins and parts are hidden, use 'full' to show them"},
	COMMAND_KICK: {"<channel> <5 digit log number> [reason]",
		"Kick a user from a channel"},
	COMMAND_BAN: {"<channel> <5 digit log number> <duration> [reason]",
		"Kick and ban a user from a channel",
		helpDuration},
	COMMAND_DROP: {"<channel> <confirm channel>",
		"Delete all channel data, allowing it to be FOUNDed again"},
	COMMAND_KILL: {"<channel> <5 digit log number> <duration> [reason]",
		"Disconnect and ban a user from the server",
		helpDuration},
	COMMAND_STATS: {"",
		"Print the current number of clients and channels"},
	COMMAND_REHASH: {"",
		"Reload the server configuration"},
	COMMAND_UPGRADE: {"",
		"Upgrade the server without disconnecting clients"},
}

type Config struct {
	Salt     string
	DBDriver string
	DBSource string
	SSLCert  string
	SSLKey   string
}

type Server struct {
	config       *Config
	configfile   string
	created      int64
	clients      *sync.Map
	channels     *sync.Map
	odyssey      *os.File
	odysseymutex *sync.RWMutex

	restartplain chan bool
	restartssl   chan bool

	*sync.RWMutex
}

var db = &Database{}

func NewServer(configfile string) *Server {
	s := &Server{}
	s.config = &Config{}
	s.configfile = configfile
	s.created = time.Now().Unix()
	s.clients = new(sync.Map)
	s.channels = new(sync.Map)
	s.odysseymutex = new(sync.RWMutex)

	s.restartplain = make(chan bool, 1)
	s.restartssl = make(chan bool, 1)
	s.RWMutex = new(sync.RWMutex)

	return s
}

func (s *Server) hashPassword(username string, password string) string {
	sha512 := sha3.New512()
	_, err := sha512.Write([]byte(strings.Join([]string{username, s.config.Salt, password}, "-")))
	if err != nil {
		return ""
	}

	return base64.URLEncoding.EncodeToString(sha512.Sum(nil))
}

func (s *Server) getAnonymousPrefix(i int) *irc.Prefix {
	prefix := prefixAnonymous
	if i > 1 {
		prefix.Name += fmt.Sprintf("%d", i)
	}
	return &prefix
}

func (s *Server) getChannel(channel string) *Channel {
	if ch, ok := s.channels.Load(channel); ok {
		return ch.(*Channel)
	}

	return nil
}

func (s *Server) getChannels(client string) map[string]*Channel {
	channels := make(map[string]*Channel)
	s.channels.Range(func(k, v interface{}) bool {
		key := k.(string)
		channel := v.(*Channel)
		if client == "" || s.inChannel(key, client) {
			channels[key] = channel
		}

		return true
	})

	return channels
}

func (s *Server) channelCount() int {
	i := 0
	s.channels.Range(func(k, v interface{}) bool {
		i++
		return true
	})

	return i
}

func (s *Server) getClient(client string) *Client {
	if cl, ok := s.clients.Load(client); ok {
		return cl.(*Client)
	}

	return nil
}

func (s *Server) getClients(channel string) map[string]*Client {
	clients := make(map[string]*Client)

	if channel == "" {
		s.clients.Range(func(k, v interface{}) bool {
			cl := s.getClient(k.(string))
			if cl != nil {
				clients[cl.identifier] = cl
			}
			return true
		})
		return clients
	}

	ch := s.getChannel(channel)
	if ch == nil {
		return clients
	}

	ch.clients.Range(func(k, v interface{}) bool {
		cl := s.getClient(k.(string))
		if cl != nil {
			clients[cl.identifier] = cl
		}
		return true
	})

	return clients
}

func (s *Server) clientCount() int {
	i := 0
	s.clients.Range(func(k, v interface{}) bool {
		i++
		return true
	})

	return i
}

func (s *Server) revealClient(channel string, identifier string) *Client {
	riphash, raccount := s.revealClientInfo(channel, identifier)
	if riphash == "" && raccount == 0 {
		log.Println("hash not found")
		return nil
	}
	log.Println("have hash")
	cls := s.getClients("")
	for _, rcl := range cls {
		if rcl.iphash == riphash || (rcl.account > 0 && rcl.account == raccount) {
			return rcl
		}
	}

	return nil
}

func (s *Server) revealClientInfo(channel string, identifier string) (string, int) {
	if len(identifier) != 5 {
		return "", 0
	}

	ch := s.getChannel(channel)
	if ch == nil {
		return "", 0
	}

	return ch.RevealInfo(identifier)
}

func (s *Server) inChannel(channel string, client string) bool {
	ch := s.getChannel(channel)
	if ch != nil {
		_, ok := ch.clients.Load(client)
		return ok
	}

	return false
}

func (s *Server) joinChannel(channel string, client string) {
	if s.inChannel(channel, client) {
		return // Already in channel
	}

	cl := s.getClient(client)
	if cl == nil {
		return
	}

	if len(channel) == 0 {
		return
	} else if channel[0] == '&' {
		if cl.globalPermission() < PERMISSION_VIP {
			cl.accessDenied(0)
			return
		}
	} else if channel[0] != '#' {
		return
	}

	ch := s.getChannel(channel)
	if ch == nil {
		ch = NewChannel(channel)
		s.channels.Store(channel, ch)
	} else if ch.hasMode("z") && !cl.ssl {
		cl.sendNotice("Unable to join " + channel + ": SSL connections only (channel mode +z)")
		return
	}

	banned, reason := cl.isBanned(channel)
	if banned {
		ex := ""
		if reason != "" {
			ex = ". Reason: " + reason
		}
		cl.sendNotice("Unable to join " + channel + ": You are banned" + ex)
		return
	}

	ch.clients.Store(client, s.clientsInChannel(channel, client)+1)
	cl.write(&irc.Message{cl.getPrefix(), irc.JOIN, []string{channel}})
	ch.Log(cl, irc.JOIN, "")

	s.sendNames(channel, client)
	s.updateClientCount(channel, client, "")
	s.sendTopic(channel, client, false)
}

func (s *Server) partChannel(channel string, client string, reason string) {
	ch := s.getChannel(channel)
	cl := s.getClient(client)

	if cl == nil || !s.inChannel(channel, client) {
		return
	}

	cl.write(&irc.Message{cl.getPrefix(), irc.PART, []string{channel, reason}})
	ch.Log(cl, irc.PART, reason)
	ch.clients.Delete(client)

	s.updateClientCount(channel, client, reason)
	// TODO: Destroy empty channel
}

func (s *Server) partAllChannels(client string, reason string) {
	for channelname := range s.getChannels(client) {
		s.partChannel(channelname, client, reason)
	}
}

func (s *Server) revealChannelLog(channel string, client string, page int, full bool) {
	cl := s.getClient(client)
	if cl == nil {
		return
	}

	// TODO: Check channel permission
	ch := s.getChannel(channel)
	if ch == nil {
		cl.sendError("Unable to reveal, invalid channel specified")
		return
	} else if !ch.HasClient(client) {
		cl.sendError("Unable to reveal, you are not in that channel")
		return
	}

	r := ch.RevealLog(page, full)
	for _, rev := range r {
		cl.sendMessage(rev)
	}
}

func (s *Server) enforceModes(channel string) {
	ch := s.getChannel(channel)

	if ch != nil && ch.hasMode("z") {
		for client, cl := range s.getClients(channel) {
			if !cl.ssl {
				s.partChannel(channel, client, fmt.Sprintf("You must connect via SSL to join %s", channel))
			}
		}
	}
}

func (s *Server) clientsInChannel(channel string, client string) int {
	ch := s.getChannel(channel)
	cl := s.getClient(client)

	if ch == nil || cl == nil {
		return 0
	}

	ccount := 0
	ch.clients.Range(func(k, v interface{}) bool {
		ccount++
		return true
	})

	if (ch.hasMode("c") || cl.hasMode("c")) && ccount >= 2 {
		return 2
	}

	return ccount
}

func (s *Server) updateClientCount(channel string, client string, reason string) {
	ch := s.getChannel(channel)

	if ch == nil {
		return
	}

	var reasonShown bool
	ch.clients.Range(func(k, v interface{}) bool {
		cl := s.getClient(k.(string))
		ccount := v.(int)

		if cl == nil {
			return true
		} else if client != "" && ch.hasMode("D") && cl.identifier != client {
			return true
		}

		reasonShown = false
		chancount := s.clientsInChannel(channel, cl.identifier)

		if ccount < chancount {
			for i := ccount; i < chancount; i++ {
				cl.write(&irc.Message{s.getAnonymousPrefix(i), irc.JOIN, []string{channel}})
			}

			ch.clients.Store(cl.identifier, chancount)
		} else if ccount > chancount {
			for i := ccount; i > chancount; i-- {
				pr := ""
				if !reasonShown {
					pr = reason
				}

				cl.write(&irc.Message{s.getAnonymousPrefix(i - 1), irc.PART, []string{channel, pr}})
				reasonShown = true
			}
		} else {
			return true
		}

		ch.clients.Store(cl.identifier, chancount)

		return true
	})
}

func (s *Server) sendNames(channel string, clientname string) {
	if !s.inChannel(channel, clientname) {
		return
	}

	cl := s.getClient(clientname)

	if cl == nil {
		return
	}

	names := []string{}
	if cl.capHostInNames {
		names = append(names, cl.getPrefix().String())
	} else {
		names = append(names, cl.nick)
	}

	ccount := s.clientsInChannel(channel, clientname)
	for i := 1; i < ccount; i++ {
		if cl.capHostInNames {
			names = append(names, s.getAnonymousPrefix(i).String())
		} else {
			names = append(names, s.getAnonymousPrefix(i).Name)
		}
	}

	cl.writeMessage(irc.RPL_NAMREPLY, []string{"=", channel, strings.Join(names, " ")})
	cl.writeMessage(irc.RPL_ENDOFNAMES, []string{channel, "End of /NAMES list."})
}

func (s *Server) sendTopic(channel string, client string, changed bool) {
	if !s.inChannel(channel, client) {
		return
	}

	ch := s.getChannel(channel)
	cl := s.getClient(client)

	if ch == nil || cl == nil {
		return
	}

	tprefix := prefixAnonymous
	tcommand := irc.TOPIC
	if !changed {
		tprefix = prefixAnonIRC
		tcommand = irc.RPL_TOPIC
	}
	cl.write(&irc.Message{&tprefix, tcommand, []string{channel, ch.topic}})

	if !changed {
		cl.writeMessage(strings.Join([]string{irc.RPL_TOPICWHOTIME, cl.nick, channel, prefixAnonymous.Name, fmt.Sprintf("%d", ch.topictime)}, " "), nil)
	}
}

func (s *Server) handleTopic(channel string, client string, topic string) {
	ch := s.getChannel(channel)
	cl := s.getClient(client)

	if ch == nil || cl == nil {
		return
	}

	if !s.inChannel(channel, client) {
		cl.sendNotice("Invalid use of TOPIC")
		return
	}

	chp, err := db.GetPermission(cl.account, channel)
	if err != nil {
		log.Panicf("%+v", err)
	} else if ch.hasMode("t") && (chp.Permission < PERMISSION_VIP) {
		cl.accessDenied(PERMISSION_VIP)
		return
	}

	ch.topic = topic
	ch.topictime = time.Now().Unix()

	ch.clients.Range(func(k, v interface{}) bool {
		s.sendTopic(channel, k.(string), true)
		return true
	})
	ch.Log(cl, irc.TOPIC, ch.topic)
}

func (s *Server) handleMode(c *Client, params []string) {
	// TODO: irssi sends mode <channel> b, send response
	if len(params) == 0 || len(params[0]) == 0 {
		c.sendNotice("Invalid use of MODE")
		return
	}

	if c == nil {
		return
	}

	if validChannelPrefix(params[0]) {
		ch := s.getChannel(params[0])

		if ch == nil {
			return
		}

		if len(params) == 1 || params[1] == "" {
			c.writeMessage(strings.Join([]string{irc.RPL_CHANNELMODEIS, c.nick, params[0], ch.printModes(ch.getModes(), nil)}, " "), []string{})

			// Send channel creation time
			c.writeMessage(strings.Join([]string{"329", c.nick, params[0], fmt.Sprintf("%d", int32(ch.created))}, " "), []string{})
		} else if len(params) > 1 && len(params[1]) > 0 && (params[1][0] == '+' || params[1][0] == '-') {
			if !c.canUse(irc.MODE, params[0]) {
				// TODO: Send proper mode denied message
				c.accessDenied(c.permissionRequired(irc.MODE))
				return
			}

			lastmodes := make(map[string]string)
			for m, mv := range ch.getModes() {
				lastmodes[m] = mv
			}

			if params[1][0] == '+' {
				ch.addModes(params[1][1:])
			} else {
				ch.removeModes(params[1][1:])
			}
			s.enforceModes(params[0])

			if !reflect.DeepEqual(ch.getModes(), lastmodes) {
				// TODO: Check if local modes were set/unset, only send changes to local client
				addedmodes, removedmodes := ch.diffModes(lastmodes)

				resendusercount := false
				if _, ok := addedmodes["c"]; ok {
					resendusercount = true
				}
				if _, ok := removedmodes["c"]; ok {
					resendusercount = true
				}
				if _, ok := removedmodes["D"]; ok {
					resendusercount = true
				}

				if len(addedmodes) == 0 && len(removedmodes) == 0 {
					addedmodes = c.getModes()
				}

				ch.clients.Range(func(k, v interface{}) bool {
					cl := s.getClient(k.(string))
					if cl != nil {
						cl.write(&irc.Message{&prefixAnonymous, irc.MODE, []string{params[0], ch.printModes(addedmodes, removedmodes)}})
					}

					return true
				})

				if resendusercount {
					s.updateClientCount(params[0], c.identifier, "Enforcing MODEs")
				}
			}
		}
	} else {
		if len(params) == 1 || params[1] == "" {
			c.writeMessage(strings.Join([]string{irc.RPL_UMODEIS, c.nick, c.printModes(c.getModes(), nil)}, " "), []string{})
			return
		}

		lastmodes := c.getModes()

		if len(params) > 1 && len(params[1]) > 0 && (params[1][0] == '+' || params[1][0] == '-') {
			if params[1][0] == '+' {
				c.addModes(params[1][1:])
			} else {
				c.removeModes(params[1][1:])
			}
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
			if _, ok := removedmodes["D"]; ok {
				resendusercount = true
			}

			if len(addedmodes) == 0 && len(removedmodes) == 0 {
				addedmodes = c.getModes()
			}

			c.writeMessage(strings.Join([]string{irc.MODE, c.nick}, " "), []string{c.printModes(addedmodes, removedmodes)})

			if resendusercount {
				for ch := range s.getChannels(c.identifier) {
					s.updateClientCount(ch, c.identifier, "Enforcing MODEs")
				}
			}
		}
	}
}

func (s *Server) buildUsage(cl *Client, command string) map[string][]string {
	u := map[string][]string{}
	command = strings.ToUpper(command)
	for cmd, usage := range commandUsage {
		if command == COMMAND_HELP || cmd == command {
			if cl.canUse(cmd, "") {
				u[cmd] = usage
			}
		}
	}

	return u
}

func (s *Server) sendUsage(cl *Client, command string) {
	u := s.buildUsage(cl, command)

	commands := make([]string, 0, len(u))
	for cmd := range u {
		commands = append(commands, cmd)
	}
	sort.Strings(commands)

	var usage []string
	for _, cmd := range commands {
		usage = u[cmd]

		cl.sendMessage(cmd + " " + usage[0])
		for _, ul := range usage[1:] {
			cl.sendMessage("  " + ul)
		}
	}

}

func (s *Server) handleUserCommand(client string, command string, params []string) {
	cl := s.getClient(client)
	if cl == nil {
		return
	}

	var err error
	command = strings.ToUpper(command)
	ch := ""
	if len(params) > 0 {
		ch = params[0]
	}
	if !cl.canUse(command, ch) {
		cl.accessDenied(cl.permissionRequired(command))
		return
	}

	switch command {
	case COMMAND_HELP:
		cmd := command
		if len(params) > 0 {
			cmd = params[0]
		}
		s.sendUsage(cl, cmd)
		return
	case COMMAND_INFO:
		// TODO: when channel is supplied, send whether it is registered and show a notice that it is dropping soon if no super admins have logged in in X days
		cl.sendMessage("Server info: AnonIRCd https://github.com/sageru-6ch/anonircd")
		return
	case COMMAND_REGISTER:
		if len(params) == 0 {
			s.sendUsage(cl, command)
			return
		}

		// TODO: Only alphanumeric username
	case COMMAND_IDENTIFY:
		if len(params) == 0 || len(params) > 2 {
			s.sendUsage(cl, command)
			return
		}

		username := cl.nick
		password := params[0]
		if len(params) == 2 {
			username = params[0]
			password = params[1]
		}

		authSuccess := cl.identify(username, password)
		if authSuccess {
			cl.sendNotice("Identified successfully")

			if cl.globalPermission() >= PERMISSION_VIP {
				s.joinChannel(CHANNEL_SERVER, cl.identifier)
			}

			for clch := range s.getChannels(cl.identifier) {
				banned, br := cl.isBanned(clch)
				if banned {
					reason := "Banned"
					if br != "" {
						reason += ": " + br
					}
					s.partChannel(clch, cl.identifier, reason)
					return
				}
			}
		} else {
			cl.sendNotice("Failed to identify, incorrect username/password")
		}
	case COMMAND_USERNAME:
		if cl.account == 0 {
			cl.sendError("You must identify before using that command")
		}

		if len(params) == 0 || len(params) < 4 {
			s.sendUsage(cl, command)
			return
		}

		if params[2] != params[3] {
			cl.sendError("Unable to change username, new usernames don't match")
			return
		}
		// TODO: Alphanumeric username

		accid, err := db.Auth(params[0], params[1])
		if err != nil {
			log.Panicf("%+v", err)
		}

		if accid == 0 {
			cl.sendError("Unable to change username, incorrect username/password supplied")
			return
		}

		err = db.SetUsername(accid, params[2], params[1])
		if err != nil {
			log.Panicf("%+v", err)
		}
		cl.sendMessage("Username changed successfully")
	case COMMAND_PASSWORD:
		if len(params) == 0 || len(params) < 4 {
			s.sendUsage(cl, command)
			return
		}

		if params[2] != params[3] {
			cl.sendError("Unable to change password, new passwords don't match")
			return
		}

		accid, err := db.Auth(params[0], params[1])
		if err != nil {
			log.Panicf("%+v", err)
		}

		if accid == 0 {
			cl.sendError("Unable to change password, incorrect username/password supplied")
			return
		}

		err = db.SetPassword(accid, params[0], params[2])
		if err != nil {
			log.Panicf("%+v", err)
		}
		cl.sendMessage("Password changed successfully")
	case COMMAND_REVEAL:
		// TODO: &#chan shows moderator audit log, & alone shows server admin audit log
		if len(params) == 0 {
			s.sendUsage(cl, command)
			return
		}

		ch := s.getChannel(params[0])
		if ch == nil {
			cl.sendError("Unable to reveal, invalid channel specified")
			return
		}

		page := 1
		if len(params) > 1 {
			page, err = strconv.Atoi(params[1])
			if err != nil || page < -1 || page == 0 {
				cl.sendError("Unable to reveal, invalid page specified")
				return
			}
		}

		full := false
		if len(params) > 2 {
			if strings.ToLower(params[2]) == "full" {
				full = true
			}
		}

		s.revealChannelLog(params[0], cl.identifier, page, full)
	case COMMAND_KICK:
		if len(params) < 2 {
			s.sendUsage(cl, command)
			return
		}

		ch := s.getChannel(params[0])
		if ch == nil {
			cl.sendError("Unable to kick, invalid channel specified")
			return
		}

		rcl := s.revealClient(params[0], params[1])
		if rcl == nil {
			cl.sendError("Unable to kick, client not found or no longer connected")
			return
		}

		reason := "Kicked"
		if len(params) > 2 {
			reason = fmt.Sprintf("%s: %s", reason, strings.Join(params[2:], " "))
		}
		s.partChannel(ch.identifier, rcl.identifier, reason)
		cl.sendMessage(fmt.Sprintf("Kicked %s %s", params[0], params[1]))
	case COMMAND_BAN:
		if len(params) < 3 {
			s.sendUsage(cl, command)
			return
		}

		ch := s.getChannel(params[0])
		if ch == nil {
			cl.sendError("Unable to ban, invalid channel specified")
			return
		}

		rcl := s.revealClient(params[0], params[1])
		if rcl == nil {
			cl.sendError("Unable to ban, client not found or no longer connected")
			return
		}

		reason := strings.Join(params[3:], " ")

		partmsg := "Banned"
		if len(params) > 3 {
			partmsg = fmt.Sprintf("%s: %s", partmsg, reason)
		}
		// TODO: Apply ban in DB
		s.partChannel(ch.identifier, rcl.identifier, partmsg)
		cl.sendMessage(fmt.Sprintf("Banned %s %s", params[0], params[1]))
	case COMMAND_KILL:
		if len(params) < 3 {
			s.sendUsage(cl, command)
			return
		}

		rcl := s.revealClient(params[0], params[1])
		if rcl == nil {
			cl.sendError("Unable to kill, client not found or no longer connected")
			return
		}

		reason := "Killed"
		if len(params) > 3 {
			reason = fmt.Sprintf("%s: %s", reason, strings.Join(params[3:], " "))
		}
		s.partAllChannels(rcl.identifier, reason)
		s.killClient(rcl)
		cl.sendMessage(fmt.Sprintf("Killed %s %s", params[0], params[1]))
	case COMMAND_STATS:

		cl.sendMessage(fmt.Sprintf("%d clients in %d channels", s.clientCount(), s.channelCount()))
	case COMMAND_REHASH:

		err := s.reload()
		if err != nil {
			cl.sendError(err.Error())
		} else {
			cl.sendMessage("Reloaded configuration")
		}
	case COMMAND_UPGRADE:
		// TODO
	}
}

func (s *Server) handlePrivmsg(channel string, client string, message string) {
	cl := s.getClient(client)
	if cl == nil {
		return
	}

	if strings.ToLower(channel) == "anonirc" {
		params := strings.Split(message, " ")
		if len(params) > 0 && len(params[0]) > 0 {
			var otherparams []string
			if len(params) > 1 {
				otherparams = params[1:]
			}

			s.handleUserCommand(client, params[0], otherparams)
		}

		return
	} else if channel == "" || !validChannelPrefix(channel) {
		return
	} else if !s.inChannel(channel, client) {
		return // Not in channel
	}

	ch := s.getChannel(channel)
	if ch == nil {
		return
	}

	s.updateClientCount(channel, "", "")

	ch.clients.Range(func(k, v interface{}) bool {
		chcl := s.getClient(k.(string))
		if chcl != nil && chcl.identifier != client {
			chcl.write(&irc.Message{&prefixAnonymous, irc.PRIVMSG, []string{channel, message}})
		}

		return true
	})
	ch.Log(cl, "CHAT", message)
}

func (s *Server) handleRead(c *Client) {
	for {
		if c.state == ENTITY_STATE_TERMINATING {
			return
		}

		c.conn.SetReadDeadline(time.Now().Add(300 * time.Second))

		if _, ok := s.clients.Load(c.identifier); !ok {
			s.killClient(c)
			return
		}

		msg, err := c.reader.Decode()
		if msg == nil || err != nil {
			// Error decoding message, client probably disconnected
			s.killClient(c)
			return
		}
		if debugMode && (verbose || (len(msg.Command) >= 4 && msg.Command[0:4] != irc.PING && msg.Command[0:4] != irc.PONG)) {
			log.Printf("%s -> %s", c.identifier, msg)
		}

		if msg.Command == irc.NICK && c.nick == "*" && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] != "" && msg.Params[0] != "*" {
			c.nick = strings.Trim(msg.Params[0], "\"")
		} else if msg.Command == irc.USER && c.user == "" && len(msg.Params) >= 3 && msg.Params[0] != "" && msg.Params[2] != "" {
			c.user = strings.Trim(msg.Params[0], "\"")
			c.host = strings.Trim(msg.Params[2], "\"")

			c.writeMessage(irc.RPL_WELCOME, []string{"Welcome to AnonIRC " + c.getPrefix().String()})
			c.writeMessage(irc.RPL_YOURHOST, []string{"Your host is AnonIRC, running version AnonIRCd https://github.com/sageru-6ch/anonircd"})
			c.writeMessage(irc.RPL_CREATED, []string{fmt.Sprintf("This server was created %s", time.Unix(s.created, 0).UTC())})
			c.writeMessage(strings.Join([]string{irc.RPL_MYINFO, c.nick, "AnonIRC", "AnonIRCd", CLIENT_MODES, CHANNEL_MODES, CHANNEL_MODES_ARG}, " "), []string{})

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
				c.writeMessage(motdcode, []string{"  " + motdmsg})
			}

			s.joinChannel(CHANNEL_LOBBY, c.identifier)
			if c.globalPermission() >= PERMISSION_VIP {
				s.joinChannel(CHANNEL_SERVER, c.identifier)
			}
		} else if msg.Command == irc.PASS && c.user == "" && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			// TODO: Add auth and multiple failed attempts ban
			authSuccess := false
			psplit := strings.SplitN(msg.Params[0], ":", 2)
			if len(psplit) == 2 {
				authSuccess = c.identify(psplit[0], psplit[1])
			}

			if !authSuccess {
				c.sendPasswordIncorrect()
				s.killClient(c)
			}
		} else if msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_LS {
			c.writeMessage(irc.CAP, []string{irc.CAP_LS, "userhost-in-names"})
		} else if msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_REQ {
			if strings.Contains(msg.Trailing(), "userhost-in-names") {
				c.capHostInNames = true
			}
			c.writeMessage(irc.CAP, []string{irc.CAP_ACK, msg.Trailing()})
		} else if msg.Command == irc.CAP && len(msg.Params) > 0 && len(msg.Params[0]) > 0 && msg.Params[0] == irc.CAP_LIST {
			caps := []string{}
			if c.capHostInNames {
				caps = append(caps, "userhost-in-names")
			}
			c.writeMessage(irc.CAP, []string{irc.CAP_LIST, strings.Join(caps, " ")})
		} else if msg.Command == irc.PING {
			c.writeMessage(irc.PONG+" AnonIRC", []string{msg.Trailing()})
		} else if c.user == "" {
			return // Client must send USER before issuing remaining commands
		} else if msg.Command == irc.WHOIS && len(msg.Params) > 0 && len(msg.Params[0]) >= len(prefixAnonymous.Name) && strings.ToLower(msg.Params[0][:len(prefixAnonymous.Name)]) == strings.ToLower(prefixAnonymous.Name) {
			go func() {
				whoisindex := 1
				if len(msg.Params[0]) > len(prefixAnonymous.Name) {
					whoisindex, err = strconv.Atoi(msg.Params[0][len(prefixAnonymous.Name):])
					if err != nil || whoisindex <= 1 {
						return
					}
				}

				whoisnick := prefixAnonymous.Name
				if whoisindex > 1 {
					whoisnick += strconv.Itoa(whoisindex)
				}

				easteregg := s.readOdyssey(whoisindex)
				if easteregg == "" {
					easteregg = "I am the owner of my actions, heir of my actions, actions are the womb (from which I have sprung), actions are my relations, actions are my protection. Whatever actions I do, good or bad, of these I shall become the heir."
				}

				c.writeMessage(irc.RPL_AWAY, []string{whoisnick, easteregg})
				c.writeMessage(irc.RPL_ENDOFWHOIS, []string{whoisnick, "End of /WHOIS list."})
			}()
		} else if msg.Command == irc.ISON {
			c.writeMessage(irc.RPL_ISON, []string{""})
		} else if msg.Command == irc.AWAY {
			if len(msg.Params) > 0 {
				c.writeMessage(irc.RPL_NOWAWAY, []string{"You have been marked as being away"})
			} else {
				c.writeMessage(irc.RPL_UNAWAY, []string{"You are no longer marked as being away"})
			}
		} else if msg.Command == irc.LIST {
			chans := make(map[string]int)
			s.channels.Range(func(k, v interface{}) bool {
				key := k.(string)
				ch := v.(*Channel)

				if key[0] == '&' && c.globalPermission() < PERMISSION_VIP {
					return true
				}

				if ch == nil || ch.hasMode("p") || ch.hasMode("s") {
					return true
				}

				chans[key] = s.clientsInChannel(key, c.identifier)
				return true
			})

			c.writeMessage(irc.RPL_LISTSTART, []string{"Channel", "Users Name"})
			for _, pl := range sortMapByValues(chans) {
				ch := s.getChannel(pl.Key)

				c.writeMessage(irc.RPL_LIST, []string{pl.Key, strconv.Itoa(pl.Value), "[" + ch.printModes(ch.getModes(), nil) + "] " + ch.topic})
			}
			c.writeMessage(irc.RPL_LISTEND, []string{"End of /LIST"})
		} else if msg.Command == irc.JOIN && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.joinChannel(channel, c.identifier)
			}
		} else if msg.Command == irc.NAMES && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.sendNames(channel, c.identifier)
			}
		} else if msg.Command == irc.WHO && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			var ccount int
			for _, channel := range strings.Split(msg.Params[0], ",") {
				if s.inChannel(channel, c.identifier) {
					ccount = s.clientsInChannel(channel, c.identifier)
					for i := 0; i < ccount; i++ {
						var prfx *irc.Prefix
						if i == 0 {
							prfx = c.getPrefix()
						} else {
							prfx = s.getAnonymousPrefix(i)
						}

						c.writeMessage(irc.RPL_WHOREPLY, []string{channel, prfx.User, prfx.Host, "AnonIRC", prfx.Name, "H", "0 " + prefixAnonymous.Name})
					}
					c.writeMessage(irc.RPL_ENDOFWHO, []string{channel, "End of /WHO list."})
				}
			}
		} else if msg.Command == irc.MODE {
			if len(msg.Params) == 2 && validChannelPrefix(msg.Params[0]) && msg.Params[1] == "b" {
				c.writeMessage(irc.RPL_ENDOFBANLIST, []string{msg.Params[0], "End of Channel Ban List"})
			} else {
				s.handleMode(c, msg.Params)
			}
		} else if msg.Command == irc.TOPIC && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			if len(msg.Params) == 1 {
				s.sendTopic(msg.Params[0], c.identifier, false)
			} else {
				s.handleTopic(msg.Params[0], c.identifier, strings.Join(msg.Params[1:], " "))
			}
		} else if msg.Command == irc.PRIVMSG && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			s.handlePrivmsg(msg.Params[0], c.identifier, msg.Trailing())
		} else if msg.Command == irc.PART && len(msg.Params) > 0 && len(msg.Params[0]) > 0 {
			for _, channel := range strings.Split(msg.Params[0], ",") {
				s.partChannel(channel, c.identifier, "")
			}
		} else if msg.Command == irc.QUIT {
			s.killClient(c)
		} else {
			s.handleUserCommand(c.identifier, msg.Command, msg.Params)
		}
	}
}

func (s *Server) handleWrite(c *Client) {
	for {
		select {
		case msg := <-c.writebuffer:
			if c.state == ENTITY_STATE_TERMINATING {
				continue
			}

			c.wg.Add(1)
			addnick := false
			if _, err := strconv.Atoi(msg.Command); err == nil {
				addnick = true
			} else if msg.Command == irc.CAP {
				addnick = true
			}

			if addnick {
				msg.Params = append([]string{c.nick}, msg.Params...)
			}

			if debugMode && (verbose || (len(msg.Command) >= 4 && msg.Command[0:4] != irc.PING && msg.Command[0:4] != irc.PONG)) {
				log.Printf("%s <- %s", c.identifier, msg)
			}
			c.writer.Encode(msg)
			c.wg.Done()
		case <-c.terminate:
			close(c.writebuffer)
			return
		}
	}
}

func (s *Server) handleConnection(conn net.Conn, ssl bool) {
	defer conn.Close()
	var identifier string

	for {
		identifier = randomIdentifier()
		if _, ok := s.clients.Load(identifier); !ok {
			break
		}
	}

	client := NewClient(identifier, conn, ssl)
	banned := true
	reason := ""
	if client != nil {
		banned, reason = client.isBanned("")
	}
	if banned {
		// TODO: Send banned message
		_ = reason
		return // Banned
	}
	s.clients.Store(client.identifier, client)

	go s.handleWrite(client)
	s.handleRead(client) // Block until the connection is closed

	s.killClient(client)
}

func (s *Server) killClient(c *Client) {
	if c == nil || c.state == ENTITY_STATE_TERMINATING {
		return
	}
	c.state = ENTITY_STATE_TERMINATING

	select {
	case c.terminate <- true:
		if _, ok := s.clients.Load(c.identifier); ok {
			s.partAllChannels(c.identifier, "")
		}
		c.wg.Wait()
	default:
	}
}

func (s *Server) listenPlain() {
	for {
		listen, err := net.Listen("tcp", ":6667")
		if err != nil {
			log.Printf("Failed to listen: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}
		log.Println("Listening on 6667")

	accept:
		for {
			select {
			case <-s.restartplain:
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
			log.Printf("Failed to load SSL certificate: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}

		listen, err := tls.Listen("tcp", ":6697", &tls.Config{Certificates: []tls.Certificate{cert}})
		if err != nil {
			log.Printf("Failed to listen: %v", err)
			time.Sleep(1 * time.Minute)
			continue
		}
		log.Println("Listening on +6697")

	accept:
		for {
			select {
			case <-s.restartssl:
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
		s.clients.Range(func(k, v interface{}) bool {
			cl := v.(*Client)
			if cl != nil {
				cl.write(&irc.Message{nil, irc.PING, []string{fmt.Sprintf("anonirc%d%d", int32(time.Now().Unix()), rand.Intn(1000))}})
			}

			return true
		})
		time.Sleep(90 * time.Second)
	}
}

func (s *Server) connectDatabase() {
	err := db.Connect(s.config.DBDriver, s.config.DBSource)
	if err != nil {
		log.Panicf("%+v", err)
	}
}

func (s *Server) closeDatabase() {
	err := db.Close()
	if err != nil {
		log.Panicf("%+v", err)
	}
}

func (s *Server) loadConfig() error {
	if s.configfile == "" {
		return errors.New("configuration file must be specified:  anonircd -c /home/user/anonircd/anonircd.conf")
	}

	if _, err := os.Stat(s.configfile); err != nil {
		return errors.New("unable to find configuration file " + s.configfile)
	}

	oldconfig := &*s.config

	if _, err := toml.DecodeFile(s.configfile, &s.config); err != nil {
		if oldconfig != nil {
			s.config = oldconfig
		}

		return errors.New(fmt.Sprintf("Failed to read configuration file %s: %v", s.configfile, err))
	}

	if s.config.DBDriver == "" || s.config.DBSource == "" {
		if oldconfig != nil {
			s.config = oldconfig
		}

		return errors.New(fmt.Sprintf("DBDriver and DBSource must be configured in %s\nExample:\n\nDBDriver=\"sqlite3\"\nDBSource=\"/home/user/anonircd/anonircd.db\"", s.configfile))
	}

	return nil
}

func (s *Server) reload() error {
	log.Println("Reloading configuration...")

	err := s.loadConfig()
	if err != nil {
		log.Println("Failed to reload configuration")
		return errors.Wrap(err, "failed to reload configuration")
	}
	log.Println("Reloaded configuration")

	s.restartplain <- true
	s.restartssl <- true

	return nil
}

func (s *Server) readOdyssey(line int) string {
	s.odysseymutex.Lock()
	defer s.odysseymutex.Unlock()

	scanner := bufio.NewScanner(s.odyssey)
	currentline := 1
	for scanner.Scan() {
		if currentline == line {
			s.odyssey.Seek(0, 0)
			return scanner.Text()
		}

		currentline++
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Failed to read ODYSSEY: %v", err)
		return ""
	}

	s.odyssey.Seek(0, 0)
	return ""
}

func (s *Server) listen() {
	go s.listenPlain()
	go s.listenSSL()

	s.pingClients()
}
