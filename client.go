package main

import (
	"net"

	irc "gopkg.in/sorcix/irc.v2"
)

type Client struct {
	Entity

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

func (c *Client) getPrefix() *irc.Prefix {
	return &irc.Prefix{Name: c.nick, User: c.user, Host: c.host}
}

func (c *Client) write(msg *irc.Message) {
	c.writebuffer <- msg
}

func (c *Client) sendNotice(notice string) {
	c.write(&irc.Message{&anonirc, irc.NOTICE, []string{c.nick, "*** " + notice}})
}
