package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
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

func formatAction(action string, reason string) string {
	rs := action
	if reason != "" {
		rs += ": " + reason
	}

	return rs
}

func parseDuration(duration string) int64 {
	duration = strings.TrimSpace(duration)
	if intval, err := strconv.Atoi(duration); err == nil {
		if intval == 0 {
			return 0 // Never expire
		}
	}

	if len(duration) < 2 {
		return -1 // Value and unit are required
	}

	sv := duration[0 : len(duration)-1]
	unit := strings.ToLower(duration[len(duration)-1:])

	value, err := strconv.ParseInt(sv, 10, 64)
	if err != nil || value < 0 {
		return -1
	}

	switch unit {
	case "y":
		return value * 3600 * 24 * 365
	case "w":
		return value * 3600 * 24 * 7
	case "d":
		return value * 3600 * 24
	case "h":
		return value * 3600
	case "m":
		return value * 60
	case "s":
		return value
	}

	return -1
}
