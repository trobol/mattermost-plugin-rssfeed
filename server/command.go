package main

import (
	"context"
	"fmt"
	net_url "net/url"
	"strings"

	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
)

// CommandHelp is the text you see when you type /feed help
const CommandHelp = `* |/feed sub [url]| - Connect your Mattermost channel to an rss feed 
* |/feed list | - Lists the rss feeds you have subscribed to
* |/feed unsub | - Opens the unsubscribe dialog
* |/feed fetch | - Fetches the latest content from all the rss feeds`

// + `* |/feed initiate| - initiates the rss feed subscription poller`

func getCommand() *model.Command {
	return &model.Command{
		Trigger:          "feed",
		DisplayName:      "RSSFeed",
		Description:      "Allows user to subscribe to an rss feed.",
		AutoComplete:     true,
		AutoCompleteDesc: "Available commands: list, subscribe, unsubscribe, help",
		AutoCompleteHint: "[command]",
	}
}

func getCommandResponse(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.COMMAND_RESPONSE_TYPE_IN_CHANNEL,
		Text:         text,
		Username:     botDisplayName,
		IconURL:      RSSFeedIconURL,
		Type:         model.POST_DEFAULT,
	}
}

func getCommandPrivate(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
		Text:         text,
		Username:     botDisplayName,
		IconURL:      RSSFeedIconURL,
		Type:         model.POST_DEFAULT,
	}
}

// ExecuteCommand will execute commands ...
func (p *RSSFeedPlugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	split := strings.Fields(args.Command)

	command := split[0]

	action := ""
	if len(split) > 1 {
		action = split[1]
	}

	param := ""
	if len(split) > 2 {
		param = split[2]
	}

	if command != "/feed" {
		return &model.CommandResponse{}, nil
	}

	switch action {
	case "subscribe", "sub":
		return p.handleSub(param, args), nil
	case "list":
		return p.handleList(param, args), nil
	case "unsubscribe", "unsub":
		return p.handleUnsub(param, args), nil
	case "fetch":
		return p.handleFetch(param, args), nil
	case "help":
		text := "###### Mattermost RSSFeed Plugin - Slash Command Help\n" + strings.ReplaceAll(CommandHelp, "|", "`")
		return getCommandPrivate(text), nil
	default:
		text := "###### Mattermost RSSFeed Plugin - Slash Command Help\n" + strings.ReplaceAll(CommandHelp, "|", "`")
		return getCommandPrivate(text), nil
	}
}

func (p *RSSFeedPlugin) handleSub(param string, args *model.CommandArgs) *model.CommandResponse {
	if !IsURL(param) {
		return getCommandPrivate("Argument is not a valid URL")
	}

	subList, err := p.getSubscriptions(args.ChannelId)

	if err != nil {
		return getCommandPrivate(fmt.Sprintf("Error: %s", err))
	}

	sub, _ := subList.find(param)

	if sub != nil {
		return getCommandPrivate(fmt.Sprintf("Already Subscribed to [%s](%s)", sub.Title, sub.URL))
	}

	go p.subscribe(context.Background(), param, args.ChannelId, args.UserId)

	return getCommandPrivate(fmt.Sprintf("Attempting to Subscribed to [url](%s)", param))
}

func (p *RSSFeedPlugin) handleUnsub(param string, args *model.CommandArgs) *model.CommandResponse {
	attachment, err := p.makeUnsubAttachments(args.ChannelId, 0)
	if err != nil {
		return getCommandPrivate(err.Error())
	}

	p.createBotPost("", args.ChannelId, args.UserId, []*model.SlackAttachment{attachment})

	return &model.CommandResponse{}
}

func (p *RSSFeedPlugin) handleFetch(param string, args *model.CommandArgs) *model.CommandResponse {
	p.processChannel(args.ChannelId)
	return getCommandResponse("Fetching Feeds in this Channel")
}

func (p *RSSFeedPlugin) handleList(param string, args *model.CommandArgs) *model.CommandResponse {
	hideURLs := p.getConfiguration().HideURLs
	subs, err := p.getSubscriptions(args.ChannelId)
	if err != nil {
		return getCommandPrivate(err.Error())
	}

	attachments := make([]*model.SlackAttachment, len(subs.Subscriptions))

	for i, sub := range subs.Subscriptions {
		title := sub.Title
		if !hideURLs {
			title = fmt.Sprintf("[%s](%s)", sub.Title, sub.URL)
		}
		user, err := p.API.GetUser(sub.UserID)
		username := "unknown"
		if err == nil {
			username = user.Username
		}
		attachments[i] = &model.SlackAttachment{
			Title: title,
			Text:  fmt.Sprintf("Subscribed by: %s", username),
			Color: sub.Color,
		}
	}

	grouped, err := p.groupAttachments(attachments)

	if err != nil {
		return getCommandPrivate(err.Error())
	}

	for i, group := range grouped {
		msg := ""
		if i == 0 {
			msg = "### Subscriptions:"
		}
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: args.ChannelId,
			Message:   msg,
			Props: model.StringInterface{
				"attachments": group,
			},
		}

		p.API.SendEphemeralPost(args.UserId, post)
	}

	return &model.CommandResponse{}
}

// thanks to https://stackoverflow.com/a/55551215/8781351
func IsURL(str string) bool {
	u, err := net_url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}
