package main

import (
	"context"
	"fmt"
	"os"
	"time"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
	"github.com/jedib0t/go-pretty/v6/table"
)

// Command to list interfaces on an listener.
type InterfaceListCmd struct{}

func (a *InterfaceListCmd) Run(l *ListenerCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Pull list of interface on the requested listener.
	listener := &pb.ListenerRequestWithName{Name: l.name()}
	r, err := c.ListInterfaces(ctx, listener)
	if err != nil {
		return
	}

	// Setup table for interfaces.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "VNI", "MTU", "Permanent"})

	// Add row for each interface.
	for _, ifce := range r.Interfaces {
		t.AppendRow([]interface{}{ifce.Name, ifce.Vni, ifce.Mtu, ifce.Permanent})
	}

	// Render the table.
	t.Render()
	return
}

// Command to add interface to an listener.
type InterfaceAddCmd struct {
	VNI       uint32 `help:"VXLAN VNI" required:""`
	MTU       int32  `help:"MTU of interface, 0 will result in calculated value" default:"0"`
	Permanent bool   `help:"Should listener be saved to disk?"`
}

func (a *InterfaceAddCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup interface request.
	ifce := &pb.AddInterfaceRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Vni:          a.VNI,
		Mtu:          a.MTU,
		Permanent:    a.Permanent,
	}

	// Attempt to add the interface.
	_, err = c.AddInterface(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Added interface to listener.")
	return
}

// Command to remove an interface on an listener.
type InterfaceRemoveCmd struct{}

func (a *InterfaceRemoveCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup interface request.
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}

	// Attempt to add the interface.
	_, err = c.RemoveInterface(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Removed interface from listener.")
	return
}

// Command to set interface's MTU.
type InterfaceSetMTUCmd struct {
	MTU int32 `help:"Interface MTU value (0 will result in calculated value)" default:"0"`
}

func (a *InterfaceSetMTUCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to update interface's MTU.
	ifce := &pb.InterfaceMTURequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Mtu:          a.MTU,
	}
	_, err = c.SetInterfaceMTU(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Updated MTU.")
	return
}

// Command to set interface's MAC address.
type InterfaceSetMACAddressCmd struct {
	MAC string `help:"The MAC Address to set"`
}

func (a *InterfaceSetMACAddressCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to update interface's MAC address.
	ifce := &pb.InterfaceMACAddressRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Mac:          a.MAC,
	}
	_, err = c.SetInterfaceMACAddress(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Updated MAC Address.")
	return
}

// Command to get interface's MAC address.
type InterfaceGetMACAddressCmd struct{}

func (a *InterfaceGetMACAddressCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to get interface's MAC address.
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	r, err := c.GetInterfaceMACAddress(ctx, ifce)
	if err != nil {
		return
	}

	// Setup table for MAC listing.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"MAC Address"})

	// Add row for the MAC address.
	t.AppendRow([]interface{}{r.Mac})

	// Render the table.
	t.Render()
	return
}

// Command to set interface's IP addresses.
type InterfaceSetIPAddressesCmd struct {
	IPAddress []string `help:"The IP address(es) to set"`
}

func (a *InterfaceSetIPAddressesCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to update interface's IP address(es).
	ifce := &pb.InterfaceIPAddressesRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		IpAddress:    a.IPAddress,
	}
	_, err = c.SetInterfaceIPAddresses(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Updated IP Address(es).")
	return
}

// Command to get interface's MAC Address.
type InterfaceGetIPAddressesCmd struct{}

func (a *InterfaceGetIPAddressesCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to get interface's IP address(es).
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	r, err := c.GetInterfaceIPAddresses(ctx, ifce)
	if err != nil {
		return
	}

	// Setup table for IP list.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"IP Address"})

	// Add row for the IP address(es).
	for _, ipAddress := range r.IpAddress {
		t.AppendRow([]interface{}{ipAddress})
	}

	// Render the table.
	t.Render()
	return
}

// Command to add an MAC entry to an interface.
type InterfaceAddMACEntryCmd struct {
	MAC         string `help:"MAC address to route (00:00:00:00:00:00 is treated as default route)" default:"00:00:00:00:00:00"`
	Destination string `help:"The IP address to send vxlan traffic" required:""`
	Permanent   bool   `help:"Should the MAC entry be saved to disk?"`
}

func (a *InterfaceAddMACEntryCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to add MAC entry to an interface.
	ifce := &pb.InterfaceMacEntryRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Mac:          a.MAC,
		Destination:  a.Destination,
		Permanent:    a.Permanent,
	}
	_, err = c.InterfaceAddMACEntry(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Added MAC entry.")
	return
}

// Command to remove an MAC entry from an interface.
type InterfaceRemoveMACEntryCmd struct {
	MAC         string `help:"MAC address" required:""`
	Destination string `help:"The IP address" required:""`
}

func (a *InterfaceRemoveMACEntryCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to remove an MAC entry from an interface.
	ifce := &pb.InterfaceRemoveMacEntryRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Mac:          a.MAC,
		Destination:  a.Destination,
	}
	_, err = c.InterfaceRemoveMACEntry(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Removed MAC entry.")
	return
}

// Command to get MAC entries on an interface.
type InterfaceGetMACEntriesCmd struct{}

func (a *InterfaceGetMACEntriesCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to get MAC entries on an interface.
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	r, err := c.InterfaceGetMACEntries(ctx, ifce)
	if err != nil {
		return
	}

	// Setup table for MAC entries.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"MAC Address", "Destination", "Permanent"})

	// Add rows for entries.
	for _, ent := range r.Entries {
		t.AppendRow([]interface{}{ent.Mac, ent.Destination, ent.Permanent})
	}

	// Render the table.
	t.Render()
	return
}

// Command to remove all mac entries from an interface.
type InterfaceFlushMACTableCmd struct{}

func (a *InterfaceFlushMACTableCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to flush the MAC entry table on an interface.
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	_, err = c.InterfaceFlushMACTable(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Flushed MAC entry table.")
	return
}

// Command to add a static route to an interface.
type InterfaceAddStaticRouteCmd struct {
	Destination string `help:"The CIDR of the destination network" required:""`
	Gateway     string `help:"The IP address to route traffic via." required:""`
	Metric      int    `help:"Metric value to set route priority." required:""`
	Permanent   bool   `help:"Should the static route be saved to disk?"`
}

func (a *InterfaceAddStaticRouteCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to add static route to an interface.
	ifce := &pb.InterfaceAddStaticRouteRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Destination:  a.Destination,
		Gateway:      a.Gateway,
		Metric:       int32(a.Metric),
		Permanent:    a.Permanent,
	}
	_, err = c.InterfaceAddStaticRoute(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Added static route.")
	return
}

// Command to remove a static route from an interface.
type InterfaceRemoveStaticRouteCmd struct {
	Destination string `help:"The CIDR of the destination network" required:""`
	Gateway     string `help:"The IP address to route traffic via." required:""`
}

func (a *InterfaceRemoveStaticRouteCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to remove a static route from an interface.
	ifce := &pb.InterfaceRemoveStaticRouteRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Destination:  a.Destination,
		Gateway:      a.Gateway,
	}
	_, err = c.InterfaceRemoveStaticRoute(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Removed static route.")
	return
}

// Command to get static routes on an interface.
type InterfaceGetStaticRoutesCmd struct{}

func (a *InterfaceGetStaticRoutesCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to get static routes on an interface.
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	r, err := c.InterfaceGetStaticRoutes(ctx, ifce)
	if err != nil {
		return
	}

	// Setup table for static routes.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Destination", "Gateway", "Metric", "Permanent"})

	// Add rows for entries.
	for _, ent := range r.Routes {
		t.AppendRow([]interface{}{ent.Destination, ent.Gateway, ent.Metric, ent.Permanent})
	}

	// Render the table.
	t.Render()
	return
}

// Command to add an ARP entry to an interface.
type InterfaceAddStaticARPEntryCmd struct {
	Address   string `help:"IP address" required:""`
	MAC       string `help:"MAC address" required:""`
	Permanent bool   `help:"Should the ARP entry be saved to disk?"`
}

func (a *InterfaceAddStaticARPEntryCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to add an static ARP entry to an interface.
	ifce := &pb.InterfaceARPEntryRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Address:      a.Address,
		Mac:          a.MAC,
		Permanent:    a.Permanent,
	}
	_, err = c.InterfaceAddStaticARPEntry(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Added static ARP entry.")
	return
}

// Command to remove an ARP entry from an interface.
type InterfaceRemoveARPEntryCmd struct {
	Address string `help:"IP address" required:""`
}

func (a *InterfaceRemoveARPEntryCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
	// Connect to GRPC.
	c, conn, err := NewGRPCClient()
	if err != nil {
		return
	}
	defer conn.Close()

	// Setup call timeout of 10 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to remove an ARP entry from a table.
	ifce := &pb.InterfaceRemoveARPEntryRequest{
		ListenerName: l.name(),
		Name:         i.name(),
		Address:      a.Address,
	}
	_, err = c.InterfaceRemoveARPEntry(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Removed ARP entry.")
	return
}

// Command to get ARP entries on an interface.
type InterfaceGetARPEntriesCmd struct{}

func (a *InterfaceGetARPEntriesCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
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
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	r, err := c.InterfaceGetARPEntries(ctx, ifce)
	if err != nil {
		return
	}

	// Setup table for interfaces.
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"IP Address", "MAC Address", "Expires", "Permanent"})

	// Add row for the IP address(es).
	for _, ent := range r.Entries {
		t.AppendRow([]interface{}{ent.Address, ent.Mac, ent.Expires, ent.Permanent})
	}

	// Render the table.
	t.Render()
	return
}

// Command to remove all ARP entries from an interface.
type InterfaceFlushARPTableCmd struct{}

func (a *InterfaceFlushARPTableCmd) Run(l *ListenerCmd, i *InterfaceCmd) (err error) {
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
	ifce := &pb.InterfaceRequestWithName{
		ListenerName: l.name(),
		Name:         i.name(),
	}
	_, err = c.InterfaceFlushARPTable(ctx, ifce)
	if err != nil {
		return
	}

	fmt.Println("Flushed ARP entry table.")
	return
}

// Commands managing listeners.
type InterfaceCmd struct {
	List InterfaceListCmd `cmd:""`
	Name struct {
		Name              string                        `arg:"" help:"Interface name" required:""`
		Add               InterfaceAddCmd               `cmd:""`
		Remove            InterfaceRemoveCmd            `cmd:""`
		SetMTU            InterfaceSetMTUCmd            `cmd:""`
		SetMACAddress     InterfaceSetMACAddressCmd     `cmd:""`
		GetMACAddress     InterfaceGetMACAddressCmd     `cmd:""`
		SetIPAddresses    InterfaceSetIPAddressesCmd    `cmd:""`
		GetIPAddresses    InterfaceGetIPAddressesCmd    `cmd:""`
		AddMACEntry       InterfaceAddMACEntryCmd       `cmd:""`
		RemoveMACEntry    InterfaceRemoveMACEntryCmd    `cmd:""`
		GetMACEntries     InterfaceGetMACEntriesCmd     `cmd:""`
		FlushMACTable     InterfaceFlushMACTableCmd     `cmd:""`
		AddStaticRoute    InterfaceAddStaticRouteCmd    `cmd:""`
		RemoveStaticRoute InterfaceRemoveStaticRouteCmd `cmd:""`
		GetStaticRoutes   InterfaceGetStaticRoutesCmd   `cmd:""`
		AddStaticARPEntry InterfaceAddStaticARPEntryCmd `cmd:""`
		RemoveARPEntry    InterfaceRemoveARPEntryCmd    `cmd:""`
		GetARPEntries     InterfaceGetARPEntriesCmd     `cmd:""`
		FlushARPTable     InterfaceFlushARPTableCmd     `cmd:""`
	} `arg:""`
}

// Returns the interface name.
func (i *InterfaceCmd) name() string {
	return i.Name.Name
}
