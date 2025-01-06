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
	config := ReadConfig()
	err := ApplyConfig(config)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return new(pb.Empty), nil
}
