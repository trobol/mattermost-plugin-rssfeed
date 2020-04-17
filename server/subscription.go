package main

import (
	"io/ioutil"
	"net/http"
)

type SubscriptionFormat int8

const (
	FEED_FORMAT_UNKNOWN SubscriptionFormat = 0
	FEED_FORMAT_RSSV2   SubscriptionFormat = 1
	FEED_FORMAT_ATOM    SubscriptionFormat = 2
)

// Subscription Object
type Subscription struct {
	ChannelID string
	URL       string
	XML       string
	Timestamp int64
	ETag      string
	Format    SubscriptionFormat
	Title     string
}

type SubscriptionResponse struct {
	Body       []byte
	Header     http.Header
	StatusCode int
}

func (s *Subscription) Fetch() (SubscriptionResponse, error) {

	client := &http.Client{}

	req, err := http.NewRequest("GET", s.URL, nil)

	req.Header.Add("If-None-Match", s.ETag)

	resp, err := client.Do(req)
	if err != nil {
		return SubscriptionResponse{nil, resp.Header, resp.StatusCode}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SubscriptionResponse{nil, resp.Header, resp.StatusCode}, nil
	}

	body, err := ioutil.ReadAll(resp.Body)

	return SubscriptionResponse{body, resp.Header, resp.StatusCode}, nil
}
