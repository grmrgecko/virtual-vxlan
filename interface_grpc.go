package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	pb "github.com/grmrgecko/virtual-vxlan/vxlan"
)

// List interfaces on a listener.
func (s *GRPCServer) ListInterfaces(ctx context.Context, in *pb.ListenerRequestWithName) (*pb.ListInterfacesReply, error) {
	reply := new(pb.ListInterfacesReply)
	app.Net.Lock()
	for _, listener := range app.Net.Listeners {
		if listener.Name == in.Name {
			listener.net.RLock()
			for _, iface := range listener.net.interfaces {
				ifce := &pb.Interface{
					Name:      iface.Name(),
					Vni:       iface.VNI(),
					Mtu:       int32(iface.MTU()),
					Permanent: iface.Permanent,
				}
				reply.Interfaces = append(reply.Interfaces, ifce)
			}
			listener.net.RUnlock()
			break
		}
	}
	app.Net.Unlock()
	return reply, nil
}

// Add interface to the listener.
func (s *GRPCServer) AddInterface(ctx context.Context, in *pb.AddInterfaceRequest) (*pb.Empty, error) {
	// Find listener.
	list, err := s.FindListener(in.ListenerName)
	if err != nil {
		return nil, err
	}

	// Add interface to listener.
	_, err = NewInterface(in.Name, in.Vni, int(in.Mtu), list, in.Permanent)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Find interface on listener.
func (s *GRPCServer) FindInterface(listenerName, name string) (list *Listener, ifce *Interface, err error) {
	// Find listener
	list, err = s.FindListener(listenerName)
	if err != nil {
		return nil, nil, err
	}

	// Find interface.
	list.net.RLock()
	for _, iface := range list.net.interfaces {
		if iface.Name() == name {
			ifce = iface
			break
		}
	}
	list.net.RUnlock()

	// If no interface found, error.
	if ifce == nil {
		return nil, nil, fmt.Errorf("no interface with name: %s", name)
	}
	return
}

// Remove interface from the listener.
func (s *GRPCServer) RemoveInterface(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.Empty, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Close the interface.
	err = ifce.Close()
	if err != nil {
		return nil, err
	}
	return new(pb.Empty), nil
}

// Set interface's MTU.
func (s *GRPCServer) SetInterfaceMTU(ctx context.Context, in *pb.InterfaceMTURequest) (*pb.Empty, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Set the MTU for the interface.
	ifce.SetMTU(int(in.Mtu))

	return new(pb.Empty), nil
}

// Get interface's MTU.
func (s *GRPCServer) GetInterfaceMTU(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.InterfaceMTUReply, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Get the MTU for the interface.
	reply := &pb.InterfaceMTUReply{
		Mtu: int32(ifce.MTU()),
	}

	return reply, nil
}

// Set interface's MAC address.
func (s *GRPCServer) SetInterfaceMACAddress(ctx context.Context, in *pb.InterfaceMACAddressRequest) (*pb.Empty, error) {
	// Parse MAC Address.
	mac, err := net.ParseMAC(in.Mac)
	if err != nil {
		return nil, fmt.Errorf("unable to parse MAC address: %v", err)
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Set the MAC Address for the interface.
	err = ifce.SetMACAddress(mac)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Get interface's MAC address.
func (s *GRPCServer) GetInterfaceMACAddress(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.InterfaceMACAddressReply, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Get the MAC address of the interface.
	mac, err := ifce.GetMACAddress()
	if err != nil {
		return nil, err
	}

	// Set reply with MAC address.
	reply := &pb.InterfaceMACAddressReply{
		Mac: mac.String(),
	}

	return reply, nil
}

// Set interface's IP addresses.
func (s *GRPCServer) SetInterfaceIPAddresses(ctx context.Context, in *pb.InterfaceIPAddressesRequest) (*pb.Empty, error) {
	// Parse provided IP addresses.
	var prefixes []netip.Prefix
	for _, ipAddress := range in.IpAddress {
		prefix, err := netip.ParsePrefix(ipAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to parse IP Address %s: %v", ipAddress, err)
		}
		prefixes = append(prefixes, prefix)
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Set the IP Addresses for the interface.
	err = ifce.SetIPAddresses(prefixes)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Get interface's IP addresses.
func (s *GRPCServer) GetInterfaceIPAddresses(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.InterfaceIPAddressesReply, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Get the IP address(es) of the interface.
	prefixes, err := ifce.GetIPAddresses()
	if err != nil {
		return nil, err
	}

	// Set reply with IP address(es).
	reply := new(pb.InterfaceIPAddressesReply)
	for _, prefix := range prefixes {
		reply.IpAddress = append(reply.IpAddress, prefix.String())
	}

	return reply, nil
}

// Add MAC entry to an interface.
func (s *GRPCServer) InterfaceAddMACEntry(ctx context.Context, in *pb.InterfaceMacEntryRequest) (*pb.Empty, error) {
	// Parse MAC Address.
	mac, err := net.ParseMAC(in.Mac)
	if err != nil {
		return nil, fmt.Errorf("unable to parse MAC address: %v", err)
	}

	// Parse destination
	dst := net.ParseIP(in.Destination)
	if dst == nil {
		return nil, fmt.Errorf("unable to parse IP address")
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Add the MAC entry.
	err = ifce.AddMACEntry(mac, dst, in.Permanent)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Remove MAC entry from an interface.
func (s *GRPCServer) InterfaceRemoveMACEntry(ctx context.Context, in *pb.InterfaceRemoveMacEntryRequest) (*pb.Empty, error) {
	// Parse MAC Address.
	mac, err := net.ParseMAC(in.Mac)
	if err != nil {
		return nil, fmt.Errorf("unable to parse MAC address: %v", err)
	}

	// Parse destination
	dst := net.ParseIP(in.Destination)
	if dst == nil {
		return nil, fmt.Errorf("unable to parse IP address")
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Remove the MAC entry.
	err = ifce.RemoveMACEntry(mac, dst)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Get MAC entries on interface.
func (s *GRPCServer) InterfaceGetMACEntries(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.InterfaceMacEntryReply, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Get MAC entries and make reply.
	entries := ifce.GetMACEntries()
	reply := new(pb.InterfaceMacEntryReply)
	for _, entry := range entries {
		ent := &pb.MacEntry{
			Mac:         entry.MAC.String(),
			Destination: entry.Dst.IP.String(),
			Permanent:   entry.Permanent,
		}
		reply.Entries = append(reply.Entries, ent)
	}

	return reply, nil
}

// Flush MAC table on interface.
func (s *GRPCServer) InterfaceFlushMACTable(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.Empty, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Flush MAC table.
	ifce.FlushMACTable()
	return new(pb.Empty), nil
}

// Add static route to an interface.
func (s *GRPCServer) InterfaceAddStaticRoute(ctx context.Context, in *pb.InterfaceAddStaticRouteRequest) (*pb.Empty, error) {
	// Parse destination prefix.
	dst, err := netip.ParsePrefix(in.Destination)
	if err != nil {
		return nil, fmt.Errorf("failed to parse destination prefix %s: %v", in.Destination, err)
	}

	// Parse gateway.
	gateway, err := netip.ParseAddr(in.Gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway %s: %v", in.Gateway, err)
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Add the MAC entry.
	err = ifce.AddStaticRoute(dst, gateway, int(in.Metric), in.Permanent)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Remove MAC entry from an interface.
func (s *GRPCServer) InterfaceRemoveStaticRoute(ctx context.Context, in *pb.InterfaceRemoveStaticRouteRequest) (*pb.Empty, error) {
	// Parse destination prefix.
	dst, err := netip.ParsePrefix(in.Destination)
	if err != nil {
		return nil, fmt.Errorf("failed to parse destination prefix %s: %v", in.Destination, err)
	}

	// Parse gateway.
	gateway, err := netip.ParseAddr(in.Gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway %s: %v", in.Gateway, err)
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Remove the MAC entry.
	err = ifce.RemoveStaticRoute(dst, gateway)
	if err != nil {
		return nil, err
	}

	return new(pb.Empty), nil
}

// Get MAC entries on interface.
func (s *GRPCServer) InterfaceGetStaticRoutes(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.InterfaceStaticRouteReply, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Get MAC entries and make reply.
	routes := ifce.GetStaticRoutes()
	reply := new(pb.InterfaceStaticRouteReply)
	for _, route := range routes {
		ent := &pb.StaticRoute{
			Destination: route.Destination.String(),
			Gateway:     route.Gateway.String(),
			Metric:      int32(route.Metric),
			Permanent:   route.Permanent,
		}
		reply.Routes = append(reply.Routes, ent)
	}

	return reply, nil
}

// Add static ARP entry to an interface.
func (s *GRPCServer) InterfaceAddStaticARPEntry(ctx context.Context, in *pb.InterfaceARPEntryRequest) (*pb.Empty, error) {
	// Parse IP address
	addr, err := netip.ParseAddr(in.Address)
	if err != nil {
		return nil, fmt.Errorf("unable to parse IP address: %v", err)
	}

	// Parse MAC Address.
	mac, err := net.ParseMAC(in.Mac)
	if err != nil {
		return nil, fmt.Errorf("unable to parse MAC address: %v", err)
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Add the ARP entry.
	ifce.AddStaticARPEntry(addr, mac, in.Permanent)
	return new(pb.Empty), nil
}

// Remove ARP entry from an interface.
func (s *GRPCServer) InterfaceRemoveARPEntry(ctx context.Context, in *pb.InterfaceRemoveARPEntryRequest) (*pb.Empty, error) {
	// Parse IP address
	addr, err := netip.ParseAddr(in.Address)
	if err != nil {
		return nil, fmt.Errorf("unable to parse IP address: %v", err)
	}

	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Remove the ARP entry.
	err = ifce.RemoveARPEntry(addr)
	if err != nil {
		return nil, err
	}
	return new(pb.Empty), nil
}

// Get ARP entries on interface.
func (s *GRPCServer) InterfaceGetARPEntries(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.InterfaceArpEntryReply, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Get MAC entries and make reply.
	entries := ifce.GetARPEntries()
	reply := new(pb.InterfaceArpEntryReply)
	for _, entry := range entries {
		ent := &pb.ArpEntry{
			Address:   entry.Addr.String(),
			Mac:       entry.MAC.String(),
			Expires:   entry.Expires.String(),
			Permanent: entry.Permanent,
		}
		reply.Entries = append(reply.Entries, ent)
	}

	return reply, nil
}

// Flush ARP table on interface.
func (s *GRPCServer) InterfaceFlushARPTable(ctx context.Context, in *pb.InterfaceRequestWithName) (*pb.Empty, error) {
	// Find interface.
	_, ifce, err := s.FindInterface(in.ListenerName, in.Name)
	if err != nil {
		return nil, err
	}

	// Flush ARP table.
	ifce.FlushARPTable()
	return new(pb.Empty), nil
}
