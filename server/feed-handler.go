package main

import (
	"encoding/xml"
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lunny/html2md"
	"github.com/mattermost/mattermost-server/model"
	"golang.org/x/net/html/charset"
)

type FeedFormat int8

const (
	FeedFormatRSSV2 FeedFormat = 1
	FeedFormatAtom  FeedFormat = 2
)

type FeedInfo struct {
	Title      string
	AuthorName string
	AuthorURL  string
	Format     FeedFormat
	Alternate  string
	Icon       string
	Generator  string
}

type FeedHandler interface {
	processFeed(*Subscription, *configuration) ([]*model.SlackAttachment, error)
	processRSSV2Feed(*Subscription, *RSSV2, string, *configuration) ([]*model.SlackAttachment, error)
	processAtomFeed(*Subscription, *AtomFeed, *configuration) ([]*model.SlackAttachment, error)

	FetchFeedInfo(url string) (*FeedInfo, error)
	FetchFeedBody(subs *Subscription) (string, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type FeedHandlerDefault struct {
	client HTTPClient
}

const RelAlternate = "alternate"

func newFeedHandler() FeedHandler {
	//src: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
	return FeedHandlerDefault{
		client: &http.Client{
			Transport: &http.Transport{
				Dial: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).Dial,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
}

func (h FeedHandlerDefault) processFeed(subscription *Subscription, config *configuration) ([]*model.SlackAttachment, error) {
	if len(subscription.URL) == 0 {
		return nil, errors.New("no url supplied")
	}

	body, err := h.FetchFeedBody(subscription)

	if err != nil {
		return nil, err
	}
	if body == "" {
		return nil, nil
	}

	decoder := xml.NewDecoder(strings.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel

	if subscription.Format == FeedFormatRSSV2 {
		rssFeed, err := RSSV2ParseString(body)

		if err != nil {
			return nil, err
		}
		return h.processRSSV2Feed(subscription, rssFeed, body, config)
	}

	if subscription.Format == FeedFormatAtom {
		atomFeed, err := AtomParseString(body)

		if err != nil {
			return nil, err
		}
		return h.processAtomFeed(subscription, atomFeed, config)
	}

	return nil, errors.New("invalid feed format")
}

func (h FeedHandlerDefault) processRSSV2Feed(subscription *Subscription, newRssFeed *RSSV2, newRssFeedString string, config *configuration) ([]*model.SlackAttachment, error) {
	// retrieve old xml feed from database
	oldRssFeed, err := RSSV2ParseString(subscription.XML)
	if err != nil {
		return nil, err
	}

	items := RSSV2CompareItemsBetweenOldAndNew(oldRssFeed, newRssFeed)
	attachments := make([]*model.SlackAttachment, len(items))
	for index, item := range items {
		attachment := &model.SlackAttachment{
			Title:     item.Title,
			TitleLink: item.Link,
		}

		if config.ShowDescription {
			attachment.Text = html2md.Convert(item.Description)
		}
		attachments[index] = attachment
	}
	if len(items) > 0 {
		subscription.XML = newRssFeedString
	}

	subscription.Timestamp = time.Now().Unix()

	return attachments, nil
}

func (h FeedHandlerDefault) processAtomFeed(subscription *Subscription, feed *AtomFeed, config *configuration) ([]*model.SlackAttachment, error) {
	feedTimestamp := AtomParseTimestamp(feed.Updated)

	if subscription.Timestamp >= AtomParseTimestamp(feed.Updated) {
		return nil, nil
	}

	items := feed.ItemsAfter(subscription.Timestamp)

	attachments := make([]*model.SlackAttachment, len(items))

	for index, item := range items {
		attachment := &model.SlackAttachment{
			Title:      item.Title,
			Fallback:   item.Title,
			AuthorName: item.Author.Name,
			AuthorLink: item.Author.URI,
			AuthorIcon: getGravatarIcon(item.Author.Email, config.GravatarDefault),
			Color:      subscription.Color,
		}

		attachments[index] = attachment
		for _, link := range item.Link {
			if link.Rel == RelAlternate {
				attachment.TitleLink = link.Href
			}
		}

		// timestamp field currently unused by mattermost
		if item.Published != "" {
			attachment.Timestamp = AtomParseTimestamp(item.Published)
		} else {
			attachment.Timestamp = AtomParseTimestamp(item.Updated)
		}

		if item.Content != nil {
			body := attachment.Text
			if item.Content.Type != "text" {
				body = html2md.Convert(body)
			}
			attachment.Text = strings.TrimSpace(body)
		}
	}

	subscription.Timestamp = feedTimestamp

	return attachments, nil
}

func (h FeedHandlerDefault) FetchFeedBody(sub *Subscription) (string, error) {
	req, err := http.NewRequest("GET", sub.URL, nil)

	if err != nil {
		return "", err
	}

	// setting ETag will (depending on the support of the server)
	// return only entries from after the date or NotModified if there are none
	// https://en.wikipedia.org/wiki/HTTP_ETag
	req.Header.Add("If-None-Match", sub.ETag)

	return h.fetchRequest(req)
}

func (h FeedHandlerDefault) fetchRequest(req *http.Request) (string, error) {
	resp, err := h.client.Do(req)
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

func (h FeedHandlerDefault) FetchFeedInfo(url string) (*FeedInfo, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	body, err := h.fetchRequest(req)
	if err != nil {
		return nil, err
	}

	atomFeed, err := AtomParseString(body)

	if err == nil {
		info := &FeedInfo{
			Title:      atomFeed.Title,
			Format:     FeedFormatAtom,
			AuthorName: atomFeed.Author.Name,
			AuthorURL:  atomFeed.Author.URI,
			Icon:       atomFeed.Icon,
			Generator:  atomFeed.Generator,
		}

		for _, link := range atomFeed.Link {
			if link.Rel == RelAlternate {
				info.Alternate = link.Href
				break
			}
		}

		return info, nil
	}

	rssFeed, err := RSSV2ParseString(body)

	if err == nil {
		info := &FeedInfo{
			Title:  rssFeed.Channel.Title,
			Format: FeedFormatRSSV2,
		}

		return info, nil
	}

	return nil, errors.New("invalid feed")
}
