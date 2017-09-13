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
