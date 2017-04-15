package main

import "github.com/orcaman/concurrent-map"

type Channel struct {
	Entity

	clients cmap.ConcurrentMap

	topic     string
	topictime int64
}
