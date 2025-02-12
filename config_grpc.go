package main

import (
	"context"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
	log "github.com/sirupsen/logrus"
)

// Save configuration to yaml file.
func (s *GRPCServer) SaveConfig(ctx context.Context, in *pb.Empty) (*pb.Empty, error) {
	log.Println("Saving configurations.")
	err := SaveConfig()
	if err != nil {
		return nil, err
	}
	return new(pb.Empty), nil
}

// Reload the configuration from the yaml file.
func (s *GRPCServer) ReloadConfig(ctx context.Context, in *pb.Empty) (*pb.Empty, error) {
	log.Println("Reloading configurations.")
	app.ApplyingConfig = true
	config := ReadConfig()
	err := ApplyConfig(config)
	app.ApplyingConfig = false
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return new(pb.Empty), nil
}

// Check if the config is being applied.
func (s *GRPCServer) IsApplyingConfig(ctx context.Context, in *pb.Empty) (*pb.IsApplyingConfigReply, error) {
	reply := new(pb.IsApplyingConfigReply)
	reply.IsApplying = app.ApplyingConfig
	return reply, nil
}
