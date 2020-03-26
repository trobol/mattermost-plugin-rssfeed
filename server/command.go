package main

import (
	"context"
	"fmt"
	URL "net/url"
	"strings"

	"errors"
	"github.com/mattermost/mattermost-server/mlog"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
)

// COMMAND_HELP is the text you see when you type /feed help
const COMMAND_HELP = `* |/feed subscribe url| - Connect your Mattermost channel to an rss feed 
 * |/feed list| - Lists the rss feeds you have subscribed to
 * |/feed unsubscribe url| - Unsubscribes the Mattermost channel from the rss feed`

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

func getCommandResponse(responseType, text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: responseType,
		Text:         text,
		Username:     botDisplayName,
		IconURL:      RSSFEED_ICON_URL,
		Type:         model.POST_DEFAULT,
	}
}

// ExecuteCommand will execute commands ...
func (p *RSSFeedPlugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {

	config := p.getConfiguration()
	split := strings.Fields(args.Command)
	command := split[0]
	parameters := []string{}
	action := ""
	if len(split) > 1 {
		action = split[1]
	}
	if len(split) > 2 {
		parameters = split[2:]
	}

	if command != "/feed" {
		return &model.CommandResponse{}, nil
	}

	private := model.COMMAND_RESPONSE_TYPE_EPHEMERAL

	normal := model.COMMAND_RESPONSE_TYPE_IN_CHANNEL
	if config.HideSubscribeMessage {
		normal = private
	}

	switch action {
	case "list":
		txt := "### Subscriptions in this channel\n"
		subscriptions, err := p.getSubscriptions()
		if err != nil {
			return getCommandResponse(private, err.Error()), nil
		}

		for _, value := range subscriptions.Subscriptions {
			if value.ChannelID == args.ChannelId {
				txt += fmt.Sprintf("* `%s`\n", value.URL)
			}
		}
		return getCommandResponse(private, txt), nil
	case "subscribe":

		url, err := parseUrlParam(&parameters)

		if err != nil {
			return getCommandResponse(private, "Invalid arguments: "+err.Error()), nil
		}

		if err := p.subscribe(context.Background(), args.ChannelId, url); err != nil {
			return getCommandResponse(private, fmt.Sprintf("Failed to subscribe: %s.", err.Error())), nil
		}

		return getCommandResponse(normal, fmt.Sprintf("Subscribed to %s.", url)), nil
	case "unsubscribe":

		url, err := parseUrlParam(&parameters)

		if err != nil {
			return getCommandResponse(private, "Invalid arguments: "+err.Error()), nil
		}

		if err := p.unsubscribe(args.ChannelId, url); err != nil {
			mlog.Error(err.Error())
			return getCommandResponse(private, "Encountered an error trying to unsubscribe. Please try again."), nil
		}

		return getCommandResponse(normal, fmt.Sprintf("Unsubscribed from %s.", url)), nil
	case "help":
		text := "###### Mattermost RSSFeed Plugin - Slash Command Help\n" + strings.Replace(COMMAND_HELP, "|", "`", -1)
		return getCommandResponse(private, text), nil
	default:
		text := "###### Mattermost RSSFeed Plugin - Slash Command Help\n" + strings.Replace(COMMAND_HELP, "|", "`", -1)
		return getCommandResponse(private, text), nil
	}
}

func parseUrlParam(parameters *[]string) (string, error) {
	if len(*parameters) == 0 {
		return "", errors.New("url not specified")
	} else if len(*parameters) > 1 {
		return "", errors.New("too many arguments")
	}

	url := (*parameters)[0]

	if !IsUrl(url) {
		return "", errors.New("url invalid")
	}

	return url, nil
}

//thanks to https://stackoverflow.com/a/55551215/8781351
func IsUrl(str string) bool {
	u, err := URL.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}
