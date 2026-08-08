//go:build !linux

package nodeinventory

func storageMountFacts(string) (string, string) { return "", "" }
