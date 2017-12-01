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
	logs    []*ChannelLog

	topic     string
	topictime int64

	sync.RWMutex
}

type ChannelLog struct {
	Timestamp int64
	Client    string
	IP        string
	Action    string
	Message   string
}

const CHANNEL_LOGS_PER_PAGE = 25

func (cl *ChannelLog) Identifier(index int) string {
	return fmt.Sprintf("%03d%02d", index+1, cl.Timestamp%100)
}

func (cl *ChannelLog) Print(index int, channel string) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %5s %4s %s", time.Unix(0, cl.Timestamp).Format(time.Stamp), channel, cl.Identifier(index), cl.Action, cl.Message))
}

func NewChannel(identifier string) *Channel {
	c := &Channel{}
	c.Initialize(ENTITY_CHANNEL, identifier)

	c.clients = new(sync.Map)

	return c
}

func (c *Channel) Log(client *Client, action string, message string) {
	c.Lock()
	defer c.Unlock()

	// TODO: Log size limiting, max capacity will be 998 entries

	c.logs = append(c.logs, &ChannelLog{Timestamp: time.Now().UTC().UnixNano(), Client: client.identifier, IP: client.ip, Action: action, Message: message})
}

func (c *Channel) Reveal(page int) []string {
	c.RLock()
	defer c.RUnlock()

	// TODO:
	// Trim old channel logs periodically
	// Add pagination
	var ls []string
	logsRemain := false
	j := 0
	for i, l := range c.logs {
		if page == -1 || i >= (CHANNEL_LOGS_PER_PAGE*(page-1)) {
			if page > -1 && j == CHANNEL_LOGS_PER_PAGE {
				logsRemain = true
				break
			}
			ls = append(ls, l.Print(i, c.identifier))
			j++
		}
	}

	if len(ls) == 0 {
		ls = append(ls, "No matching logs were returned")
	} else {
		filterType := "all"
		if page > -1 {
			filterType = fmt.Sprintf("page %d", page)
		}
		ls = append([]string{fmt.Sprintf("Revealing %s (%s)", c.identifier, filterType)}, ls...)

		finishedMessage := fmt.Sprintf("Finished revealing %s", c.identifier)
		if logsRemain {
			finishedMessage = fmt.Sprintf("Additional logs on page %d", page+1)
		}
		ls = append(ls, finishedMessage)
	}

	return ls
}

func (c *Channel) HasClient(client string) bool {
	_, ok := c.clients.Load(client)
	return ok
}
