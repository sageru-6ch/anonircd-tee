package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Channel struct {
	Entity

	clients *sync.Map
	logs    *ConcurrentSlice

	topic     string
	topictime int64
}

type ChannelLog struct {
	Timestamp int64
	Client    string
	IP        string
	Action    string
	Message   string
}

func (cl *ChannelLog) String() string {
	return strings.TrimSpace(fmt.Sprintf("%d [%s] %s %s %s", cl.Timestamp, time.Unix(0, cl.Timestamp).Format(time.Stamp), cl.Client, cl.Action, cl.Message))
}

func NewChannel(identifier string) *Channel {
	c := &Channel{}
	c.Initialize(ENTITY_CHANNEL, identifier)
	c.logs = NewConcurrentSlice()

	c.clients = new(sync.Map)

	return c
}

func (c *Channel) Log(client *Client, action string, message string) {
	// TODO: client identifier is hashed to be unique per channel
	c.logs.Append(&ChannelLog{Timestamp: time.Now().UTC().UnixNano(), Client: client.identifier, IP: client.ip, Action: action, Message: message})
}

func (c *Channel) Reveal(duration string, filtertime int64) []string {
	// TODO: duration and client limiting (via nanosecond)
	// Trim old channel logs periodically
	var ls []string
	for i := range c.logs.Iter() {
		l := i.Value.(*ChannelLog)
		ls = append(ls, l.String())
	}

	if len(ls) == 0 {
		ls = append(ls, "No matching logs were returned")
	}

	return ls
}
