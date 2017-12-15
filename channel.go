package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/sorcix/irc.v2"
)

type Channel struct {
	Entity

	clients *sync.Map
	logs    map[int64]*ChannelLog

	topic     string
	topictime int64

	sync.RWMutex
}

type ChannelLog struct {
	Timestamp int64
	Client    string
	IP        string
	Account   int64
	Action    string
	Message   string
}

const CHANNEL_LOGS_PER_PAGE = 25

func (cl *ChannelLog) Identifier(index int) string {
	return fmt.Sprintf("%03d%02d", index, cl.Timestamp%100)
}

func (cl *ChannelLog) Print(index int, channel string) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s %4s %s", time.Unix(0, cl.Timestamp).Format(time.Stamp), channel, cl.Identifier(index), cl.Action, cl.Message))
}

func NewChannel(identifier string) *Channel {
	c := &Channel{}
	c.Initialize(ENTITY_CHANNEL, identifier)

	c.clients = new(sync.Map)
	c.logs = make(map[int64]*ChannelLog)

	return c
}

func (c *Channel) Log(client *Client, action string, message string) {
	c.Lock()
	defer c.Unlock()

	// TODO: Log size limiting, max capacity will be 999 entries
	// Log hash of IP address which is used later when connecting/joining

	nano := time.Now().UTC().UnixNano()
	c.logs[nano] = &ChannelLog{Timestamp: nano, Client: client.identifier, IP: client.iphash, Account: client.account, Action: action, Message: message}
}

func (c *Channel) RevealLog(page int, showAll bool) []string {
	c.RLock()
	defer c.RUnlock()

	// TODO:
	// Trim old channel logs periodically
	// Add pagination
	var ls []string
	logsRemain := false
	j := 0

	var nanos int64arr
	for n := range c.logs {
		nanos = append(nanos, n)
	}
	sort.Sort(nanos)

	// To perform the opertion you want
	var l *ChannelLog
	var ok bool
	for i, nano := range nanos {
		if l, ok = c.logs[nano]; !ok {
			continue
		}

		if page == -1 || i >= (CHANNEL_LOGS_PER_PAGE*(page-1)) {
			if showAll || (l.Action != irc.JOIN && l.Action != irc.PART) {
				if page > -1 && j == CHANNEL_LOGS_PER_PAGE {
					logsRemain = true
					break
				}
				ls = append(ls, l.Print(i, c.identifier))
				j++
			}
		}
	}

	if len(ls) == 0 {
		ls = append(ls, "No log entries match criteria")
	} else {
		filterType := "all entries"
		if page > -1 {
			filterType = fmt.Sprintf("page %d", page)
		}
		ls = append([]string{fmt.Sprintf("Revealing %s (%s)", c.identifier, filterType)}, ls...)

		finishedMessage := fmt.Sprintf("Finished revealing %s", c.identifier)
		if logsRemain {
			finishedMessage = fmt.Sprintf("Additional log entries on page %d", page+1)
		}
		ls = append(ls, finishedMessage)
	}

	return ls
}

func (c *Channel) RevealInfo(identifier string) (string, int64) {
	if len(identifier) != 5 {
		return "", 0
	}

	c.RLock()
	defer c.RUnlock()

	var nanos int64arr
	for n := range c.logs {
		nanos = append(nanos, n)
	}
	sort.Sort(nanos)

	var l *ChannelLog
	var ok bool
	for i, nano := range nanos {
		if l, ok = c.logs[nano]; !ok {
			continue
		} else if l.Identifier(i) == identifier {
			return l.IP, l.Account
		}
	}

	return "", 0
}

func (c *Channel) HasClient(client string) bool {
	_, ok := c.clients.Load(client)
	return ok
}
