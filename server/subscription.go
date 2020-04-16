package main

import (
	"io/ioutil"
	"net/http"
)

type SubscriptionFormat int8

const (
	RSS2 SubscriptionFormat = 0
	ATOM SubscriptionFormat = 1
)

// Subscription Object
type Subscription struct {
	ChannelID string
	URL       string
	XML       string
	Timestamp int64
	ETag      string
	Format    SubscriptionFormat
}

type SubscriptionResponse struct {
	Body       []byte
	Header     http.Header
	StatusCode int
}

func (s *Subscription) Fetch() (SubscriptionResponse, error) {
	resp, err := http.Get(s.URL)
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
