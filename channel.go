package main

type Channel struct {
	Entity

	clients   map[string]int

	topic     string
	topictime int64
}
