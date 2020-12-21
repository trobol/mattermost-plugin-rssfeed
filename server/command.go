package main

import (
	"context"
	"fmt"
	URL "net/url"
	"strconv"
	"strings"

	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
)

// CommandHelp is the text you see when you type /feed help
const CommandHelp = `* |/feed subscribe [url]| - Connect your Mattermost channel to an rss feed 
* |/feed list| - Lists the rss feeds you have subscribed to
* |/feed unsubscribe [url]| - Unsubscribes the Mattermost channel from the rss feed
* |/feed fetch [url]| - Fetches the latest content from the rss feed`

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

	return getCommandPrivate(fmt.Sprintf("Attempting to Subscribed to %s", param))
}

func (p *RSSFeedPlugin) handleUnsub(param string, args *model.CommandArgs) *model.CommandResponse {
	index, err := strconv.Atoi(param)

	if err == nil {
		err = p.unsubscribeFromIndex(args.ChannelId, index)
	} else {
		if !IsURL(param) {
			return getCommandPrivate("Argument is not a valid URL or index")
		}
		err = p.unsubscribeFromURL(args.ChannelId, param)
	}

	if err != nil {
		return getCommandPrivate("Error: " + err.Error())
	}

	return getCommandPrivate("Successfully Unsubscribed")
}

func (p *RSSFeedPlugin) handleFetch(param string, args *model.CommandArgs) *model.CommandResponse {
	subList, err := p.getSubscriptions(args.ChannelId)
	if err != nil {
		p.API.LogError(err.Error())
	}
	var sub *Subscription = nil

	index, err := strconv.Atoi(param)

	if err == nil {
		if index < 0 || index > len(subList.Subscriptions) {
			return getCommandPrivate("index out of range")
		}
		sub = subList.Subscriptions[index]
	} else {
		if !IsURL(param) {
			return getCommandPrivate("Argument is not a valid URL or index")
		}
		sub, _ = subList.find(param)
		if sub == nil {
			return getCommandPrivate("Channel not subscribed")
		}
	}

	go func(channelID string, sub *Subscription, subList *SubscriptionList) {
		p.processSubscription(channelID, sub)
		err := p.storeSubscriptions(channelID, subList)
		if err != nil {
			p.API.LogError(err.Error())
		}
	}(args.ChannelId, sub, subList)

	return getCommandResponse("Fetching " + sub.Title)
}

func (p *RSSFeedPlugin) handleList(param string, args *model.CommandArgs) *model.CommandResponse {
	hideURLs := p.getConfiguration().HideURLs
	txt := "### Subscriptions in this channel\n"

	subscriptions, err := p.getSubscriptions(args.ChannelId)
	if err != nil {
		return getCommandPrivate(err.Error())
	}

	for index, sub := range subscriptions.Subscriptions {
		if hideURLs {
			txt += fmt.Sprintf("%d: %s\n", index, sub.Title)
		} else {
			txt += fmt.Sprintf("%d: [%s](%s)\n", index, sub.Title, sub.URL)
		}
	}
	return getCommandPrivate(txt)
}

// thanks to https://stackoverflow.com/a/55551215/8781351
func IsURL(str string) bool {
	u, err := URL.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}
