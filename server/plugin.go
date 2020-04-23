package main

import (
	"crypto/md5" //nolint comments
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/blang/semver"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
	"github.com/pkg/errors"
)

//const RSSFeedIconURL = "./plugins/rssfeed/assets/rss.png"

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

	FeedHandler
}

// ServeHTTP hook from mattermost plugin
func (p *RSSFeedPlugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	switch path := r.URL.Path; path {
	case "/images/rss.png":
		data, err := ioutil.ReadFile(string("plugins/rssfeed/assets/rss.png"))
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			_, writeErr := w.Write(data)
			if writeErr != nil {
				p.API.LogError(writeErr.Error())
			}
		} else {
			w.WriteHeader(404)
			_, writeErr := w.Write([]byte("404 Something went wrong - " + http.StatusText(404)))
			if writeErr != nil {
				p.API.LogError(err.Error())
			}
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
			p.processSubscription(key, value)
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

func (p *RSSFeedPlugin) processSubscription(channelID string, subscription *Subscription) {
	config := p.getConfiguration()

	attachments, err := p.processFeed(subscription, config)

	if err != nil {
		p.API.LogError(err.Error())
		return
	}

	if attachments == nil {
		return
	}

	if config.SortMessages {
		sort.Slice(attachments, func(i, j int) bool {
			return attachments[i].Timestamp.(int64) < attachments[j].Timestamp.(int64)
		})
	}

	//Send as separate messages or group as few messages as possible
	var groupedAttachments [][]*model.SlackAttachment
	if config.GroupMessages {
		groupedAttachments, err = p.groupAttachments(attachments)
		if err != nil {
			p.API.LogError(err.Error())
			return
		}
	} else {
		groupedAttachments = p.padAttachments(attachments)
	}

	for _, group := range groupedAttachments {
		p.createBotPost("", group, channelID, model.POST_DEFAULT)
	}

	if err := p.updateSubscription(channelID, subscription); err != nil {
		p.API.LogError(err.Error())
	}
}

func (p *RSSFeedPlugin) checkServerVersion() error {
	serverVersion, err := semver.Parse(p.API.GetServerVersion())
	if err != nil {
		return errors.Wrap(err, "failed to parse server version")
	}

	r := semver.MustParseRange(">=" + minimumServerVersion)
	if !r(serverVersion) {
		return fmt.Errorf("this plugin requires Mattermost v%s or later", minimumServerVersion)
	}

	return nil
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

func (p *RSSFeedPlugin) createBotPost(msg string, attachments []*model.SlackAttachment, channelID string, postType string) {
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Message:   msg,
		Type:      postType,
	}

	post.AddProp("attachments", attachments)

	if _, err := p.API.CreatePost(post); err != nil {
		p.API.LogError(err.Error())
	}
}

func getGravatarIcon(email string, defaultIcon string) string {
	hash := ""
	if email == "" {
		hash = "00000000000000000000000000000000"
	} else {
		sum := md5.Sum([]byte(strings.TrimSpace(email))) //nolint comments
		hash = hex.EncodeToString(sum[:])
	}
	return fmt.Sprintf("https://www.gravatar.com/avatar/%s?d=%s&s40", hash, defaultIcon)
}

func hashColor(s string) string {
	hash := fnv.New32()
	_, err := hash.Write([]byte(s))
	if err != nil {
		return "ffffff"
	}
	color := fmt.Sprintf("#%x", hash.Sum32())
	return color[:7]
}
