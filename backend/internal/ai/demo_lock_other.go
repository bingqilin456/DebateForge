//go:build !linux

package ai

import "os"

func lockDemo(string) (*os.File, error) { return nil, errDemoDenied }
