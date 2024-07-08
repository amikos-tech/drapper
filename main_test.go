package main

import (
	"context"
	dapr "github.com/dapr/go-sdk/client"
	"testing"
)

func TestP(t *testing.T) {
	daprClient, err := dapr.NewClient()
	if err != nil {
		t.Errorf("Error: %v\n", err)
	}
	t.Logf("daprClient: %v\n", daprClient)
	err = daprClient.PublishEvent(context.Background(), "pubsub", "search-result", []byte("hello world"))
	if err != nil {
		t.Errorf("Error: %v\n", err)
	}
}
