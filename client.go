package main

import (
	"net"

	irc "gopkg.in/sorcix/irc.v2"
)

type Client struct {
	Entity
	ip string

	ssl  bool
	nick string
	user string
	host string

	conn        net.Conn
	writebuffer chan *irc.Message

	reader *irc.Decoder
	writer *irc.Encoder

	capHostInNames bool
}

func NewClient(identifier string, conn net.Conn, ssl bool) *Client {
	c := &Client{}
	c.ip = conn.RemoteAddr().String()
	c.Initialize(ENTITY_CLIENT, identifier)

	c.ssl = ssl
	c.nick = "*"
	c.conn = conn
	c.writebuffer = make(chan *irc.Message, writebuffersize)
	c.reader = irc.NewDecoder(conn)
	c.writer = irc.NewEncoder(conn)

	return c
}

func (c *Client) getPrefix() *irc.Prefix {
	return &irc.Prefix{Name: c.nick, User: c.user, Host: c.host}
}

func (c *Client) write(msg *irc.Message) {
	c.writebuffer <- msg
}

func (c *Client) sendNotice(notice string) {
	c.write(&irc.Message{&anonirc, irc.NOTICE, []string{c.nick, "*** " + notice}})
}
