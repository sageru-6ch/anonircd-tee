package main

import (
	"encoding/base64"
	"math/rand"
	"sort"
	"strings"

	"crypto/md5"

	"fmt"

	"golang.org/x/crypto/sha3"
)

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

func validChannelPrefix(channel string) bool {
	return channel[0] == '&' || channel[0] == '#'
}

func generateHash(s string) string {
	sha512 := sha3.New512()
	_, err := sha512.Write([]byte(strings.Join([]string{s, fmt.Sprintf("%x", md5.Sum([]byte(s))), s}, "-")))
	if err != nil {
		return ""
	}

	return base64.URLEncoding.EncodeToString(sha512.Sum(nil))
}
