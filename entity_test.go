package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEntityMode(t *testing.T) {
	channel := NewChannel("#channel")

	channel.addModes([]string{"pk", "MyAwesomeChannelKey"})

	modes := channel.getModes()
	assert.Equal(t, map[string]string{"k": "MyAwesomeChannelKey", "p": ""}, modes)

	assert.Equal(t, "+kp", channel.printModes(modes, nil))

	client := NewClient("client", nil, false)

	client.addModes([]string{"ck", "MyAwesomeChannelKey"}) // +k is not a client mode

	modes = client.getModes()
	assert.Equal(t, map[string]string{"c": ""}, modes)

	assert.Equal(t, "+c", client.printModes(modes, nil))
}
