package main

import (
	"net"

	"sync"

	irc "gopkg.in/sorcix/irc.v2"
)

type Client struct {
	Entity
	ip string

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

	c.ip = generateHash(ip)
	// TODO: Check bans, return nil

	c.ssl = ssl
	c.nick = "*"
	c.conn = conn
	c.writebuffer = make(chan *irc.Message, writebuffersize)
	c.terminate = make(chan bool)
	c.reader = irc.NewDecoder(conn)
	c.writer = irc.NewEncoder(conn)

	return c
}

func (c *Client) registered() bool {
	// TODO
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

func (c *Client) accessDenied() {
	c.sendNotice("Access denied")
}
