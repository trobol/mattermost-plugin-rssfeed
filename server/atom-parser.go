/*
Package atomparser
*/

package main

import (
	"encoding/xml"
	"strings"

	"golang.org/x/net/html/charset"
	"golang.org/x/tools/blog/atom"

	"time"
)

type AtomFeed struct {
	atom.Feed
	Icon      string `xml:"icon"`
	Generator string `xml:"generator"`
}

// AtomParseString will be used to parse strings and will return the Atom object
func AtomParseString(s string) (*AtomFeed, error) {
	feed := AtomFeed{}
	if len(s) == 0 {
		return &feed, nil
	}

	decoder := xml.NewDecoder(strings.NewReader(s))
	decoder.CharsetReader = charset.NewReaderLabel
	err := decoder.Decode(&feed)
	if err != nil {
		return nil, err
	}
	return &feed, nil
}

// ItemsAfter - Get items that have been updated after timestamp
func (feed *AtomFeed) ItemsAfter(timestamp int64) []*atom.Entry {
	itemList := []*atom.Entry{}

	for _, item := range feed.Entry {
		if AtomParseTimestamp(item.Updated) > timestamp {
			itemList = append(itemList, item)
		}
	}
	return itemList
}

// AtomParseTimestamp - turn an atom timestamp into a unix timestamp
func AtomParseTimestamp(str atom.TimeStr) int64 {
	t, _ := time.Parse(time.RFC3339, string(str))
	return t.Unix()
}
