package main

import (
	"log"
	"net"

	"sync"

	"strings"

	"fmt"

	irc "gopkg.in/sorcix/irc.v2"
)

type Client struct {
	Entity
	iphash string

	ssl     bool
	nick    string
	user    string
	host    string
	account int

	conn        net.Conn
	writebuffer chan *irc.Message
	terminate   chan bool

	reader *irc.Decoder
	writer *irc.Encoder

	capHostInNames bool

	wg sync.WaitGroup
}

func NewClient(identifier string, conn net.Conn, ssl bool) *Client {
	c := &Client{}
	c.Initialize(ENTITY_CLIENT, identifier)

	ip, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil
	}

	c.iphash = generateHash(ip)
	c.ssl = ssl
	c.nick = "*"
	c.conn = conn
	c.writebuffer = make(chan *irc.Message, writebuffersize)
	c.terminate = make(chan bool)
	c.reader = irc.NewDecoder(conn)
	c.writer = irc.NewEncoder(conn)

	return c
}

func (c *Client) getAccount() (*DBAccount, error) {
	if c.account == 0 {
		return nil, nil
	}

	acc, err := db.Account(c.account)
	if err != nil {
		return nil, err
	}

	return &acc, nil
}

func (c *Client) registered() bool {
	// TODO get account and check if it is valid
	return c.account > 0
}

func (c *Client) getPrefix() *irc.Prefix {
	return &irc.Prefix{Name: c.nick, User: c.user, Host: c.host}
}

func (c *Client) write(msg *irc.Message) {
	if c.state == ENTITY_STATE_TERMINATING {
		return
	}

	c.writebuffer <- msg
}

func (c *Client) writeMessage(command string, params []string) {
	c.write(&irc.Message{&prefixAnonIRC, command, params})
}

func (c *Client) sendMessage(message string) {
	c.writeMessage(irc.PRIVMSG, []string{c.nick, message})
}

func (c *Client) sendPasswordIncorrect() {
	c.writeMessage(irc.ERR_PASSWDMISMATCH, []string{"Password incorrect"})
}

func (c *Client) sendError(message string) {
	c.sendMessage("Error! " + message)
}

func (c *Client) sendNotice(message string) {
	c.sendMessage("*** " + message)
}

func (c *Client) accessDenied(permissionRequired int) {
	ex := ""
	if permissionRequired > PERMISSION_CLIENT {
		ex = fmt.Sprintf(", that command is available to %ss only", strings.ToLower(permissionLabels[permissionRequired]))
		if permissionRequired == PERMISSION_REGISTERED {
			ex += " - Reply HELP for more info (see REGISTER and IDENTIFY)"
		}
	}

	c.sendNotice("Access denied" + ex)
}

func (c *Client) identify(username string, password string) bool {
	accountid, err := db.Auth(username, password)
	if err != nil {
		log.Panicf("%+v", err)
	}

	account, err := db.Account(accountid)
	if err != nil {
		log.Panicf("%+v", err)
	} else if account.ID == 0 {
		return false
	}

	c.account = accountid
	return true
}

func (c *Client) getPermission(channel string) int {
	if c.account == 0 {
		return PERMISSION_CLIENT
	}

	p, err := db.GetPermission(c.account, channel)
	if err != nil {
		log.Panicf("%+v", err)
	}

	return p.Permission
}

func (c *Client) globalPermission() int {
	return c.getPermission("&")
}

func (c *Client) canUse(command string, channel string) bool {
	command = strings.ToUpper(command)
	req := c.permissionRequired(command)

	globalPermission := c.globalPermission()
	if globalPermission >= req {
		return true
	} else if containsString(serverCommands, command) {
		return false
	}

	return c.getPermission(channel) >= req
}

func (c *Client) permissionRequired(command string) int {
	command = strings.ToUpper(command)
	for permissionRequired, commands := range commandRestrictions {
		for _, cmd := range commands {
			if cmd == command {
				return permissionRequired
			}
		}
	}

	return 0
}

func (c *Client) isBanned(channel string) (bool, string) {
	b, err := db.BanAddr(c.iphash, channel)
	if err != nil {
		log.Panicf("%+v", err)
	}

	if b.Channel == "" && c.account > 0 {
		b, err = db.BanAccount(c.account, channel)
		if err != nil {
			log.Panicf("%+v", err)
		}
	}

	if b.Channel != "" {
		return true, b.Reason
	}

	return false, ""
}
