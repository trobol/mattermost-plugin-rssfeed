package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mattermost/mattermost-server/model"
)

// Subscription Object
type Subscription struct {
	URL       string
	XML       string
	Timestamp int64
	ETag      string
	Format    FeedFormat
	Title     string
	Color     string
}

type SubscriptionList struct {
	Subscriptions []*Subscription
}

// for old database compatibility
type SubscriptionMap struct {
	Subscriptions map[string]*Subscription
}

func (s *SubscriptionList) find(url string) (*Subscription, int) {
	for index, sub := range s.Subscriptions {
		if sub.URL == url {
			return sub, index
		}
	}
	return nil, -1
}

func (s *SubscriptionList) remove(index int) {
	s.Subscriptions = append(s.Subscriptions[:index], s.Subscriptions[index+1:]...)
}

func (s *SubscriptionList) addpend(sub *Subscription) {
	s.Subscriptions = append(s.Subscriptions, sub)
}

// Subscribe process the /feed subscribe <channel> <url>
func (p *RSSFeedPlugin) subscribe(ctx context.Context, url string, channelID string, userID string) {
	sub := &Subscription{
		URL:   url,
		XML:   "",
		Color: hashColor(url),
	}

	info, err := p.FetchFeedInfo(url)

	if err == nil {
		sub.Title = info.Title
		sub.Format = info.Format
		err = p.addSubscription(channelID, sub)
	}

	if err != nil {
		p.API.LogError(err.Error())
		msg := fmt.Sprintf("Failed to subscribe to %s: `%s`", url, err.Error())
		p.createBotEphemeralPost(msg, channelID, userID)
		return
	}

	attachment := &model.SlackAttachment{
		Text:     fmt.Sprintf("**[%s](%s)**", info.Title, info.Alternate),
		ThumbURL: info.Icon,
		Color:    sub.Color,
		Fields: []*model.SlackAttachmentField{
			{
				Title: "Author",
				Value: info.AuthorName,
				Short: true,
			},
			{
				Title: "Generator",
				Value: info.Generator,
				Short: true,
			},
		},
	}

	p.createBotAttachmentPost("Subscribed to:", []*model.SlackAttachment{attachment}, channelID)
}

func (p *RSSFeedPlugin) addSubscription(channelID string, sub *Subscription) error {
	subList, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	// check if url already exists
	_, index := subList.find(sub.URL)
	if index != -1 {
		return errors.New("this channel is already subscribed to that feed")
	}
	subList.addpend(sub)
	return p.storeSubscriptions(channelID, subList)
}

func (p *RSSFeedPlugin) getSubscriptions(channelID string) (*SubscriptionList, error) {
	var subList *SubscriptionList

	value, err := p.API.KVGet(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return nil, err
	}

	if value == nil {
		subList = &SubscriptionList{Subscriptions: []*Subscription{}}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(value))
		err := decoder.Decode(&subList)
		if err != nil {
			// convert old database entries
			var subMap *SubscriptionMap
			decoder := json.NewDecoder(bytes.NewReader(value))
			err := decoder.Decode(&subMap)
			if err != nil {
				return nil, err
			}

			subList = &SubscriptionList{Subscriptions: make([]*Subscription, len(subMap.Subscriptions))}
			index := 0

			for _, sub := range subMap.Subscriptions {
				subList.Subscriptions[index] = sub
				index++
			}
		}
	}

	return subList, nil
}

func (p *RSSFeedPlugin) storeSubscriptions(channelID string, s *SubscriptionList) error {
	b, err := json.Marshal(s)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	if err := p.API.KVSet(channelID, b); err != nil {
		return err
	}
	return nil
}

func (p *RSSFeedPlugin) unsubscribeFromURL(channelID string, url string) error {
	subs, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	_, index := subs.find(url)

	if index != -1 {
		subs.remove(index)
		if err := p.storeSubscriptions(channelID, subs); err != nil {
			p.API.LogError(err.Error())
			return err
		}

		return nil
	}

	return errors.New("not subscribed to that url")
}

func (p *RSSFeedPlugin) unsubscribeFromIndex(channelID string, index int) error {
	subs, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	if index < 0 || index >= len(subs.Subscriptions) {
		return errors.New("index out of range")
	}

	subs.remove(index)
	err = p.storeSubscriptions(channelID, subs)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	return nil
}

/*
func (p *RSSFeedPlugin) updateSubscription(channelID string, subscription *Subscription) error {
	currentSubscriptions, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	key := subscription.URL
	_, ok := currentSubscriptions.Subscriptions[key]
	if ok {
		currentSubscriptions.Subscriptions[key] = subscription
		if err := p.storeSubscriptions(channelID, currentSubscriptions); err != nil {
			p.API.LogError(err.Error())
			return err
		}
	}
	return nil
}
*/
