package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"golang.org/x/crypto/sha3"
)

type Pair struct {
	Key   string
	Value int
}

type PairList []Pair

type int64arr []int64

func (a int64arr) Len() int           { return len(a) }
func (a int64arr) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a int64arr) Less(i, j int) bool { return a[i] < a[j] }

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

func containsString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// Problem
func p(err error) bool {
	return err != nil && err != sql.ErrNoRows
}
