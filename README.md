# Dapr integration testing wrapper

This a naive wrapper around Dapr for integration testing your Dapr applications.


## Installation

```bash
go get github.com/amikos-tech/darpper
```

### Assumptions

- Docker is installed on the machine (used for self-hosted Dapr)
- Dapr CLI is installed on the machine and on the PATH

## Usage

```go
package main

import (
	"context"
	dapr "github.com/dapr/go-sdk/client"
	common "github.com/dapr/go-sdk/service/common"
	"github.com/stretchr/testify/require"
	dwrap "github.com/amikos-tech/drapper"
	"testing"
	"time"
)

func TestEventH(t *testing.T) {
	dwrap.EnsureDapr()
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

	service := &dwrap.DaprService{
		AppID:        "myapp",
		AppPort:      8001,
		DaprHTTPPort: 3500,
		DaprGRPCPort: 50001,
		ServiceInvocationHandlers: map[string]common.ServiceInvocationHandler{
			"search": handler,
		},
		TopicEventHandlers: []dwrap.TopicEventHandler{{
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

```