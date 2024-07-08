package main

import (
	"context"
	dapr "github.com/dapr/go-sdk/client"
	"github.com/dapr/go-sdk/service/common"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEventH(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Create a mock subscription handler
	eventReceived := make(chan bool)
	handler := func(ctx context.Context, e *common.TopicEvent) (retry bool, err error) {
		t.Logf("Received event: %s %v\n", e.Type, e.Data)
		require.Equal(t, map[string]interface{}{"data": "hello world"}, e.Data)
		eventReceived <- true
		return false, nil
	}

	closeEvent, err := RegisterEventHandler(ctx, "pubsub", "search-result", "/search", handler)
	require.NoError(t, err)
	daprClient, err := dapr.NewClient()
	require.NoError(t, err, "Error creating Dapr client: %v\n", err)

	err = daprClient.PublishEvent(context.Background(), "pubsub", "search-result", []byte(`{"data": "hello world"}`), dapr.PublishEventWithMetadata(map[string]string{"cloudevent.type": "test"}))
	require.NoError(t, err, "Error publishing event: %v\n", err)
	select {
	case <-eventReceived:
		t.Log("Event received successfully")
		if closeEvent != nil {
			closeEvent()
		}
		cancel()
	case <-time.After(10 * time.Second): // Adjust timeout as necessary
		if closeEvent != nil {
			closeEvent()
		}
		cancel()
		t.Fatalf("Timed out waiting for event")
	}

}
