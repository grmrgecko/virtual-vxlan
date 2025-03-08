package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/grmrgecko/virtual-vxlan/tun"
	log "github.com/sirupsen/logrus"
)

// ARP Table Entry.
type ArpEntry struct {
	Addr      netip.Addr
	MAC       net.HardwareAddr
	Expires   time.Time
	Permanent bool
	Updating  bool
}

// Static Route.
type StaticRoute struct {
	Destination netip.Prefix
	Gateway     netip.Addr
	Metric      int
	Permanent   bool
}

// MAC Table Entry.
type MacEntry struct {
	MAC       net.HardwareAddr
	Dst       *net.UDPAddr
	Permanent bool
}

// The vxlan interface containing the tun device.
type Interface struct {
	name string
	vni  uint32

	state struct {
		state    atomic.Uint32
		stopping sync.WaitGroup
		sync.Mutex
	}

	tun struct {
		device    tun.Device
		mtu       atomic.Int32
		mtuc      chan int
		mac       net.HardwareAddr
		addresses []netip.Prefix
		sync.RWMutex
	}

	tables struct {
		arp      []ArpEntry
		arpEvent map[netip.Addr][]chan net.HardwareAddr
		route    []StaticRoute
		mac      []MacEntry
		sync.RWMutex
	}

	Permanent bool
	l         *Listener
	closed    chan struct{}
	log       *log.Entry
}

// Create an interface for the vxlan VNI.
func NewInterface(name string, vni uint32, mtu int, l *Listener, perm bool) (i *Interface, err error) {
	// Verify we have an listener, and lock it.
	if l == nil {
		return nil, fmt.Errorf("we need a listener to attach to")
	}
	if !l.Permanent && perm {
		return nil, fmt.Errorf("cannot add permanent interface to non-permanent listener")
	}
	l.net.Lock()
	defer l.net.Unlock()

	// Check that an interface doesn't already exist with this VNI.
	for _, ifce := range l.net.interfaces {
		if ifce.vni == vni {
			return nil, fmt.Errorf("existing vni %d interface on listener %s", vni, l.PrettyName())
		}
	}

	// Verify an interface on the machine doesn't already have the requested name.
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, iface := range ifaces {
		if iface.Name == name {
			return nil, fmt.Errorf("existing interface with name %s", name)
		}
	}

	// If MTU is not provided, use the listener's MTU minus 50 bytes for vxlan.
	if mtu <= 1 {
		mtu = l.net.maxMessageSize - 50
	}

	// Create the tunnel interface.
	tun, err := tun.CreateTUN(name, mtu)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUN device: %v", err)
	}

	// Verify the real name from the interface in-case the OS decided
	// to change it from the request.
	realName, err := tun.Name()
	if err != nil {
		return nil, fmt.Errorf("failed to get device name: %v", err)
	}

	// Verify the MTU incase the OS changed from requested MTU.
	mtu, err = tun.MTU()
	if err != nil {
		return nil, fmt.Errorf("failed to get device mtu: %v", err)
	}

	// Setup the interface structure.
	i = new(Interface)
	i.name = realName
	i.vni = vni
	i.state.state.Store(uint32(deviceStateDown))
	i.closed = make(chan struct{})
	i.tun.device = tun
	i.tun.mtu.Store(int32(mtu))
	i.tun.mtuc = make(chan int)
	i.tun.mac = generateRandomMAC()
	i.tables.arpEvent = make(map[netip.Addr][]chan net.HardwareAddr)
	i.l = l
	i.log = log.WithFields(log.Fields{
		"listener": l.PrettyName(),
		"device":   name,
		"vni":      vni,
	})
	i.Permanent = perm
	l.net.interfaces = append(l.net.interfaces, i)

	// Start the task queues.
	go i.runARPExpiryJob()
	go i.eventReader()
	go i.packetReader()

	// Inform that the interface has started.
	i.log.Print("Interface started.")

	return
}

// The state of the tun device.
type deviceState uint32

//go:generate go run golang.org/x/tools/cmd/stringer -type deviceState -trimprefix=deviceState
const (
	deviceStateDown deviceState = iota
	deviceStateUp
	deviceStateClosed
)

// Gets the current device state.
func (i *Interface) DeviceState() deviceState {
	return deviceState(i.state.state.Load())
}

// Is the device closed?
func (i *Interface) IsClosed() bool {
	return i.DeviceState() == deviceStateClosed
}

// Is the device up?
func (i *Interface) IsUp() bool {
	return i.DeviceState() == deviceStateUp
}

// Change the device state.
func (i *Interface) changeState(want deviceState) {
	i.state.Lock()
	defer i.state.Unlock()
	old := i.DeviceState()
	if old == deviceStateClosed {
		// once closed, always closed
		i.log.Debugf("Interface closed, ignored requested state %v", want)
		return
	}
	switch want {
	case old:
		return
	case deviceStateUp:
		i.state.state.Store(uint32(deviceStateUp))
		fallthrough // up failed; bring the device all the way back down
	case deviceStateDown:
		i.state.state.Store(uint32(deviceStateDown))
	}
	i.log.Debugf("Interface state was %v, requested %v, now %v", old, want, i.DeviceState())
}

// Bring the internal device state to up.
func (i *Interface) Up() {
	i.changeState(deviceStateUp)
}

// Bring the internal device state to down.
func (i *Interface) Down() {
	i.changeState(deviceStateDown)
}

// Set the interface MTU.
func (i *Interface) SetMTU(mtu int) error {
	// If MTU is not provided, use the listener's MTU minus 50 bytes for vxlan.
	if mtu <= 1 {
		mtu = i.l.net.maxMessageSize - 50
	}
	return i.tun.device.SetMTU(mtu)
}

// Get the interface MTU.
func (i *Interface) MTU() int {
	return int(i.tun.mtu.Load())
}

// Get the interface name.
func (i *Interface) Name() string {
	return i.name
}

// Get the VNI assigned for this interface.
func (i *Interface) VNI() uint32 {
	return i.vni
}

// Close this interface.
func (i *Interface) Close() (err error) {
	i.state.Lock()
	defer i.state.Unlock()
	if i.IsClosed() {
		return
	}
	i.state.state.Store(uint32(deviceStateClosed))
	i.log.Debug("Interface is closing.")

	err = i.tun.device.Close()
	i.state.stopping.Wait()

	// Remove self from listener.
	netc := &i.l.net
	netc.Lock()
	defer netc.Unlock()
	for p, iface := range netc.interfaces {
		if iface == i {
			netc.interfaces = append(netc.interfaces[:p], netc.interfaces[p+1:]...)
			break
		}
	}
	close(i.closed)
	i.log.Print("Interface closed.")
	return
}

// Set the MAC address for this interface.
func (i *Interface) SetMACAddress(mac net.HardwareAddr) (err error) {
	i.tun.Lock()
	i.tun.mac = mac
	i.tun.Unlock()
	return
}

// Get the MAC address for this interface.
func (i *Interface) GetMACAddress() (net.HardwareAddr, error) {
	i.tun.RLock()
	defer i.tun.RUnlock()
	return i.tun.mac, nil
}

// Set IP addresses on this interface.
func (i *Interface) SetIPAddresses(addresses []netip.Prefix) (err error) {
	i.tun.Lock()
	defer i.tun.Unlock()
	err = i.tun.device.SetIPAddresses(addresses)
	if err != nil {
		return
	}
	i.tun.addresses = addresses
	return
}

// Get IP adddreses on this interface.
func (i *Interface) GetIPAddresses() ([]netip.Prefix, error) {
	return i.tun.device.GetIPAddresses()
}

// Add an entry to the MAC table.
func (i *Interface) AddMACEntry(mac net.HardwareAddr, dst net.IP, perm bool) error {
	// Lock tables to prevent concurrent requests.
	i.tables.Lock()
	defer i.tables.Unlock()

	// Verify this entry doesn't already exist.
	for _, ent := range i.tables.mac {
		if bytes.Equal(ent.MAC, mac) && ent.Dst.IP.Equal(dst) {
			return fmt.Errorf("entry already exists for %v via %v", mac, dst)
		}
	}

	// Get the UDP address for the provided IP.
	addr, _ := netip.AddrFromSlice(dst)
	udpAddr := net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, uint16(i.l.net.addr.Port)))

	// Add entry to MAC table.
	entry := MacEntry{
		MAC:       mac,
		Dst:       udpAddr,
		Permanent: perm,
	}
	i.tables.mac = append(i.tables.mac, entry)
	return nil
}

// Remove an individual MAC table entry.
func (i *Interface) RemoveMACEntry(mac net.HardwareAddr, dst net.IP) error {
	// Lock table during modification.
	i.tables.Lock()
	defer i.tables.Unlock()

	// Find matching entry.
	found := -1
	for i, ent := range i.tables.mac {
		if bytes.Equal(ent.MAC, mac) && ent.Dst.IP.Equal(dst) {
			found = i
			break
		}
	}

	// If not found return error.
	if found == -1 {
		return fmt.Errorf("unable to find matching MAC entry")
	}

	// If found, remove it.
	i.tables.mac = append(i.tables.mac[:found], i.tables.mac[found+1:]...)
	return nil
}

// Returns the MAC to destination table.
func (i *Interface) GetMACEntries() []MacEntry {
	i.tables.RLock()
	defer i.tables.RUnlock()
	return i.tables.mac
}

// Clears the MAC to UDP Address association table.
func (i *Interface) FlushMACTable() {
	i.tables.Lock()
	i.tables.mac = nil
	i.tables.Unlock()
}

// Find a destination UDP address for a MAC address.
func (i *Interface) GetDestinationFor(mac net.HardwareAddr) *net.UDPAddr {
	// Lock tables to prevent concurrent requests.
	i.tables.RLock()
	defer i.tables.RUnlock()

	// Keep track of both controller and direct matches.
	var controllers []*net.UDPAddr
	var matches []*net.UDPAddr

	// Find matches.
	for _, ent := range i.tables.mac {
		if bytes.Equal(ent.MAC, app.ControllerMac) {
			controllers = append(controllers, ent.Dst)
		} else if bytes.Equal(ent.MAC, mac) {
			matches = append(matches, ent.Dst)
		}
	}

	// If we have an direct match, return a random entry.
	if len(matches) != 0 {
		return matches[rand.Intn(len(matches))]
	}

	// If we have no direct matches, but controllers, return a random controller.
	if len(controllers) != 0 {
		return controllers[rand.Intn(len(controllers))]
	}

	// If we found no matches at all, return nil.
	return nil
}

// Add static route to interface.
func (i *Interface) AddStaticRoute(destination netip.Prefix, gateway netip.Addr, metric int, perm bool) error {
	// Prevent concurrent table modifications.
	i.tables.Lock()
	defer i.tables.Unlock()
	i.tun.Lock()
	defer i.tun.Unlock()

	// Default to 256 metric.
	if metric == 0 {
		metric = 256
	}

	// Try adding via tunnel device.
	err := i.tun.device.AddRoute(destination, gateway, metric)
	if err != nil {
		return err
	}

	// If route successfully added, add to route table.
	route := StaticRoute{
		Destination: destination,
		Gateway:     gateway,
		Metric:      metric,
		Permanent:   perm,
	}
	i.tables.route = append(i.tables.route, route)
	return nil
}

// Remove static route from interface.
func (i *Interface) RemoveStaticRoute(destination netip.Prefix, gateway netip.Addr) error {
	// Prevent concurrent table modifications.
	i.tables.Lock()
	defer i.tables.Unlock()
	i.tun.Lock()
	defer i.tun.Unlock()

	// Remove from device first.
	err := i.tun.device.RemoveRoute(destination, gateway)
	if err != nil {
		return err
	}

	// Remove from table.
	for r, route := range i.tables.route {
		// If route matches, remove it.
		if route.Destination == destination && route.Gateway == gateway {
			i.tables.route = append(i.tables.route[:r], i.tables.route[r+1:]...)
			break
		}
	}
	return nil
}

// Returns the static route table.
func (i *Interface) GetStaticRoutes() []StaticRoute {
	i.tables.RLock()
	defer i.tables.RUnlock()
	return i.tables.route
}

// Find gateway for IP.
func (i *Interface) GetGateway(destination netip.Addr) (gateway netip.Addr) {
	// Get read lock on table.
	i.tables.RLock()
	defer i.tables.RUnlock()
	metric := math.MaxInt
	for _, route := range i.tables.route {
		if route.Destination.Contains(destination) && route.Metric < metric {
			gateway = route.Gateway
			metric = route.Metric
		}
	}
	return
}

// Add an ARP entry that is static.
func (i *Interface) AddStaticARPEntry(addr netip.Addr, mac net.HardwareAddr, perm bool) {
	// Prevent concurrent table modifications.
	i.tables.Lock()
	defer i.tables.Unlock()

	// Keep track on if we updated an existing entry.
	updated := false

	// Check existing arp entires for this address.
	for p, ent := range i.tables.arp {
		// If this address is this entry, update or remove based on MAC.
		if ent.Addr == addr {
			// If MAC matches, update it. Otherwise, remove
			// tne ARP entry as we're adding a new one.
			if bytes.Equal(ent.MAC, mac) {
				updated = true
				i.tables.arp[p].Expires = time.Time{}
				i.tables.arp[p].Updating = false
				i.tables.arp[p].Permanent = perm
			} else {
				i.tables.arp = append(i.tables.arp[:p], i.tables.arp[p+1:]...)
			}
			break
		}
	}

	// If we didn't update an existing entry, we should add a new one.
	if !updated {
		newMac := make(net.HardwareAddr, 6)
		copy(newMac, mac)
		entry := ArpEntry{
			Addr:      addr,
			MAC:       newMac,
			Updating:  false,
			Permanent: perm,
		}
		i.tables.arp = append(i.tables.arp, entry)
	}
}

// Remove an individual MAC table entry.
func (i *Interface) RemoveARPEntry(addr netip.Addr) error {
	// Lock table during modification.
	i.tables.Lock()
	defer i.tables.Unlock()

	// Find matching entry.
	found := -1
	for i, ent := range i.tables.arp {
		if ent.Addr == addr {
			found = i
			break
		}
	}

	// If not found return error.
	if found == -1 {
		return fmt.Errorf("unable to find matching ARP entry")
	}

	// If found, remove it.
	i.tables.arp = append(i.tables.arp[:found], i.tables.arp[found+1:]...)
	return nil
}

// Get ARP entries.
func (i *Interface) GetARPEntries() []ArpEntry {
	i.tables.RLock()
	defer i.tables.RUnlock()
	return i.tables.arp
}

// Add ARP entry, or update expiry on existing entry.
func (i *Interface) AddOrUpdateARP(addr netip.Addr, mac net.HardwareAddr) {
	// Prevent concurrent table modifications.
	i.tables.Lock()
	defer i.tables.Unlock()

	// Keep track on if we updated an existing entry.
	updated := false

	// Check existing arp entires for this address.
	for p, ent := range i.tables.arp {
		// If this address is this entry, update or remove based on MAC.
		if ent.Addr == addr {
			// If MAC matches, update it. Otherwise, remove
			// tne ARP entry as we're adding a new one.
			if bytes.Equal(ent.MAC, mac) {
				updated = true
				i.tables.arp[p].Expires = time.Now().Add(60 * time.Second)
				i.tables.arp[p].Updating = false
			} else {
				i.tables.arp = append(i.tables.arp[:p], i.tables.arp[p+1:]...)
			}
			break
		}
	}

	// If we didn't update an existing entry, we should add a new one.
	if !updated {
		newMac := make(net.HardwareAddr, 6)
		copy(newMac, mac)
		entry := ArpEntry{
			Addr:      addr,
			MAC:       newMac,
			Expires:   time.Now().Add(60 * time.Second),
			Updating:  false,
			Permanent: false,
		}
		i.tables.arp = append(i.tables.arp, entry)
	}
}

// In the event of finding an expired MAC entry, we should
// try updating the entry to ensure its not changed.
func (i *Interface) updateARPFor(addr netip.Addr, brdMac net.HardwareAddr, ifceAddr netip.Addr) {
	// Lock tables to make update channel.
	i.tables.Lock()

	// As we did not find an existing entry, we need to make an ARP request.
	// Start a channel to receive the ARP event back with the MAC.
	c := make(chan net.HardwareAddr)
	// Add to the event list.
	i.tables.arpEvent[addr] = append(i.tables.arpEvent[addr], c)
	// When this function is done, ensure the event channel is removed.
	defer func() {
		i.tables.Lock()
		for p, ch := range i.tables.arpEvent[addr] {
			if ch == c {
				i.tables.arpEvent[addr] = append(i.tables.arpEvent[addr][:p], i.tables.arpEvent[addr][p+1:]...)
				break
			}
		}
		i.tables.Unlock()
	}()

	// Unlock the tables to allow ARP replies to add to the tables.
	i.tables.Unlock()

	// Get an destination UDP address for the ARP request.
	dst := i.GetDestinationFor(brdMac)
	if dst == nil {
		return
	}

	// Make Ethernet layer for packet.
	eth := layers.Ethernet{
		SrcMAC:       i.tun.mac,
		DstMAC:       brdMac,
		EthernetType: layers.EthernetTypeARP,
	}

	// Make ARP layer for packet.
	arp := layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		HwAddressSize:     6,
		Operation:         layers.ARPRequest,
		SourceHwAddress:   []byte(i.tun.mac),
		SourceProtAddress: ifceAddr.AsSlice(),
		DstHwAddress:      []byte{0, 0, 0, 0, 0, 0},
		DstProtAddress:    addr.AsSlice(),
	}
	// Size and protocol type differs on IPv6 vs IPv4.
	if addr.Is4() {
		arp.Protocol = layers.EthernetTypeIPv4
		arp.ProtAddressSize = 4
	} else {
		arp.Protocol = layers.EthernetTypeIPv6
		arp.ProtAddressSize = 16
	}

	// We give 3 ARP request tries, otherwise we timeout.
	tries := 0
	for tries < 3 {
		tries += 1
		// Try sending packet.
		i.SendLayers(dst, &eth, &arp)
		select {
		// If we receive the response, return the value.
		case <-c:
			return
			// If we timeout, after 3 seconds, then continue.
		case <-time.After(3 * time.Second):
			i.log.Debugf("timeout on ARP request for %s with tries %d", addr.String(), tries)
			continue
		}
	}
	// If all tries are done, the addres is expired.
	i.log.Debugf("MAC address for %s has expired", addr.String())

	// Lock tables and find the entry to remove.
	i.tables.Lock()
	for p, ent := range i.tables.arp {
		// If this is the right entry, remove it.
		if ent.Addr == addr {
			i.tables.arp = append(i.tables.arp[:p], i.tables.arp[p+1:]...)
			break
		}
	}
	i.tables.Unlock()
}

// Find the MAC address for an IP address from the ARP table.
// If an entry doesn't exist, we will attempt to request it.
func (i *Interface) GetMacFor(addr netip.Addr) (net.HardwareAddr, error) {
	// First, check if a static route defines a replacement address.
	gateway := i.GetGateway(addr)
	// If an gateway is defined, replace the requested address with the gateway.
	if gateway.IsValid() {
		addr = gateway
	}

	// Lots of math depending on is4.
	is4 := addr.Is4()

	// Multicast traffic should be destined to a multicast MAC address.
	if addr.IsMulticast() {
		if is4 {
			// Get the lower 23 bits of the IPv4 address.
			ip := addr.As4()
			lower23 := uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
			lower23 &= 0x7FFFFF // Mask to 23 bits

			// Construct the MAC address.
			mac := net.HardwareAddr{
				0x01, 0x00, 0x5E,
				byte((lower23 >> 16) & 0xFF),
				byte((lower23 >> 8) & 0xFF),
				byte(lower23 & 0xFF),
			}
			return mac, nil
		} else {
			// Construct MAC Address using the lower 32 bits of the IPv6 address.
			ip := addr.As16()
			mac := net.HardwareAddr{
				0x33, 0x33,
				ip[12],
				ip[13],
				ip[14],
				ip[15],
			}
			return mac, nil
		}
	}

	// Find an IP address on this interface that matches the IP we're pinging.
	i.tun.RLock()
	var ifcePrefix netip.Prefix
	var ifceAddr netip.Addr
	for _, prefix := range i.tun.addresses {
		// If this prefix is an exact match, this is the IP we use.
		// Otherwise, we will accept the IP of the same type.
		if prefix.Contains(addr) {
			ifcePrefix = prefix
			ifceAddr = prefix.Addr()
			break
		} else if prefix.Addr().Is4() == is4 {
			ifcePrefix = prefix
			ifceAddr = prefix.Addr()
		}
	}
	i.tun.RUnlock()

	// Set broadcast MAC.
	brdMac := net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

	// If this is IPv4, determine if the address provided is broadcast
	// and return broadcast mac.
	if is4 && ifcePrefix.Contains(ifceAddr) {
		// Get the host bits to mask IP to broadcast.
		hostBits := 32 - ifcePrefix.Bits()

		// Set all host bits to 1 to calculate the broadcast address.
		broadcastAddr := ifceAddr.As4()
		for i := 0; i < hostBits; i++ {
			broadcastAddr[3-i/8] |= byte(1 << (i % 8))
		}

		// If the provided address is the broadcast, return the broadcast MAC.
		if addr == netip.AddrFrom4(broadcastAddr) {
			return brdMac, nil
		}
	}

	// Lock tables, no concurrent requests.
	i.tables.Lock()

	// Check arp table for existing entry.
	for _, ent := range i.tables.arp {
		// If we found an existing entry, return the value.
		if ent.Addr == addr {
			defer i.tables.Unlock()
			// If we're not already updating, check if this entry is expired.
			if !ent.Updating && !ent.Expires.IsZero() {
				now := time.Now()
				// If expired, attempt to update MAC address in background.
				if now.After(ent.Expires) {
					ent.Updating = true
					go i.updateARPFor(addr, brdMac, ifceAddr)
				}
			}
			return ent.MAC, nil
		}
	}

	// As we did not find an existing entry, we need to make an ARP request.
	// Start a channel to receive the ARP event back with the MAC.
	c := make(chan net.HardwareAddr)
	// Add to the event list.
	i.tables.arpEvent[addr] = append(i.tables.arpEvent[addr], c)
	// When this function is done, ensure the event channel is removed.
	defer func() {
		i.tables.Lock()
		for p, ch := range i.tables.arpEvent[addr] {
			if ch == c {
				i.tables.arpEvent[addr] = append(i.tables.arpEvent[addr][:p], i.tables.arpEvent[addr][p+1:]...)
				break
			}
		}
		i.tables.Unlock()
	}()

	// Unlock the tables to allow ARP replies to add to the tables.
	i.tables.Unlock()

	// Get an destination UDP address for the ARP request.
	dst := i.GetDestinationFor(brdMac)
	if dst == nil {
		return nil, fmt.Errorf("no destination to send ARP to")
	}

	// Make Ethernet layer for packet.
	eth := layers.Ethernet{
		SrcMAC:       i.tun.mac,
		DstMAC:       brdMac,
		EthernetType: layers.EthernetTypeARP,
	}

	// Make ARP layer for packet.
	arp := layers.ARP{
		AddrType:          layers.LinkTypeEthernet,
		HwAddressSize:     6,
		Operation:         layers.ARPRequest,
		SourceHwAddress:   []byte(i.tun.mac),
		SourceProtAddress: ifceAddr.AsSlice(),
		DstHwAddress:      []byte{0, 0, 0, 0, 0, 0},
		DstProtAddress:    addr.AsSlice(),
	}
	// Size and protocol type differs on IPv6 vs IPv4.
	if is4 {
		arp.Protocol = layers.EthernetTypeIPv4
		arp.ProtAddressSize = 4
	} else {
		arp.Protocol = layers.EthernetTypeIPv6
		arp.ProtAddressSize = 16
	}

	// We give 3 ARP request tries, otherwise we timeout.
	tries := 0
	for tries < 3 {
		tries += 1
		// Try sending packet.
		i.SendLayers(dst, &eth, &arp)
		select {
		// If we receive the response, return the value.
		case mac := <-c:
			return mac, nil
			// If we timeout, after 3 seconds, then continue.
		case <-time.After(3 * time.Second):
			i.log.Debugf("timeout on ARP request for %s with tries %d", addr.String(), tries)
			continue
		}
	}
	// If all tries are done, return error.
	return nil, fmt.Errorf("unable to get MAC address for %s", addr.String())
}

// A job to check for expired arp entries and remove them from the arp table.
func (i *Interface) runARPExpiryJob() {
	// Run job every minute.
	ticker := time.NewTicker(1 * time.Minute)
	for {
		// Run the job queue if this interface doesn't close.
		select {
		case <-ticker.C:
			// Lock tables for ARP operations.
			i.tables.Lock()
			// Get current time to compare against expiry date.
			now := time.Now()
			// Start with a found entry so the loop runs first.
			foundEntry := true
			for foundEntry {
				// Set to no found entry, so we stop the loop if no entry is found.
				foundEntry = false
				// Scan arp table for expired entries.
				for p, ent := range i.tables.arp {
					// If this entry is expired and past 2 minute grace, remove it and set that we found an entry.
					if !ent.Expires.IsZero() && now.Before(ent.Expires.Add(120*time.Second)) {
						foundEntry = true
						i.tables.arp = append(i.tables.arp[:p], i.tables.arp[p+1:]...)
						break
					}
				}
			}
			// No further expired entries found, unlock the table.
			i.tables.Unlock()
		case <-i.closed:
			return
		}
	}
}

// Clears the arp table.
func (i *Interface) FlushARPTable() {
	i.tables.Lock()
	i.tables.arp = nil
	i.tables.Unlock()
}

// Handle an ARP packet received.
func (i *Interface) HandleARP(arp layers.ARP) {
	// If type isn't ethernet, we don't care.
	if arp.AddrType != layers.LinkTypeEthernet {
		return
	}
	// If ARP request, we should verify its for our IP.
	if arp.Operation == layers.ARPRequest {
		// Parse the destination address.
		addr, ok := netip.AddrFromSlice(arp.DstProtAddress)
		if !ok {
			return
		}
		// Find the address that matches the requested address.
		i.tun.RLock()
		defer i.tun.RUnlock()
		for _, prefix := range i.tun.addresses {
			// If the requested address is this one, we should reply.
			if prefix.Addr() == addr {
				// Get the MAC address and destination to send reply to.
				srcMac := net.HardwareAddr(arp.SourceHwAddress)
				dst := i.GetDestinationFor(srcMac)
				if dst == nil {
					return
				}

				// Parse the source address.
				srcAddr, ok := netip.AddrFromSlice(arp.SourceProtAddress)
				if !ok {
					return
				}

				// Add the ARP entry for the source.
				i.AddOrUpdateARP(srcAddr, srcMac)

				// Make an ethernet layer for sending ARP reply.
				eth := layers.Ethernet{
					SrcMAC:       i.tun.mac,
					DstMAC:       srcMac,
					EthernetType: layers.EthernetTypeARP,
				}

				// Make the ARP reply layer.
				arpReply := layers.ARP{
					AddrType:          arp.AddrType,
					Protocol:          arp.Protocol,
					HwAddressSize:     arp.HwAddressSize,
					ProtAddressSize:   arp.ProtAddressSize,
					Operation:         layers.ARPReply,
					SourceHwAddress:   []byte(i.tun.mac),
					SourceProtAddress: addr.AsSlice(),
					DstHwAddress:      srcMac,
					DstProtAddress:    srcAddr.AsSlice(),
				}

				// Send the ARP reply.
				i.SendLayers(dst, &eth, &arpReply)
			}
		}
		// On ARP reply, we should verify its to us and update table.
	} else if arp.Operation == layers.ARPReply {
		// If not destined to us, we should stop here.
		if !bytes.Equal(i.tun.mac, arp.DstHwAddress) {
			i.log.Debugf("Mac doesn't match our MAC: %s", net.HardwareAddr(arp.DstHwAddress))
			return
		}

		// Parse the source.
		srcMac := net.HardwareAddr(arp.SourceHwAddress)
		srcAddr, ok := netip.AddrFromSlice(arp.SourceProtAddress)
		if !ok {
			return
		}

		// Add the source of the ARP reply to the ARP table.
		i.AddOrUpdateARP(srcAddr, srcMac)

		// Send event to any ARP reply event listeners.
		i.tables.RLock()
		chs, ok := i.tables.arpEvent[srcAddr]
		if ok {
			for _, c := range chs {
				c <- srcMac
			}
		}
		i.tables.RUnlock()
	}
}

// Process IPv4 packet.
func (i *Interface) HandleIPv4(eth layers.Ethernet, ip4 layers.IPv4) {
	// Verify the destination is us.
	if !bytes.Equal(eth.DstMAC, i.tun.mac) {
		return
	}

	// Pass packet to the interface.
	data := append(ip4.Contents, ip4.Payload...)
	i.Write(data)
}

// Process IPv6 packet.
func (i *Interface) HandleIPv6(eth layers.Ethernet, ip6 layers.IPv6) {
	// Verify the destination is us.
	if !bytes.Equal(eth.DstMAC, i.tun.mac) {
		return
	}

	// Pass packet to the interface.
	data := append(ip6.Contents, ip6.Payload...)
	i.Write(data)
}

// Send a packet to a destination with ethernet layers and bytes to append to encoded packet.
// The packet will be encapsulated in vxlan tunnel.
func (i *Interface) SendLayers(dst *net.UDPAddr, layersToSend ...gopacket.SerializableLayer) {
	// Make the VXLAN layer with our VNI.
	vxlan := layers.VXLAN{
		ValidIDFlag: true,
		VNI:         i.vni,
	}

	// Setup buffer for encoding the packet.
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	// Encode packet with the vxlan and other provided layers.
	err := gopacket.SerializeLayers(buf, opts, append([]gopacket.SerializableLayer{&vxlan}, layersToSend...)...)
	if err != nil {
		i.log.Errorf("failed to encode layers: %v", err)
	}

	// Send packet to destination.
	_, err = i.l.WriteTo(buf.Bytes(), dst)
	if err != nil {
		i.log.Errorf("failed to send layers: %v", err)
	}
}

// Passsthru to tun write.
func (i *Interface) Write(b []byte) (int, error) {
	return i.tun.device.Write(b)
}

// Listen to events from the tun device.
func (i *Interface) eventReader() {
	i.log.Debug("event worker - started")

	// For each event received, process it.
	for event := range i.tun.device.Events() {
		// If event is an MTU change, update our MTU.
		if event&tun.EventMTUUpdate != 0 {
			// Get the device's current MTU.
			mtu, err := i.tun.device.MTU()
			if err != nil {
				i.log.Errorf("failed to load updated MTU of device: %v", err)
				continue
			}

			// If the MTU is negative, that isn't valid.
			if mtu < 0 {
				i.log.Errorf("MTU not updated to negative value: %v", mtu)
				continue
			}

			// Determine if the MTU set is larger than our maximum based on listern's max size.
			var tooLarge string
			maxMTU := i.l.MaxMessageSize() - 50
			if mtu > maxMTU {
				tooLarge = fmt.Sprintf(" (too large, capped at %v)", maxMTU)
				mtu = maxMTU
			}

			// Update the MTU, getting the prior MTU.
			old := i.tun.mtu.Swap(int32(mtu))

			// If the MTU changed, we should update the buffer size in the packet reader.
			if int(old) != mtu {
				go func() {
					i.tun.mtuc <- mtu
				}()
				i.log.Debugf("MTU updated: %v%s", mtu, tooLarge)
			}
		}

		// If the interface is going up, we should update our state.
		if event&tun.EventUp != 0 {
			i.log.Debug("Interface up requested")
			i.Up()
		}

		// If the interface is going down, we should update our state.
		if event&tun.EventDown != 0 {
			i.log.Debug("Interface down requested")
			i.Down()
		}
	}

	i.log.Debug("event worker - stopped")
}

// Read packets from the tun device, and process them.
func (i *Interface) packetReader() {
	// When stopping, we want to ensure this packet reader stopped before we release the interface.
	i.state.stopping.Add(1)
	defer func() {
		i.log.Debug("TUN packet reader - stopped")
		i.state.stopping.Done()
	}()

	// Setup packet parsers.
	var ip4 layers.IPv4
	ip4Parser := gopacket.NewDecodingLayerParser(layers.LayerTypeIPv4, &ip4)
	ip4Parser.IgnoreUnsupported = true
	var ip6 layers.IPv6
	ip6Parser := gopacket.NewDecodingLayerParser(layers.LayerTypeIPv4, &ip6)
	ip6Parser.IgnoreUnsupported = true
	decoded := []gopacket.LayerType{}

	// Setup buffer with current MTU value.
	i.log.Debug("TUN packet reader - started")
	buf := make([]byte, i.MTU())

	// Start reading from the tun device.
	for {
		n, err := i.tun.device.Read(buf)

		// If packet received data, parse it.
		if n > 1 {
			packet := buf[:n]
			switch packet[0] >> 4 {
			case 4:
				// Parse IPv4 packet.
				decoded = nil
				err := ip4Parser.DecodeLayers(packet, &decoded)
				if err == nil {
					// Parse the IP address for the destination.
					addr, ok := netip.AddrFromSlice(ip4.DstIP)
					if !ok {
						continue
					}

					// Find the MAC address for the IP.
					mac, err := i.GetMacFor(addr)
					if err != nil {
						i.log.Error(err)
						continue
					}

					// Get the UDP destination for the MAC address.
					dst := i.GetDestinationFor(mac)
					if dst == nil {
						continue
					}

					// Setup ethernet layer.
					eth := layers.Ethernet{
						SrcMAC:       i.tun.mac,
						DstMAC:       mac,
						EthernetType: layers.EthernetTypeIPv4,
					}

					// Setup upper layer masquerade.
					masq := Masquerade{
						MData:      ip4.Payload,
						MLayerType: ip4.Protocol.LayerType(),
					}

					// Send data to destination.
					i.SendLayers(dst, &eth, &ip4, &masq)
				}
			case 6:
				// Pase IPv6 packet.
				decoded = nil
				err := ip6Parser.DecodeLayers(packet, &decoded)
				if err == nil {
					// Parse the IP address for the destination.
					addr, ok := netip.AddrFromSlice(ip6.DstIP)
					if !ok {
						continue
					}

					// Find the MAC address for the IP.
					mac, err := i.GetMacFor(addr)
					if err != nil {
						i.log.Error(err)
						continue
					}

					// Get the UDP destination for the MAC address.
					dst := i.GetDestinationFor(mac)
					if dst == nil {
						continue
					}

					// Setup ethernet layer.
					eth := layers.Ethernet{
						SrcMAC:       i.tun.mac,
						DstMAC:       mac,
						EthernetType: layers.EthernetTypeIPv6,
					}

					// Setup upper layer masquerade.
					masq := Masquerade{
						MData:      ip6.Payload,
						MLayerType: ip6.NextLayerType(),
					}

					// Send data to destination.
					i.SendLayers(dst, &eth, &ip6, &masq)
				}
			}
		}

		// If we received an error, check if we should stop.
		if err != nil {
			// If the error is relating to segments, just continue.
			if errors.Is(err, tun.ErrTooManySegments) {
				i.log.Debugf("dropped some packets from multi-segment read: %v", err)
				continue
			}
			// If this device isn't closed, we should close it for other errors.
			if !i.IsClosed() {
				// Log error if its not an closed error.
				if !errors.Is(err, os.ErrClosed) {
					i.log.Errorf("failed to read packet from TUN device: %v", err)
				}
				// Close this interface.
				go i.Close()
			}
			// Stop the packet reader here.
			return
		}

		// If the MTU has a change request, make a new buffer.
		select {
		case newMTU, ok := <-i.tun.mtuc:
			if ok {
				buf = make([]byte, newMTU)
			}
		default:
		}
	}
}
