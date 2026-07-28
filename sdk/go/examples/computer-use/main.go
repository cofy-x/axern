package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	axern "github.com/cofy-x/axern/sdk/go"
	"github.com/cofy-x/axern/sdk/go/examples/internal/exampleutil"
)

func main() {
	config := exampleutil.Flags()
	if os.Getenv("AXERN_TEMPLATE_ID") == "" {
		config.TemplateID = "desktop-base"
	}
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := exampleutil.NewClient(ctx, config)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	sandbox, err := exampleutil.StartSandbox(ctx, client, config)
	if err != nil {
		log.Fatal(err)
	}
	defer sandbox.Close(context.Background())

	status, err := sandbox.ComputerUseStatus(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("computer_use available=%t display=%s backend=%s\n", status.Available, status.Display, status.Backend)
	for _, dependency := range status.Dependencies {
		fmt.Printf("dependency %s: available=%t reason=%s\n", dependency.Name, dependency.Available, dependency.Reason)
	}
	if !status.Available {
		log.Fatalf("computer_use unavailable: %s", status.Reason)
	}

	display, err := sandbox.ComputerUseDisplay(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("display %dx%d\n", display.Width, display.Height)

	screenshot, err := sandbox.ComputerUseScreenshot(ctx, axern.ComputerUseScreenshotOptions{
		Region: &axern.ComputerUseRegion{X: 0, Y: 0, Width: min32(display.Width, 320), Height: min32(display.Height, 180)},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("computer-use-screenshot.png", screenshot.Data, 0o600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote computer-use-screenshot.png (%s, %d bytes)\n", screenshot.ContentType, len(screenshot.Data))

	if err := sandbox.ComputerUseMouse(ctx, axern.ComputerUseMouseOptions{Action: "move", X: 10, Y: 10}); err != nil {
		log.Fatal(err)
	}
	if err := sandbox.ComputerUseKeyboard(ctx, axern.ComputerUseKeyboardOptions{Key: "Escape"}); err != nil {
		log.Fatal(err)
	}
}

func min32(a int32, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
