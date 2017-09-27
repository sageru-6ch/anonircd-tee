// AnonIRCd - Anonymous IRC daemon
// https://gitlab.com/tslocum/anonircd
// Written by Trevor 'tee' Slocum <tslocum@gmail.com>
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
	"log"
	"math/rand"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	irc "gopkg.in/sorcix/irc.v2"
)

var anonymous = irc.Prefix{"Anonymous", "Anon", "IRC"}
var anonirc = irc.Prefix{Name: "AnonIRC"}

const motd = `
  _|_|                                  _|_|_|  _|_|_|      _|_|_|
_|    _|  _|_|_|      _|_|    _|_|_|      _|    _|    _|  _|
_|_|_|_|  _|    _|  _|    _|  _|    _|    _|    _|_|_|    _|
_|    _|  _|    _|  _|    _|  _|    _|    _|    _|    _|  _|
_|    _|  _|    _|    _|_|    _|    _|  _|_|_|  _|    _|    _|_|_|
`
const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const writebuffersize = 10

type Pair struct {
	Key   string
	Value int
}

type PairList []Pair

func (p PairList) Len() int {
	return len(p)
}
func (p PairList) Less(i, j int) bool {
	return p[i].Value < p[j].Value
}
func (p PairList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func sortMapByValues(m map[string]int) PairList {
	pl := make(PairList, len(m))
	i := 0
	for k, v := range m {
		pl[i] = Pair{k, v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	return pl
}

func randomIdentifier() string {
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	server := NewServer()
	server.loadConfig()

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup,
		syscall.SIGHUP)
	go func() {
		<-sighup
		server.reload()
	}()

	var err error
	server.odyssey, err = os.Open("ODYSSEY")
	if err != nil {
		log.Fatal(err)
	}
	defer server.odyssey.Close()

	go server.startProfiling()
	server.listen()
}
