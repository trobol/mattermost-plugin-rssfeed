package main

import (
	"testing"

	"github.com/stretchr/testify/mock"
)

type HTTPClientMock struct {
	mock.Mock
}

type ResponseMock struct {
	Body       string
	StatusCode int
}

var responses = [...]ResponseMock{
	{
		`<?xml version="1.0" encoding="UTF-8"?>
	<feed xmlns="http://www.w3.org/2005/Atom">
	  <title>Example</title>
	  <link rel="self" href="https://example.com/feed"/>
	  <link rel="alternate" href="https://example.com/alternate"/>
	  <id>https://example.com.edu/</id>
	  <icon>https://example.com/icon</icon>
	  <updated>2020-04-20T19:32:10-04:00</updated>
	  <author>
		<name>Example Author</name>
	  </author>
	  <generator uri="https://www.example.org/">Example Generator</generator>
	</feed>`,
		200,
	},
}

func TestFetchInfo(t *testing.T) {
	for _, response := range responses {
		mockClient := new(HTTPClientMock)

		mockClient.On("Do", mock.Anything).Return(response)
	}
}
