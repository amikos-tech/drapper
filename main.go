package main

import (
	"context"
	"fmt"
	dapr "github.com/dapr/go-sdk/client"
	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/http"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

func daprInit() {
	cmd := exec.Command("dapr", "init")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error initializing Dapr: %v\n", err)
		return
	}
}

func startDaprSidecar(ctx context.Context, appID string, appPort int, daprPort int, daprGrpcPort int, componentsPath string, done chan bool) {
	cmd := exec.CommandContext(ctx, "dapr",
		"run",
		"--app-id", appID,
		"--app-port", fmt.Sprintf("%d", appPort),
		"--dapr-http-port", fmt.Sprintf("%d", daprPort),
		"--dapr-grpc-port", fmt.Sprintf("%d", daprGrpcPort),
		"--components-path", componentsPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		fmt.Printf("Error starting Dapr sidecar: %v\n", err)
		done <- false
		return
	} else {
		fmt.Println("Dapr sidecar started successfully")
		done <- true
	}

	// Wait for the command to finish
	err = cmd.Wait()
	if err != nil {
		fmt.Printf("Dapr sidecar stopped with error: %v %v\n", err, cmd.Stderr)

	}
}

func main() {
	done := make(chan bool)
	ctx, _ := context.WithCancel(context.Background())
	s := daprd.NewService(":8886")
	var wg sync.WaitGroup
	wg.Add(1)
	waitCh := make(chan any)
	// Create a mock subscription handler
	handler := func(ctx context.Context, e *common.TopicEvent) (retry bool, err error) {
		// Add your assertions here
		fmt.Printf("Received event: %s %v\n", e.Type, e.Data)
		waitCh <- e.Data
		return false, nil
	}
	err := s.AddTopicEventHandler(&common.Subscription{
		PubsubName: "pubsub",
		Topic:      "search-result",
		Route:      "/search",
	}, handler)
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		fmt.Printf("Starting server\n")
		if err := s.Start(); err != nil && err != http.ErrServerClosed {
			return
		}
	}()

	go startDaprSidecar(ctx, "myapp", 8886, 3500, 50001, "./components", done)

	// Wait for the Dapr sidecar to start
	if <-done {
		fmt.Println("Dapr sidecar is running in the background")
	} else {
		fmt.Println("Failed to start Dapr sidecar")
		return
	}
	daprClient, err := dapr.NewClient()

	err = daprClient.PublishEvent(context.Background(), "pubsub", "search-result", []byte("hello world111"), dapr.PublishEventWithMetadata(map[string]string{"cloudevent.type": "b"}))
	if err != nil {
		return
	}
	fmt.Println("Published event")
	timeout := time.After(30 * time.Second)
	select {
	case <-waitCh:
		defer s.Stop()
		wg.Done()
	case <-timeout:
		fmt.Println("Timeout")
		return
	}

	//time.Sleep(10 * time.Second)
	//fmt.Println("Stopping Dapr sidecar...")
	//
	//// Stop the Dapr sidecar
	////cancel()
	//
	//// Give some time to clean up
	//time.Sleep(2 * time.Second)
	//fmt.Println("Main application finished")
}
