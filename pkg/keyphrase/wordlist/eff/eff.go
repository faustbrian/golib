// Package eff embeds the Electronic Frontier Foundation diceware word lists
// with pinned source and integrity metadata.
package eff

import (
	_ "embed"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

const sourceBase = "https://www.eff.org/files/2016"

// List is an alias for an immutable validated word list.
type List = wordlist.List

//go:embed data/large.txt
var largeData string

//go:embed data/short_1.txt
var shortOneData string

//go:embed data/short_2.txt
var shortTwoData string

var loadLarge = sync.OnceValues(func() (*wordlist.List, error) {
	return load(wordlist.Metadata{
		ID:            "eff-large",
		Language:      "en",
		Source:        sourceBase + "/07/18/eff_large_wordlist.txt",
		Version:       "2016-07-18",
		License:       "CC-BY-3.0-US",
		ExpectedWords: 7776,
		SHA256:        "6d557f0693958fb5e650b68b5bee585eb82cf4da32965505c789e924743bc522",
		SourceSHA256:  "addd35536511597a02fa0a9ff1e5284677b8883b83e986e43f15a3db996b903e",
	}, largeData)
})

var loadShortOne = sync.OnceValues(func() (*wordlist.List, error) {
	return load(wordlist.Metadata{
		ID:            "eff-short-1",
		Language:      "en",
		Source:        sourceBase + "/09/08/eff_short_wordlist_1.txt",
		Version:       "2016-09-08",
		License:       "CC-BY-3.0-US",
		ExpectedWords: 1296,
		SHA256:        "36ecca49e4fa20ca84b176c32f2e9c82f98f446585190e75f9879a95c08247bf",
		SourceSHA256:  "8f5ca830b8bffb6fe39c9736c024a00a6a6411adb3f83a9be8bfeeb6e067ae69",
	}, shortOneData)
})

var loadShortTwo = sync.OnceValues(func() (*wordlist.List, error) {
	return load(wordlist.Metadata{
		ID:            "eff-short-2",
		Language:      "en",
		Source:        sourceBase + "/09/08/eff_short_wordlist_2_0.txt",
		Version:       "2016-09-08",
		License:       "CC-BY-3.0-US",
		ExpectedWords: 1296,
		SHA256:        "7aa57a4d3ecf6581729992bad9575bacdebf7c28378af2aec6a50f11aec326f5",
		SourceSHA256:  "22b45c52e0bd0bbf03aa522240b111eb4c7c0c1d86c4e518e1be2a7eb2a625e4",
	}, shortTwoData)
})

// Large returns the validated 7,776-word EFF long list.
func Large() (*wordlist.List, error) {
	return loadLarge()
}

// ShortOne returns the validated first 1,296-word EFF short list.
func ShortOne() (*wordlist.List, error) {
	return loadShortOne()
}

// ShortTwo returns the validated second 1,296-word EFF short list.
func ShortTwo() (*wordlist.List, error) {
	return loadShortTwo()
}

func load(metadata wordlist.Metadata, data string) (*wordlist.List, error) {
	words := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	return wordlist.New(metadata, words)
}
