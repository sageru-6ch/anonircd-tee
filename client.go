package main

import (
	"log"
	"net"
	"strconv"

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

func (c *Client) handleWrite() {
	for msg := range c.writebuffer {
		if msg == nil {
			return
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

		if len(msg.Command) >= 4 && msg.Command[0:4] != irc.PING && msg.Command[0:4] != irc.PONG {
			log.Println(c.identifier, "->", msg)
		}
		c.writer.Encode(msg)
	}
}

func (c *Client) sendNotice(notice string) {
	c.write(&irc.Message{&anonirc, irc.NOTICE, []string{c.nick, "*** " + notice}})
}
