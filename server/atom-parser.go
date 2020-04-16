/*
Package atomparser
*/

package main

import (
	"encoding/xml"
	"strings"

	"golang.org/x/net/html/charset"
	"golang.org/x/tools/blog/atom"
)

type AtomFeed atom.Feed

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

// AtomCompareItems - This function will used to compare 2 atom feed xml item objects
// and will return a list of differing items
func AtomCompareItems(feedOne *AtomFeed, feedTwo *AtomFeed) []*atom.Entry {
	biggerFeed := feedOne
	smallerFeed := feedTwo
	itemList := []*atom.Entry{}
	if len(feedTwo.Entry) > len(feedOne.Entry) {
		biggerFeed = feedTwo
		smallerFeed = feedOne
	} else if len(feedTwo.Entry) == len(feedOne.Entry) {
		return itemList
	}

	for _, item1 := range biggerFeed.Entry {
		exists := false
		for _, item2 := range smallerFeed.Entry {
			if item1.ID == item2.ID {
				exists = true
				break
			}
		}
		if !exists {
			itemList = append(itemList, item1)
		}
	}
	return itemList
}

// AtomCompareItemsBetweenOldAndNew - This function will used to compare 2 atom xml event objects
// and will return a list of items that are specifically in the newer feed but not in
// the older feed
func AtomCompareItemsBetweenOldAndNew(feedOld *AtomFeed, feedNew *AtomFeed) []*atom.Entry {
	itemList := []*atom.Entry{}

	for _, item1 := range feedNew.Entry {
		exists := false
		for _, item2 := range feedOld.Entry {
			if item1.ID == item2.ID {
				exists = true
				break
			}
		}
		if !exists {
			itemList = append(itemList, item1)
		}
	}
	return itemList
}
