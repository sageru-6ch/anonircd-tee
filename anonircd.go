// AnonIRCd
// https://github.com/tslocum/anonircd
//
// Written by Trevor 'tee' Slocum <tslocum@gmail.com>
// Inspired by a Java implementation written by Daniel da Silva 'meltingwax'
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.


package main

import (
	"sync"
	"math/rand"
	"time"

	irc "gopkg.in/sorcix/irc.v2"
)

var anonymous = irc.Prefix{"Anonymous", "Anon", "IRC"}
var anonirc = irc.Prefix{Name:"AnonIRC"}

const motd = `
  _|_|                                  _|_|_|  _|_|_|      _|_|_|
_|    _|  _|_|_|      _|_|    _|_|_|      _|    _|    _|  _|
_|_|_|_|  _|    _|  _|    _|  _|    _|    _|    _|_|_|    _|
_|    _|  _|    _|  _|    _|  _|    _|    _|    _|    _|  _|
_|    _|  _|    _|    _|_|    _|    _|  _|_|_|  _|    _|    _|_|_|
`
const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const umodes = "a"
const cmodes = "it"
const cmodesarg = "kl"

func randomIdentifier() string {
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	server := Server{time.Now().Unix(), make(map[string]*Client), make(map[string]*Channel), new(sync.RWMutex)}
	server.listen()
}
