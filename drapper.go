package dapr_wrapper

import (
	"bufio"
	"context"
	"fmt"
	dapr "github.com/dapr/go-sdk/client"
	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/http"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// daprInit initializes Dapr
func daprInit() { //nolint:deadcode,unused
	cmd := exec.Command("dapr", "init")
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

func RegisterEventHandler(ctx context.Context, appID string, pubsubName string, topic string, route string, resourcePath string, handler func(ctx context.Context, e *common.TopicEvent) (retry bool, err error)) (cancel func(), err error) {
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
	go startDaprSidecar(ctx, appID, 8886, 3500, 50001, resourcePath, daprStarted)
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

type TopicEventHandler struct {
	Subscription *common.Subscription
	Handler      func(ctx context.Context, e *common.TopicEvent) (retry bool, err error)
}

type DaprService struct {
	AppID                     string
	AppPort                   int
	DaprHTTPPort              int
	DaprGRPCPort              int
	Command                   *exec.Cmd
	ServiceInvocationHandlers map[string]common.ServiceInvocationHandler
	TopicEventHandlers        []TopicEventHandler
	Service                   common.Service
	Wg                        sync.WaitGroup
	ResourcePath              string
}

func (s *DaprService) Start() (dapr.Client, error) {
	errChan := make(chan error, 1)

	service := daprd.NewService(fmt.Sprintf(":%d", s.AppPort))
	s.Service = service

	for handle, serviceHandler := range s.ServiceInvocationHandlers {
		if err := service.AddServiceInvocationHandler(handle, serviceHandler); err != nil {
			errChan <- fmt.Errorf("failed to add handler %s: %v", s.AppID, err)
		}
	}
	for _, topicHandler := range s.TopicEventHandlers {
		if err := service.AddTopicEventHandler(topicHandler.Subscription, topicHandler.Handler); err != nil {
			errChan <- fmt.Errorf("failed to add topic handler %s: %v", s.AppID, err)
		}
	}

	s.Wg.Add(1)
	go func(s *DaprService, daprService common.Service) {
		defer s.Wg.Done()
		fmt.Printf("Starting service worker...\n")
		if err := daprService.Start(); err != nil {
			errChan <- fmt.Errorf("failed to start service %s: %v", s.AppID, err)
		}
	}(s, service)

	select {
	case e := <-errChan:
		return nil, fmt.Errorf("failed to start Dapr service %s, %v", s.AppID, e)
	case <-time.After(2 * time.Second): // Adjust timeout as necessary
		fmt.Printf("OK") // things should start within 2 sec - this is brittle but works for now
	}
	extraArgs := []string{
		"run",
		"--app-id", s.AppID,
		"--app-port", fmt.Sprintf("%d", s.AppPort),
		"--dapr-http-port", fmt.Sprintf("%d", s.DaprHTTPPort),
		"--dapr-grpc-port", fmt.Sprintf("%d", s.DaprGRPCPort),
	}
	if s.ResourcePath != "" {
		extraArgs = append(extraArgs, "--resources-path", s.ResourcePath)
	}
	s.Command = exec.Command("dapr",
		extraArgs...,
	)

	s.Command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Ensure the command runs in its own process group
	}

	//cmd.Stdout = os.Stdout
	s.Command.Stderr = os.Stderr

	stdout, err := s.Command.StdoutPipe()
	if err != nil {
		fmt.Printf("Error getting stdout pipe: %v\n", err)
		return nil, err
	}
	err = s.Command.Start()
	if err != nil {
		fmt.Printf("Error starting Dapr sidecar: %v\n", err)
		return nil, err
	} else {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			//fmt.Printf("%s\n", line)
			// Check if the line contains the specific log message
			if strings.Contains(line, "You're up and running") {
				fmt.Println("Dapr sidecar started successfully")
				break
			}
		}
	}
	fmt.Printf("Waiting for proc")
	serrChan := make(chan error, 1)
	s.Wg.Add(1)
	go func() {
		defer s.Wg.Done()
		err = s.Command.Wait()
		if err != nil {
			fmt.Printf("Dapr sidecar stopped with error: %v %v\n", err, s.Command.Stderr)
			serrChan <- err
		}
	}()
	fmt.Printf("Process started\n")
	select {
	case e := <-serrChan:
		return nil, fmt.Errorf("failed to start Dapr service %s, %v\n", s.AppID, e)
	case <-time.After(2 * time.Second): // Adjust timeout as necessary
		fmt.Printf("OK waiting for proc to terminate\n") // things should start within 2 sec - this is brittle but works for now
	}
	daprClient, err := dapr.NewClientWithPort(fmt.Sprintf("%d", s.DaprGRPCPort))
	if err != nil {
		stopError := s.Stop()
		return nil, fmt.Errorf("failed to create Dapr client: %v\nStop error: %v", err, stopError)
	}
	return daprClient, nil
}

func (s *DaprService) Stop() error {
	if s.Command != nil && s.Command.Process != nil {
		err := syscall.Kill(-s.Command.Process.Pid, syscall.SIGTERM)
		if err != nil {
			fmt.Printf("Error stopping Dapr sidecar proccess: %d - %v\n", s.Command.Process.Pid, err)
		}
	}
	return nil
}
