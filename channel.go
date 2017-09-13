package main

import (
	"sync"
)

type Channel struct {
	Entity

	clients *sync.Map

	topic     string
	topictime int64
}

func NewChannel(identifier string) *Channel {
	c := &Channel{}
	c.Initialize(ENTITY_CHANNEL, identifier)

	c.clients = new(sync.Map)

	return c
}
