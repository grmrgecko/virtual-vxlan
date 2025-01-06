package main

import (
	"context"
	"fmt"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
)

// Provide list of listeners.
func (s *GRPCServer) ListListeners(ctx context.Context, in *pb.Empty) (*pb.ListListenersReply, error) {
	reply := new(pb.ListListenersReply)
	app.Net.Lock()
	for _, listener := range app.Net.Listeners {
		list := &pb.Listener{
			Name:           listener.Name,
			Address:        listener.PrettyName(),
			MaxMessageSize: int32(listener.net.maxMessageSize),
			Permanent:      listener.Permanent,
		}
		reply.Listeners = append(reply.Listeners, list)
	}
	app.Net.Unlock()
	return reply, nil
}

// Add listener.
func (s *GRPCServer) AddListener(ctx context.Context, in *pb.Listener) (*pb.Empty, error) {
	_, err := NewListener(in.Name, in.Address, int(in.MaxMessageSize), in.Permanent)
	if err != nil {
		return nil, err
	}
	return new(pb.Empty), nil
}

// Find listener.
func (s *GRPCServer) FindListener(name string) (list *Listener, err error) {
	// Find existing listener.
	app.Net.Lock()
	for _, listener := range app.Net.Listeners {
		if listener.Name == name {
			list = listener
			break
		}
	}
	app.Net.Unlock()

	// If no listener found, error.
	if list == nil {
		return nil, fmt.Errorf("no listener with name: %s", name)
	}
	return
}

// Remove listener.
func (s *GRPCServer) RemoveListener(ctx context.Context, in *pb.ListenerRequestWithName) (*pb.Empty, error) {
	list, err := s.FindListener(in.Name)
	if err != nil {
		return nil, err
	}

	// Try and close the listener.
	err = list.Close()
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Set listener's max message size.
func (s *GRPCServer) SetListenerMaxMessageSize(ctx context.Context, in *pb.ListenerMaxMessageSizeRequest) (*pb.Empty, error) {
	list, err := s.FindListener(in.Name)
	if err != nil {
		return nil, err
	}

	// Set the max message size for the listener.
	list.SetMaxMessageSize(int(in.Size))

	return new(pb.Empty), nil
}

// Get listener's max message size.
func (s *GRPCServer) GetListenerMaxMessageSize(ctx context.Context, in *pb.ListenerRequestWithName) (*pb.ListenerMaxMessageSizeReply, error) {
	list, err := s.FindListener(in.Name)
	if err != nil {
		return nil, err
	}

	// Get the max message size for the listener.
	reply := &pb.ListenerMaxMessageSizeReply{
		Size: int32(list.MaxMessageSize()),
	}

	return reply, nil
}
