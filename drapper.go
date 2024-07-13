package dapr_wrapper

import (
	"bufio"
	"context"
	"fmt"
	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/http"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// daprInit initializes Dapr
func daprInit() { //nolint:deadcode,unused
	cmd := exec.Command("dapr", "init", "--dev")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error initializing Dapr: %v\n", err)
		return
	}
}

func daprUninstall() {
	cmd := exec.Command("dapr", "uninstall", "--all")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error uninstalling Dapr: %v\n", err)
		return
	}
}

func EnsureDapr() {
	cmd := exec.Command("dapr", "version")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Dapr is not installed. Please install dapr CLI first: %v\n", err)

	}
	daprUninstall()
	daprInit()
}

func startDaprSidecar(ctx context.Context, appID string, appPort int, daprPort int, daprGrpcPort int, componentsPath string, done chan int) {
	cmd := exec.CommandContext(ctx, "dapr",
		"run",
		"--app-id", appID,
		"--app-port", fmt.Sprintf("%d", appPort),
		"--dapr-http-port", fmt.Sprintf("%d", daprPort),
		"--dapr-grpc-port", fmt.Sprintf("%d", daprGrpcPort),
		"--components-path", componentsPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Ensure the command runs in its own process group
	}

	//cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Error getting stdout pipe: %v\n", err)
		return
	}
	err = cmd.Start()

	if err != nil {
		fmt.Printf("Error starting Dapr sidecar: %v\n", err)
		done <- -1
		return
	} else {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Printf("%s\n", line)
			// Check if the line contains the specific log message
			if strings.Contains(line, "You're up and running") {
				fmt.Println("Dapr sidecar started successfully")
				done <- cmd.Process.Pid
			}
		}

	}

	// Wait for the command to finish
	err = cmd.Wait()
	if err != nil {
		fmt.Printf("Dapr sidecar stopped with error: %v %v\n", err, cmd.Stderr)

	}
}

func RegisterEventHandler(ctx context.Context, pubsubName string, topic string, route string, handler func(ctx context.Context, e *common.TopicEvent) (retry bool, err error)) (cancel func(), err error) {
	s := daprd.NewService(":8886") //TODO get random free port
	// Create a mock subscription handler
	err = s.AddTopicEventHandler(&common.Subscription{
		PubsubName: pubsubName,
		Topic:      topic,
		Route:      route,
	}, handler)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Printf("Starting consumer service\n")
		if err := s.Start(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("error: %v", err)
			return
		}
		fmt.Println("Consumer service stopped")
	}()
	daprStarted := make(chan int)
	go startDaprSidecar(ctx, "myapp", 8886, 3500, 50001, "./components", daprStarted)
	var sidecarpid int = -1
	if x := <-daprStarted; x > 0 {
		fmt.Println("Dapr sidecar started successfully", x)
		sidecarpid = x
	} else {
		return nil, fmt.Errorf("Error starting Dapr sidecar")
	}
	return func() {
		err := s.GracefulStop()
		if err != nil {
			fmt.Printf("Error stopping server: %v\n", err)
		}
		fmt.Println("Stopping ....")
		if sidecarpid > 0 {
			err := syscall.Kill(-sidecarpid, syscall.SIGTERM)
			if err != nil {
				fmt.Printf("Error stopping Dapr sidecar proccess: %d - %v\n", sidecarpid, err)
			}
		}
	}, nil
}
