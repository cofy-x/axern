//go:build !linux

package main

import "fmt"

func enableChildSubreaper() error {
	return fmt.Errorf("OCI init monitoring requires Linux")
}

func waitForChildExit(int) (int, error) {
	return 0, fmt.Errorf("OCI init monitoring requires Linux")
}
