package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const SUBSCRIPTIONS_KEY = "subscriptions"

// Subscriptions map to key value pairs
type Subscriptions struct {
	Subscriptions map[string]*Subscription
}

// Subscribe prosses the /feed subscribe <channel> <url>
func (p *RSSFeedPlugin) subscribe(ctx context.Context, channelID string, url string) (*Subscription, error) {
	sub := &Subscription{
		ChannelID: channelID,
		URL:       url,
		XML:       "",
	}

	body, err := sub.Fetch()

	str := string(body)

	rssFeed, err := RSSV2ParseString(str)

	if err == nil {
		sub.Title = rssFeed.Channel.Title
		sub.Format = FEED_FORMAT_RSSV2

	} else {
		atomFeed, err := AtomParseString(str)
		if err == nil {
			sub.Title = atomFeed.Title
			sub.Format = FEED_FORMAT_ATOM
		} else {
			return nil, errors.New("invalid feed")
		}
	}

	key := getKey(channelID, url)
	if err := p.addSubscription(key, sub); err != nil {
		p.API.LogError(err.Error())
		return nil, err
	}

	return sub, nil
}

func (p *RSSFeedPlugin) addSubscription(key string, sub *Subscription) error {
	currentSubscriptions, err := p.getSubscriptions()
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	// check if url already exists
	_, ok := currentSubscriptions.Subscriptions[key]
	if !ok {
		currentSubscriptions.Subscriptions[key] = sub
		err = p.storeSubscriptions(currentSubscriptions)
		if err != nil {
			p.API.LogError(err.Error())
			return err
		}

	} else {
		return errors.New("this channel is already subscribed to that feed")
	}

	return nil
}

func (p *RSSFeedPlugin) getSubscriptions() (*Subscriptions, error) {
	var subscriptions *Subscriptions

	value, err := p.API.KVGet(SUBSCRIPTIONS_KEY)
	if err != nil {
		p.API.LogError(err.Error())
		return nil, err
	}

	if value == nil {
		subscriptions = &Subscriptions{Subscriptions: map[string]*Subscription{}}
	} else {
		json.NewDecoder(bytes.NewReader(value)).Decode(&subscriptions)
	}

	return subscriptions, nil
}

func (p *RSSFeedPlugin) storeSubscriptions(s *Subscriptions) error {
	b, err := json.Marshal(s)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	p.API.KVSet(SUBSCRIPTIONS_KEY, b)
	return nil
}

func (p *RSSFeedPlugin) unsubscribe(channelID string, url string) (*Subscription, error) {

	currentSubscriptions, err := p.getSubscriptions()
	if err != nil {
		p.API.LogError(err.Error())
		return nil, err
	}

	subClone := &Subscription{}

	key := getKey(channelID, url)
	sub, ok := currentSubscriptions.Subscriptions[key]
	*subClone = *sub
	if ok {
		delete(currentSubscriptions.Subscriptions, key)
		if err := p.storeSubscriptions(currentSubscriptions); err != nil {
			p.API.LogError(err.Error())
			return nil, err
		}
	}

	return subClone, nil
}

func (p *RSSFeedPlugin) updateSubscription(subscription *Subscription) error {
	currentSubscriptions, err := p.getSubscriptions()
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	key := getKey(subscription.ChannelID, subscription.URL)
	_, ok := currentSubscriptions.Subscriptions[key]
	if ok {
		currentSubscriptions.Subscriptions[key] = subscription
		if err := p.storeSubscriptions(currentSubscriptions); err != nil {
			p.API.LogError(err.Error())
			return err
		}
	}
	return nil
}

func getKey(channelID string, url string) string {
	return fmt.Sprintf("%s/%s", channelID, url)
}
