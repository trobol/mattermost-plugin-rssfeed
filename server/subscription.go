package main

import (
	"errors"
	"io/ioutil"
	"net/http"
	"time"
)

type SubscriptionFormat int8

const (
	FEED_FORMAT_UNKNOWN SubscriptionFormat = 0
	FEED_FORMAT_RSSV2   SubscriptionFormat = 1
	FEED_FORMAT_ATOM    SubscriptionFormat = 2
)

// Subscription Object
type Subscription struct {
	URL       string
	XML       string
	Timestamp int64
	ETag      string
	Format    SubscriptionFormat
	Title     string
}

type SubscriptionInfo struct {
	Title      string
	AuthorName string
	AuthorURL  string
	Format     SubscriptionFormat
	Alternate  string
	Icon       string
	Generator  string
}

func (s *Subscription) FetchBody() (string, error) {

	client := &http.Client{}

	req, err := http.NewRequest("GET", s.URL, nil)

	if err != nil {
		return "", err
	}

	req.Header.Add("If-None-Match", s.ETag)

	return fetchRequest(client, req)

}

func fetchRequest(client *http.Client, req *http.Request) (string, error) {

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return "", nil
	} else if resp.StatusCode != http.StatusOK {
		return "", err
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (s *Subscription) FetchInfo() (*SubscriptionInfo, error) {

	client := &http.Client{
		Timeout: time.Second,
	}

	req, err := http.NewRequest("GET", s.URL, nil)
	if err != nil {
		return nil, err
	}

	body, err := fetchRequest(client, req)
	if err != nil {
		return nil, err
	}

	atomFeed, err := AtomParseString(body)

	if err == nil {
		info := &SubscriptionInfo{
			Title:      atomFeed.Title,
			Format:     FEED_FORMAT_ATOM,
			AuthorName: atomFeed.Author.Name,
			AuthorURL:  atomFeed.Author.URI,
			Icon:       atomFeed.Icon,
			Generator:  atomFeed.Generator,
		}

		for _, link := range atomFeed.Link {
			if link.Rel == "alternate" {
				info.Alternate = link.Href
				break
			}
		}

		return info, nil
	}

	rssFeed, err := RSSV2ParseString(body)

	if err == nil {
		info := &SubscriptionInfo{
			Title:  rssFeed.Channel.Title,
			Format: FEED_FORMAT_RSSV2,
		}

		return info, nil
	}

	return nil, errors.New("invalid feed")

}
