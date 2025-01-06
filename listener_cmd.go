package main

import (
	"context"
	"fmt"
	"os"
	"time"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
	"github.com/jedib0t/go-pretty/v6/table"
)

// Command to list listeners
type ListenerListCmd struct{}

func (a *ListenerListCmd) Run() (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Read listener list.
	r, err := c.ListListeners(ctx, &pb.Empty{})
	if err != nil {
		return
	}

	// Verify there are listeners.
	if len(r.Listeners) == 0 {
		fmt.Println("No listeners running.")
		return
	}

	// Setup table for listener list.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Address", "Max Message Size", "Permanent"})

	// Add rows for each listener.
	for _, list := range r.Listeners {
		t.AppendRow([]interface{}{list.Name, list.Address, list.MaxMessageSize, list.Permanent})
	}

	// Print the table.
	t.Render()
	return
}

// Command to add an listener.
type ListenerAddCmd struct {
	Address        string `help:"Bind address (':4789' or '10.0.0.2:4789')"  required:""`
	MaxMessageSize int32  `help:"Max UDP message size" default:"1500"`
	Permanent      bool   `help:"Should listener be saved to disk?"`
}

func (a *ListenerAddCmd) Run(l *ListenerCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup listener config.
	listener := &pb.Listener{
		Name:           l.name(),
		Address:        a.Address,
		MaxMessageSize: a.MaxMessageSize,
		Permanent:      a.Permanent,
	}

	// Attempt to add an listener.
	_, err = c.AddListener(ctx, listener)
	if err != nil {
		return
	}

	fmt.Println("Added listener.")
	return
}

// Command to remove an listener.
type ListenerRemoveCmd struct{}

func (a *ListenerRemoveCmd) Run(l *ListenerCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to remove the listener.
	listener := &pb.ListenerRequestWithName{Name: l.name()}
	_, err = c.RemoveListener(ctx, listener)
	if err != nil {
		return
	}

	fmt.Println("Removed listener.")
	return
}

// Command to set listener's max message size.
type ListenerSetMaxMessageSizeCmd struct {
	Size int32 `help:"Max UDP message size" default:"1500"`
}

func (a *ListenerSetMaxMessageSizeCmd) Run(l *ListenerCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt tp update listener's max message size.
	listener := &pb.ListenerMaxMessageSizeRequest{
		Name: l.name(),
		Size: a.Size,
	}
	_, err = c.SetListenerMaxMessageSize(ctx, listener)
	if err != nil {
		return
	}

	fmt.Println("Updated max message size.")
	return
}

// Commands managing listeners.
type ListenerCmd struct {
	List ListenerListCmd `cmd:""`
	Name struct {
		Name              string                       `arg:"" help:"Listener name" required:""`
		Add               ListenerAddCmd               `cmd:""`
		Remove            ListenerRemoveCmd            `cmd:""`
		SetMaxMessageSize ListenerSetMaxMessageSizeCmd `cmd:""`
		Interface         InterfaceCmd                 `cmd:""`
	} `arg:""`
}

// Returns the listener name.
func (l *ListenerCmd) name() string {
	return l.Name.Name
}
