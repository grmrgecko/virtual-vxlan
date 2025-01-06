package main

import (
	"context"
	"fmt"
	"time"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
)

// The command for saving configuration.
type ConfigSaveCmd struct {
}

func (a *ConfigSaveCmd) Run() (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to save configuration.
	_, err = c.SaveConfig(ctx, &pb.Empty{})
	if err != nil {
		return
	}

	fmt.Println("Configuration saved")
	return
}

type ConfigReloadCmd struct {
}

func (a *ConfigReloadCmd) Run() (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to reload the configuration.
	_, err = c.ReloadConfig(ctx, &pb.Empty{})
	if err != nil {
		return
	}

	fmt.Println("Configuration reloaded")
	return
}

// Commands for managing the configuration.
type ConfigCmd struct {
	Save   ConfigSaveCmd   `cmd:""`
	Reload ConfigReloadCmd `cmd:""`
}
