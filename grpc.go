package main

import (
	"fmt"
	"net"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Allows go generate to compile the protobuf to golang.
//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ./vxlan/vxlan.proto

// GRPC server structure.
type GRPCServer struct {
	pb.UnimplementedVxlanServer
	RPCPath string
	server  *grpc.Server
}

// Start serving GRPC requests.
func (s *GRPCServer) Serve(li net.Listener) {
	err := s.server.Serve(li)
	if err != nil {
		log.Errorf("Error serving grpc: %v", err)
	}
}

// Stop GRPC server.
func (s *GRPCServer) Close() {
	s.server.Stop()
}

// Start GRPC server.
func NewGRPCServer(rpcPath string) (s *GRPCServer, err error) {
	// Verify another server doesn't exist.
	if app.grpcServer != nil {
		return nil, fmt.Errorf("grpc server is already running")
	}

	// Connect to RPC path.
	li, err := net.Listen("unix", rpcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket: %v", err)
	}

	// Setup server.
	s = new(GRPCServer)
	s.server = grpc.NewServer()

	// Register the vxlan service to this server.
	pb.RegisterVxlanServer(s.server, s)

	// Update the global app gRPC server.
	app.grpcServer = s

	// Start serving requests.
	go s.Serve(li)
	return s, nil
}

// Start a connection to the gRPC Server.
func NewGRPCClient() (c pb.VxlanClient, conn *grpc.ClientConn, err error) {
	// Read the minimal config.
	config := ReadMinimalConfig()

	// Start an gRPC client connection to the unix socket.
	conn, err = grpc.NewClient(fmt.Sprintf("unix:%s", config.RPCPath), grpc.WithTransportCredentials(insecure.NewCredentials()))

	// If connection is successful, provide client to the vxlan service.
	if err == nil {
		c = pb.NewVxlanClient(conn)
	}
	return
}
