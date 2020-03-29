package main

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunny/html2md"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
	"github.com/wbernest/atom-parser"
	"github.com/wbernest/rss-v2-parser"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//const RSSFEED_ICON_URL = "./plugins/rssfeed/assets/rss.png"

// RSSFeedPlugin Object
type RSSFeedPlugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	botUserID            string
	processHeartBeatFlag bool
}

// ServeHTTP hook from mattermost plugin
func (p *RSSFeedPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	switch path := r.URL.Path; path {
	case "/images/rss.png":
		data, err := ioutil.ReadFile(string("plugins/rssfeed/assets/rss.png"))
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
		} else {
			w.WriteHeader(404)
			w.Write([]byte("404 Something went wrong - " + http.StatusText(404)))
			p.API.LogInfo("/images/rss.png err = ", err.Error())
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		http.NotFound(w, r)
	}
}

func (p *RSSFeedPlugin) setupHeartBeat() {
	heartbeatTime, err := p.getHeartbeatTime()
	if err != nil {
		p.API.LogError(err.Error())
	}

	for p.processHeartBeatFlag {
		//p.API.LogDebug("Heartbeat")

		err := p.processHeartBeat()
		if err != nil {
			p.API.LogError(err.Error())

		}
		time.Sleep(time.Duration(heartbeatTime) * time.Minute)
	}
}

func (p *RSSFeedPlugin) processHeartBeat() error {
	dictionaryOfSubscriptions, err := p.getSubscriptions()
	if err != nil {
		return err
	}

	for _, value := range dictionaryOfSubscriptions.Subscriptions {
		err := p.processSubscription(value)
		if err != nil {
			p.API.LogError(err.Error())
		}
	}

	return nil
}

func (p *RSSFeedPlugin) getHeartbeatTime() (int, error) {
	config := p.getConfiguration()
	heartbeatTime := 15
	var err error
	if len(config.Heartbeat) > 0 {
		heartbeatTime, err = strconv.Atoi(config.Heartbeat)
		if err != nil {
			return 15, err
		}
	}

	return heartbeatTime, nil
}

func (p *RSSFeedPlugin) processSubscription(subscription *Subscription) error {
	config := p.getConfiguration()
	if len(subscription.URL) == 0 {
		return errors.New("no url supplied")
	}

	var attachments []*model.SlackAttachment
	if rssv2parser.IsValidFeed(subscription.URL) {
		var err error
		attachments, err = p.processRSSV2Subscription(subscription)
		if err != nil {
			return errors.New("invalid RSS v2 feed format - " + err.Error())
		}

	} else if atomparser.IsValidFeed(subscription.URL) {
		var err error
		attachments, err = p.processAtomSubscription(subscription)
		if err != nil {
			return errors.New("invalid atom feed format - " + err.Error())
		}
	} else {
		return errors.New("invalid feed format")
	}

	//Send as separate messages or group as few messages as possible
	var groupedAttachments [][]*model.SlackAttachment
	if config.GroupMessages {
		var err error
		groupedAttachments, err = p.groupAttachments(attachments)
		if err != nil {
			return err
		}
	} else {
		groupedAttachments = p.padAttachments(attachments)
	}

	p.API.LogError(fmt.Sprintf("%i", len(groupedAttachments)))
	for _, group := range groupedAttachments {

		p.createBotPost(subscription.ChannelID, group, "custom_git_pr")
	}

	return nil
}

func (p *RSSFeedPlugin) processRSSV2Subscription(subscription *Subscription) ([]*model.SlackAttachment, error) {
	config := p.getConfiguration()

	// get new rss feed string from url
	newRssFeed, newRssFeedString, err := rssv2parser.ParseURL(subscription.URL)
	if err != nil {
		return nil, err
	}

	// retrieve old xml feed from database
	oldRssFeed, err := rssv2parser.ParseString(subscription.XML)
	if err != nil {
		return nil, err
	}

	items := rssv2parser.CompareItemsBetweenOldAndNew(oldRssFeed, newRssFeed)
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
		p.updateSubscription(subscription)
	}

	return attachments, nil
}

func (p *RSSFeedPlugin) processAtomSubscription(subscription *Subscription) ([]*model.SlackAttachment, error) {
	// get new rss feed string from url
	newFeed, newFeedString, err := atomparser.ParseURL(subscription.URL)
	if err != nil {
		return nil, err
	}

	// retrieve old xml feed from database
	oldFeed, err := atomparser.ParseString(subscription.XML)
	if err != nil {
		return nil, err
	}

	items := atomparser.CompareItemsBetweenOldAndNew(oldFeed, newFeed)

	attachments := make([]*model.SlackAttachment, len(items))

	for index, item := range items {
		attachment := &model.SlackAttachment{
			Title:      item.Title,
			Fallback:   item.Title,
			AuthorName: item.Author.Name,
			AuthorLink: item.Author.URI,
			AuthorIcon: getGravatarIcon(item.Author.Email),
		}

		attachments[index] = attachment
		for _, link := range item.Link {
			if link.Rel == "alternate" {
				attachment.TitleLink = link.Href
			}
		}

		//timestamp field currently unused by mattermost
		if item.Published != "" {
			attachment.Timestamp = string(item.Published)
		} else {
			attachment.Timestamp = string(item.Updated)
		}

		if item.Content != nil {
			body := attachment.Text
			if item.Content.Type != "text" {
				body = html2md.Convert(body)
			}
			attachment.Text = strings.TrimSpace(body)

		} else {
			p.API.LogInfo("Missing content in atom feed item",
				"subscription_url", subscription.URL,
				"item_title", item.Title)
		}

	}

	if len(items) > 0 {
		subscription.XML = newFeedString
		p.updateSubscription(subscription)
	}

	return attachments, nil
}

// # of characters Json encoded array of slack attachments must be smaller than POST_PROPS_MAX_RUNES
func (p *RSSFeedPlugin) groupAttachments(attachments []*model.SlackAttachment) ([][]*model.SlackAttachment, error) {

	start := 0
	size := 2

	//mattermost max runes +
	groupedAttachments := make([][]*model.SlackAttachment, 0)

	for end, attachment := range attachments {
		encoded, err := json.Marshal(attachment)

		if err != nil {
			return nil, err
		}

		encodedSize := utf8.RuneCountInString(string(encoded))

		if size > model.POST_PROPS_MAX_USER_RUNES {
			if start == end {
				//single attachment is too long, trim then add
				diff := size - model.POST_PROPS_MAX_USER_RUNES
				textOffset := len(attachment.Text) - diff

				if textOffset > 0 {
					attachment.Text = attachment.Text[:textOffset]
				} else {
					//if we cant trim log error and skip
					p.API.LogError("Attachment was too large and text could not be trimmed")
				}

				start = end
				size = 0
			} else {
				groupedAttachments = append(groupedAttachments, attachments[start:end])
			}
			start = end
			size = 0
		} else {
			size = size + encodedSize + 1 //+1 for comma
		}
	}
	if start <= len(attachments)-1 {
		groupedAttachments = append(groupedAttachments, attachments[start:len(attachments)])
	}

	return groupedAttachments, nil
}

func (p *RSSFeedPlugin) padAttachments(attachments []*model.SlackAttachment) [][]*model.SlackAttachment {
	result := make([][]*model.SlackAttachment, len(attachments))

	for index, attachment := range attachments {
		result[index] = []*model.SlackAttachment{attachment}
	}
	return result
}

func (p *RSSFeedPlugin) createBotPost(channelID string, attachments []*model.SlackAttachment, postType string) error {

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,

		Type: postType,
	}

	post.AddProp("attachments", attachments)

	//str, _ := json.Marshal(attachments)
	//post.Message = string(str)

	if _, err := p.API.CreatePost(post); err != nil {
		p.API.LogError(err.Error())
		return err
	}

	return nil
}

func getGravatarIcon(email string) string {
	const url = "https://www.gravatar.com/avatar/"
	parameters := "?d=mp&s=40" // TODO : Add setting to control fallback image https://en.gravatar.com/site/implement/images/
	hash := ""
	if email == "" {
		hash = "00000000000000000000000000000000"
	} else {
		sum := md5.Sum([]byte(strings.TrimSpace(email)))
		hash = fmt.Sprintf("%x", sum)
	}
	return url + hash + parameters
}

func isValidFeed(url string) bool {
	return rssv2parser.IsValidFeed(url) || atomparser.IsValidFeed(url)
}
