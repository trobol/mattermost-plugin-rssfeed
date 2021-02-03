# RSSFeed Plugin [![CircleCI](https://circleci.com/gh/trobol/mattermost-plugin-rssfeed.svg?style=svg)](https://circleci.com/gh/trobol/mattermost-plugin-rssfeed)

This plugin allows a user to subscribe a channel to an RSS (Version 2 only) or an Atom Feed.

- Version 0.1.0+ requires Mattermost 5.10
- Version < 0.1.0 requires Mattermost 5.6

## Getting Started
Upload tar.gz to Mattermost using the plugin screen.

To use the plugin, navigate to the channel you want subscribed and use the following commands:
```
/feed help                  // to see the help menu
/feed sub <url>             // to subscribe the channel to an rss feed
/feed unsub                 // to unsubscribe the channel from an rss feed
/feed list                  // to list the feeds the channel is subscribed to
/feed fetch                 // force update all feeds in channel
```

## Developers
Clone the repository:
```
git clone https://github.com/trobol/mattermost-plugin-rssfeed
```

Build your plugin:
```
make dist
```

This will produce a single plugin file (with support for multiple architectures) for upload to your Mattermost server:

```
rssfeed.0.0.1.tar.gz
```
