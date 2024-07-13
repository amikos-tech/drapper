package dapr_wrapper

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
	EnsureDapr()
	eventReceived := make(chan bool)
	handler := func(ctx context.Context, e *common.TopicEvent) (retry bool, err error) {
		t.Logf("Received event: %s %v\n", e.Type, e.Data)
		require.Equal(t, map[string]interface{}{"data": "hello world"}, e.Data)
		eventReceived <- true
		return false, nil
	}

	closeEvent, err := RegisterEventHandler(ctx, "myapp", "pubsub", "search-result", "/search", "./components", handler)
	require.NoError(t, err)
	daprClient, err := dapr.NewClient()
	require.NoError(t, err, "Error creating Dapr client: %v\n", err)

	err = daprClient.PublishEvent(context.Background(), "pubsub", "search-result", []byte(`{"data": "hello world"}`), dapr.PublishEventWithMetadata(map[string]string{"cloudevent.type": "test"}))
	require.NoError(t, err, "Error publishing event: %v\n", err)
	t.Cleanup(func() {
		if closeEvent != nil {
			closeEvent()
		}
		cancel()
	})
	select {
	case <-eventReceived:
		t.Log("Event received successfully")
	case <-time.After(10 * time.Second): // Adjust timeout as necessary
		t.Fatalf("Timed out waiting for event")
	}

}

func TestDapprService(t *testing.T) {
	EnsureDapr()
	handler := func(ctx context.Context, in *common.InvocationEvent) (out *common.Content, derr error) {
		out = &common.Content{
			Data:        in.Data,
			ContentType: in.ContentType,
			DataTypeURL: in.DataTypeURL,
		}
		return
	}

	handlerTopic := func(ctx context.Context, e *common.TopicEvent) (retry bool, err error) {
		t.Logf("Received event: %s %v\n", e.Type, e.Data)
		return false, nil
	}

	service := &DaprService{
		AppID:        "myapp",
		AppPort:      8001,
		DaprHTTPPort: 3500,
		DaprGRPCPort: 50001,
		ServiceInvocationHandlers: map[string]common.ServiceInvocationHandler{
			"search": handler,
		},
		TopicEventHandlers: []TopicEventHandler{{
			Subscription: &common.Subscription{
				PubsubName: "pubsub",
				Topic:      "search-result",
				Route:      "/search-events",
			},
			Handler: handlerTopic,
		}},
		ResourcePath: "./components",
	}
	t.Cleanup(func() {
		err := service.Stop()
		if err != nil {
			t.Fatalf("Error stopping service: %v", err)
		}
	})
	daprClient, err := service.Start()
	require.NoError(t, err)

	content, err := daprClient.InvokeMethodWithContent(context.Background(), "myapp", "search", "POST", &dapr.DataContent{
		ContentType: "application/json",
		Data:        []byte(`{"data": "hello world"}`),
	})
	require.NoError(t, err, "Error invoking method: %v\n", err)
	err = daprClient.PublishEvent(context.Background(), "pubsub", "search-result", []byte(`{"data": "hello world"}`))
	require.NoError(t, err, "Error publishing event: %v\n", err)

	require.Equal(t, `{"data": "hello world"}`, string(content))
}
