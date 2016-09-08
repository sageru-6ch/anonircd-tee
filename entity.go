package main

import (
	"sync"
	"strings"
	"fmt"
)

const ENTITY_CLIENT = 0
const ENTITY_CHANNEL = 1

const CLIENT_MODES = "c"
const CHANNEL_MODES = "cipstz"
const CHANNEL_MODES_ARG = "kl"

type Entity struct {
	entitytype int
	identifier string
	created    int64
	modes      map[string]string

	*sync.RWMutex
}

func (e *Entity) hasMode(mode string) bool {
	if _, ok := e.modes[mode]; ok {
		return true
	}

	return false
}

func (e *Entity) addMode(mode string, value string) {
	var allowedmodes string
	if e.entitytype == ENTITY_CLIENT {
		allowedmodes = CLIENT_MODES
	} else {
		allowedmodes = CHANNEL_MODES
	}

	if strings.Index(allowedmodes, mode) != -1 && !e.hasMode(mode) {
		e.modes[mode] = value
	}
}

func (e *Entity) addModes(modes string) {
	for _, mode := range strings.Split(modes, "") {
		e.addMode(fmt.Sprintf("%s", mode), "")
	}
}

func (e *Entity) removeMode(mode string) {
	if e.hasMode(mode) {
		delete(e.modes, mode)
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
		for mode := range e.modes {
			if _, ok := lastmodes[mode]; !ok {
				addedmodes[mode] = lastmodes[mode]
			}
		}
	}

	removedmodes := make(map[string]string)
	for mode := range lastmodes {
		if _, ok := e.modes[mode]; !ok {
			removedmodes[mode] = e.modes[mode]
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
