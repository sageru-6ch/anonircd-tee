package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/orcaman/concurrent-map"
)

const ENTITY_CLIENT = 0
const ENTITY_CHANNEL = 1

const ENTITY_STATE_TERMINATING = 0
const ENTITY_STATE_NORMAL = 1

const CLIENT_MODES = "cD"
const CHANNEL_MODES = "cDipstz"
const CHANNEL_MODES_ARG = "kl"

type Entity struct {
	entitytype int
	identifier string
	created    int64
	state      int
	modes      cmap.ConcurrentMap

	*sync.RWMutex
}

func (e *Entity) getModes() map[string]string {
	modes := make(map[string]string)
	for ms := range e.modes.IterBuffered() {
		modes[ms.Key] = ms.Val.(string)
	}
	return modes
}

func (e *Entity) hasMode(mode string) bool {
	return e.modes.Has(mode)
}

func (e *Entity) addMode(mode string, value string) {
	var allowedmodes string
	if e.entitytype == ENTITY_CLIENT {
		allowedmodes = CLIENT_MODES
	} else {
		allowedmodes = CHANNEL_MODES
	}

	if strings.Index(allowedmodes, mode) != -1 && !e.hasMode(mode) {
		e.modes.Set(mode, value)
	}
}

func (e *Entity) addModes(modes string) {
	for _, mode := range strings.Split(modes, "") {
		e.addMode(fmt.Sprintf("%s", mode), "")
	}
}

func (e *Entity) removeMode(mode string) {
	if e.hasMode(mode) {
		e.modes.Remove(mode)
	}
}

func (e *Entity) removeModes(modes string) {
	for _, mode := range strings.Split(modes, "") {
		e.removeMode(fmt.Sprintf("%s", mode))
	}
}

func (e *Entity) diffModes(lastmodes map[string]string) (map[string]string, map[string]string) {
	addedmodes := make(map[string]string)
	if lastmodes != nil {
		for m := range e.modes.IterBuffered() {
			if _, ok := lastmodes[m.Key]; !ok {
				addedmodes[m.Key] = lastmodes[m.Key]
			}
		}
	}

	removedmodes := make(map[string]string)
	for mode := range lastmodes {
		if e.hasMode(mode) {
			m, _ := e.modes.Get(mode)
			removedmodes[mode] = m.(string)
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
