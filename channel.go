package main

import (
	"sync"
	"fmt"
	"strings"
)

type Channel struct {
	created   int64

	clients   map[string]int
	modes     map[string]string

	topic     string
	topictime int64

	*sync.RWMutex
}

func (c *Channel) hasMode(mode string) bool {
	if _, ok := c.modes[mode]; ok {
		return true
	}

	return false
}

func (c *Channel) addMode(mode string, param string) {
	if strings.Index(cmodes, mode) != -1 && !c.hasMode(mode) {
		c.modes[mode] = param
	}
}

func (c *Channel) addModes(modes string) {
	for _, mode := range strings.Split(modes, "") {
		c.addMode(fmt.Sprintf("%s", mode), "")
	}
}

func (c *Channel) removeMode(mode string) {
	if c.hasMode(mode) {
		delete(c.modes, mode)
	}
}

func (c *Channel) printModes(lastmodes map[string]string) string {
	modes := ""
	for mode := range c.modes {
		modes += mode
	}
	return "+" + modes
}
