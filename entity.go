package main

import (
	"strings"
	"sync"
	"time"
)

const ENTITY_CLIENT = 0
const ENTITY_CHANNEL = 1

const ENTITY_STATE_TERMINATING = 0
const ENTITY_STATE_NORMAL = 1

const CLIENT_MODES = "cD"
const CHANNEL_MODES = "cDimprstz"
const CHANNEL_MODES_ARG = "kl"

type Entity struct {
	entitytype int
	identifier string
	created    int64
	state      int
	modes      *sync.Map
}

func (e *Entity) Initialize(etype int, identifier string) {
	e.identifier = identifier
	e.entitytype = etype
	e.created = time.Now().Unix()
	e.state = ENTITY_STATE_NORMAL
	e.modes = new(sync.Map)
}

func (e *Entity) getModes() map[string]string {
	modes := make(map[string]string)
	e.modes.Range(func(k, v interface{}) bool {
		modes[k.(string)] = v.(string)

		return true
	})

	return modes
}

func (e *Entity) hasMode(mode string) bool {
	_, ok := e.modes.Load(mode)

	return ok
}

func (e *Entity) addMode(mode string, value string) {
	var allowedmodes string
	if e.entitytype == ENTITY_CLIENT {
		allowedmodes = CLIENT_MODES
	} else {
		allowedmodes = CHANNEL_MODES
	}

	if strings.Contains(allowedmodes, mode) && !e.hasMode(mode) {
		e.modes.Store(mode, value)
	}
}

func (e *Entity) addModes(modes string) {
	for _, mode := range strings.Split(modes, "") {
		e.addMode(mode, "")
	}
}

func (e *Entity) removeMode(mode string) {
	if e.hasMode(mode) {
		e.modes.Delete(mode)
	}
}

func (e *Entity) removeModes(modes string) {
	for _, mode := range strings.Split(modes, "") {
		e.removeMode(mode)
	}
}

func (e *Entity) diffModes(lastmodes map[string]string) (map[string]string, map[string]string) {
	addedmodes := make(map[string]string)
	if lastmodes != nil {
		e.modes.Range(func(k, v interface{}) bool {
			if _, ok := lastmodes[k.(string)]; !ok {
				addedmodes[k.(string)] = lastmodes[k.(string)]
			}

			return true
		})
	}

	removedmodes := make(map[string]string)
	for mode := range lastmodes {
		if _, ok := e.modes.Load(mode); !ok {
			removedmodes[mode] = mode
		}
	}

	return addedmodes, removedmodes
}

func (e *Entity) printModes(addedmodes map[string]string, removedmodes map[string]string) string {
	if removedmodes == nil {
		removedmodes = make(map[string]string)
	}

	m := ""

	// Added modes
	sentsign := false
	for mode := range addedmodes {
		if !sentsign {
			m += "+"
			sentsign = true
		}
		m += mode
	}

	// Removed modes
	sentsign = false
	for mode := range removedmodes {
		if !sentsign {
			m += "-"
			sentsign = true
		}
		m += mode
	}

	if m == "" {
		m = "+"
	}

	return m
}
