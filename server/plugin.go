package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunny/html2md"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
	"golang.org/x/net/html/charset"
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

	keys, err := p.API.KVList(0, 50)

	if err != nil {
		return err
	}
	for _, key := range keys {
		dictionaryOfSubscriptions, err := p.getSubscriptions(key)
		if err != nil {
			return err
		}

		for _, value := range dictionaryOfSubscriptions.Subscriptions {
			err := p.processSubscription(key, value)
			if err != nil {
				p.API.LogError(err.Error())
			}
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

func (p *RSSFeedPlugin) processSubscription(channelID string, subscription *Subscription) error {
	config := p.getConfiguration()

	attachments, err := p.processFeed(subscription)

	if err != nil {
		return err
	}

	if attachments == nil {
		return nil
	}

	//Send as separate messages or group as few messages as possible
	var groupedAttachments [][]*model.SlackAttachment
	if config.GroupMessages {
		groupedAttachments, err = p.groupAttachments(attachments)
		if err != nil {
			return err
		}
	} else {
		groupedAttachments = p.padAttachments(attachments)
	}

	for _, group := range groupedAttachments {

		p.createBotPost("", group, channelID, model.POST_DEFAULT)
	}

	p.updateSubscription(channelID, subscription)
	return nil
}

func (p *RSSFeedPlugin) processFeed(subscription *Subscription) ([]*model.SlackAttachment, error) {

	if len(subscription.URL) == 0 {
		return nil, errors.New("no url supplied")
	}

	body, err := subscription.FetchBody()

	if err != nil {
		return nil, err
	}
	if body == "" {
		return nil, nil
	}

	decoder := xml.NewDecoder(strings.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel

	rssFeed, err := RSSV2ParseString(body)

	if err == nil {
		return p.processRSSV2Subscription(subscription, rssFeed, body)
	}

	atomFeed, err := AtomParseString(body)

	if err == nil {
		return p.processAtomSubscription(subscription, atomFeed)
	}

	return nil, errors.New("invalid feed format")
}

func (p *RSSFeedPlugin) processRSSV2Subscription(subscription *Subscription, newRssFeed *RSSV2, newRssFeedString string) ([]*model.SlackAttachment, error) {
	config := p.getConfiguration()

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

func (p *RSSFeedPlugin) processAtomSubscription(subscription *Subscription, feed *AtomFeed) ([]*model.SlackAttachment, error) {

	config := p.getConfiguration()

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

	subscription.Timestamp = feedTimestamp

	return attachments, nil
}

// number of characters Json encoded array of slack attachments must be smaller than POST_PROPS_MAX_USER_RUNES
func (p *RSSFeedPlugin) groupAttachments(attachments []*model.SlackAttachment) ([][]*model.SlackAttachment, error) {

	start := 0
	size := 2

	groupedAttachments := make([][]*model.SlackAttachment, 0)

	for end, attachment := range attachments {
		encoded, err := json.Marshal(attachment)

		if err != nil {
			return nil, err
		}

		encodedSize := utf8.RuneCountInString(string(encoded))

		size += encodedSize + 1

		if size > model.POST_PROPS_MAX_USER_RUNES && start != end {
			groupedAttachments = append(groupedAttachments, attachments[start:end])
			start = end
			size = 2
		}

		if encodedSize > model.POST_PROPS_MAX_USER_RUNES {

			//single attachment is too long, trim then add
			diff := encodedSize - model.POST_PROPS_MAX_USER_RUNES
			textOffset := len(attachment.Text) - diff

			if textOffset > 0 {
				attachment.Text = attachment.Text[:textOffset]

				groupedAttachments = append(groupedAttachments, []*model.SlackAttachment{attachment})
			} else {
				//if we cant trim log error and skip
				p.API.LogError("Attachment was too large and text could not be trimmed")
			}
			start = end + 1
		}

	}
	if start < len(attachments) {
		groupedAttachments = append(groupedAttachments, attachments[start:])
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

func (p *RSSFeedPlugin) createBotPost(msg string, attachments []*model.SlackAttachment, channelID string, postType string) error {

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Message:   msg,
		Type:      postType,
	}

	post.AddProp("attachments", attachments)

	if _, err := p.API.CreatePost(post); err != nil {
		p.API.LogError(err.Error())
		return err
	}

	return nil
}

func getGravatarIcon(email string, defaultIcon string) string {
	hash := ""
	if email == "" {
		hash = "00000000000000000000000000000000"
	} else {
		sum := md5.Sum([]byte(strings.TrimSpace(email)))
		hash = hex.EncodeToString(sum[:])
	}
	return fmt.Sprintf("https://www.gravatar.com/avatar/%s?d=%s&s40", hash, defaultIcon)
}
