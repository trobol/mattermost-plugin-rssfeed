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

// Subscriptions map to key value pairs
type Subscriptions struct {
	Subscriptions map[string]*Subscription
}

// Subscribe prosses the /feed subscribe <channel> <url>
func (p *RSSFeedPlugin) subscribe(ctx context.Context, channelID string, url string) {
	sub := &Subscription{
		URL:   url,
		XML:   "",
		Color: hashColor(url),
	}

	info, err := p.FetchFeedInfo(url)

	if err != nil {
		p.API.LogError(err.Error())
		p.createBotPost(fmt.Sprintf("Failed to subscribe to %s: `%s`", url, err.Error()), nil, channelID, model.POST_DEFAULT)
		return
	}

	sub.Title = info.Title
	sub.Format = info.Format

	if err := p.addSubscription(channelID, sub); err != nil {
		p.createBotPost(fmt.Sprintf("Failed to subscribe to %s: `%s`", url, err.Error()), nil, channelID, model.POST_DEFAULT)
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

	p.createBotPost("Subscribed to:", []*model.SlackAttachment{attachment}, channelID, model.POST_DEFAULT)
}

func (p *RSSFeedPlugin) addSubscription(channelID string, sub *Subscription) error {
	currentSubscriptions, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	key := sub.URL

	// check if url already exists
	_, ok := currentSubscriptions.Subscriptions[key]
	if !ok {
		currentSubscriptions.Subscriptions[key] = sub
		err = p.storeSubscriptions(channelID, currentSubscriptions)
		if err != nil {
			p.API.LogError(err.Error())
			return err
		}
	}
	return errors.New("this channel is already subscribed to that feed")
}

func (p *RSSFeedPlugin) getSubscriptions(channelID string) (*Subscriptions, error) {
	var subscriptions *Subscriptions

	value, err := p.API.KVGet(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return nil, err
	}

	if value == nil {
		subscriptions = &Subscriptions{Subscriptions: map[string]*Subscription{}}
	} else {
		err := json.NewDecoder(bytes.NewReader(value)).Decode(&subscriptions)
		if err != nil {
			return nil, err
		}
	}

	return subscriptions, nil
}

func (p *RSSFeedPlugin) storeSubscriptions(channelID string, s *Subscriptions) error {
	b, err := json.Marshal(s)
	if err != nil {
		p.API.LogError(err.Error())
		return err
	}

	if err := p.API.KVSet(channelID, b); err != nil {
		p.API.LogError(err.Error())
		return err
	}
	return nil
}

func (p *RSSFeedPlugin) unsubscribe(channelID string, url string) (*Subscription, error) {
	currentSubscriptions, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return nil, err
	}

	subClone := &Subscription{}

	key := url
	sub, ok := currentSubscriptions.Subscriptions[key]
	if ok {
		*subClone = *sub
		delete(currentSubscriptions.Subscriptions, key)
		if err := p.storeSubscriptions(channelID, currentSubscriptions); err != nil {
			p.API.LogError(err.Error())
			return nil, err
		}

		return subClone, nil
	}

	return nil, errors.New("not subscribed")
}

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
