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

// const RSSFeedIconURL = "./plugins/rssfeed/assets/rss.png"

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
		p.handleIcon(w, r)
	case "/unsub":
		p.handleHTTPUnsub(w, r)
	case "/fetch":
		p.handleHTTPFetch(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		http.NotFound(w, r)
	}
}

func (p *RSSFeedPlugin) getURL() string {
	siteURL := *p.API.GetConfig().ServiceSettings.SiteURL

	return fmt.Sprintf("%s/plugins/%s", siteURL, manifest.ID)
}

func (p *RSSFeedPlugin) handleIcon(w http.ResponseWriter, r *http.Request) {
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
}

func (p *RSSFeedPlugin) handleHTTPFetch(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	channelID, hasChannel := params["channel"]
	if !hasChannel {
		http.Error(w, "channel is required", http.StatusBadRequest)
		return
	}

	fmt.Fprintf(w, "OK")
	// FIXME: this will fail silently
	p.processChannel(channelID[0])
}

func (p *RSSFeedPlugin) ensureIds(channelID string, subs *SubscriptionList) {
	updated := false
	for _, s := range subs.Subscriptions {
		if s.ID == 0 {
			updated = true
			s.ID = makeHash(s.URL)
		}
	}
	if updated {
		err := p.storeSubscriptions(channelID, subs)
		if err != nil {
			return
		}
	}
}

func (p *RSSFeedPlugin) makeUnsubAttachments(channelID string, selected uint32) (*model.SlackAttachment, error) {
	subs, err := p.getSubscriptions(channelID)

	if err != nil {
		return nil, err
	}
	p.ensureIds(channelID, subs)

	options := make([]*model.PostActionOptions, len(subs.Subscriptions))
	for i, s := range subs.Subscriptions {
		options[i] = &model.PostActionOptions{
			Text:  s.Title,
			Value: strconv.FormatUint(uint64(s.ID), 10),
		}
	}

	selectedStr := strconv.FormatUint(uint64(selected), 10)

	url := p.getURL() + "/unsub"

	attachment := &model.SlackAttachment{
		Actions: []*model.PostAction{{
			Type: model.POST_ACTION_TYPE_SELECT,
			Name: "Select Subscription",
			Integration: &model.PostActionIntegration{
				URL: url,
				Context: model.StringInterface{
					"action": "select",
				},
			},
			Options:       options,
			DefaultOption: selectedStr,
		}, {
			Type: model.POST_ACTION_TYPE_BUTTON,
			Name: "Unsub",
			Integration: &model.PostActionIntegration{
				URL: url,
				Context: model.StringInterface{
					"action":          "post",
					"selected_option": selectedStr,
				},
			},
		}},
	}

	return attachment, nil
}

func (p *RSSFeedPlugin) handleHTTPUnsub(w http.ResponseWriter, r *http.Request) {
	request := model.PostActionIntegrationRequestFromJson(r.Body)

	if request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	action, actionOK := request.Context["action"].(string)
	selectedStr, selectedOK := request.Context["selected_option"].(string)

	if !(actionOK && selectedOK) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	selected, castErr := strconv.ParseUint(selectedStr, 10, 32)

	if castErr != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var attachment *model.SlackAttachment
	var err error
	switch action {
	case "select":
		attachment, err = p.makeUnsubAttachments(request.ChannelId, uint32(selected))

	case "post":
		if selected == 0 {
			attachment = &model.SlackAttachment{
				Title: "No changes were made",
				Color: "#03fc73",
			}
		} else {
			err = p.unsubscribeFromID(request.ChannelId, uint32(selected))

			if err == nil {
				attachment = &model.SlackAttachment{
					Title: "Success",
					Color: "#03fc73",
				}
			}
		}
	default:
		err = errors.New("invalid request")
	}

	if err != nil {
		attachment = &model.SlackAttachment{
			Title: "Error",
			Text:  err.Error(),
			Color: "#03fc73",
		}
	}
	post := &model.Post{
		Id:        request.PostId,
		UserId:    p.botUserID,
		ChannelId: request.ChannelId,
		Props: model.StringInterface{
			"attachments": []*model.SlackAttachment{
				attachment,
			},
		},
	}

	p.API.UpdateEphemeralPost(request.UserId, post)

	resp := &model.PostActionIntegrationResponse{}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.ToJson())
}

func (p *RSSFeedPlugin) setupHeartBeat() {
	heartbeatTime, err := p.getHeartbeatTime()
	if err != nil {
		p.API.LogError(err.Error())
	}

	for p.processHeartBeatFlag {
		err := p.processHeartBeat()
		if err != nil {
			p.API.LogError(err.Error())
		}
		time.Sleep(time.Duration(heartbeatTime) * time.Minute)
	}
}

func (p *RSSFeedPlugin) processHeartBeat() error {
	p.API.LogDebug("processing heartbeat")
	const keysPerPage = 50

	for index := 0; true; index++ {
		channelIDs, err := p.API.KVList(index, keysPerPage)

		if err != nil {
			return err
		}
		for _, channelID := range channelIDs {
			p.processChannel(channelID)
		}

		if len(channelIDs) < keysPerPage {
			break
		}
	}

	return nil
}

func (p *RSSFeedPlugin) processChannel(channelID string) {
	list, err := p.getSubscriptions(channelID)
	if err != nil {
		p.API.LogError(err.Error())
		return
	}

	var wg sync.WaitGroup
	for i, sub := range list.Subscriptions {
		wg.Add(1)
		go func(channelID string, sub *Subscription, i int) {
			defer wg.Done()
			p.processSubscription(channelID, sub)
		}(channelID, sub, i)
	}
	wg.Wait()

	err = p.storeSubscriptions(channelID, list)
	if err != nil {
		p.API.LogError(err.Error())
	}
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

/*
fetches & parses content,
creates bot post(s)

DOES NOT SAVE ETAG TO DATABASE
in order for content caching to work (preventing duplicate posts)
storeSubscriptions must be called
*/
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

	// Send as separate messages or group as few messages as possible
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
		p.createBotPost("", channelID, "", group)
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
			// single attachment is too long, trim then add
			diff := encodedSize - model.POST_PROPS_MAX_USER_RUNES
			textOffset := len(attachment.Text) - diff

			if textOffset > 0 {
				attachment.Text = attachment.Text[:textOffset]

				groupedAttachments = append(groupedAttachments, []*model.SlackAttachment{attachment})
			} else {
				// if we cant trim log error and skip
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

// if userId is provided the post will be ephemeral
func (p *RSSFeedPlugin) createBotPost(msg string, channelID string, userID string, attachments []*model.SlackAttachment) {
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Message:   msg,
	}

	if attachments != nil {
		post.AddProp("attachments", attachments)
	}

	if userID != "" {
		_ = p.API.SendEphemeralPost(userID, post)
	} else {
		_, err := p.API.CreatePost(post)
		if err != nil {
			p.API.LogError(err.Error())
		}
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
