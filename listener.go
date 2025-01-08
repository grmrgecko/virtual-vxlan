package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	log "github.com/sirupsen/logrus"
)

// The base of a vxlan connection is the port listener.
// The port listener listens for vxlan packets on a port,
// and it passes matching VNI values with interfaces.
// Interfaces are added to the listener, which processes
// vxlan packets.
type Listener struct {
	net struct {
		stopping        sync.WaitGroup
		addr            *net.UDPAddr
		is4             bool
		maxMessageSize  int
		maxMessageSizeC chan int
		conn            *net.UDPConn
		promisc         *Promiscuous
		interfaces      []*Interface
		sync.RWMutex
	}

	Name      string
	Permanent bool
	closed    chan struct{}
	log       *log.Entry
}

// Make a new listener on the specified address. This
// listener is added to the app listener list, and errors
// on existing listeners for the specified address.
func NewListener(name, address string, maxMessageSize int, perm bool) (l *Listener, err error) {
	// Verify the specified address is valid.
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return
	}
	is4 := addr.IP.To4() != nil

	// Verify no listeners exist with this address.
	app.Net.Lock()
	defer app.Net.Unlock()
	for _, listener := range app.Net.Listeners {
		if listener.PrettyName() == addr.String() {
			return nil, fmt.Errorf("listener already exists with address %s", addr.String())
		}
		if listener.Name == name {
			return nil, fmt.Errorf("listener already exists with name %s", name)
		}
	}

	llog := log.WithFields(log.Fields{
		"listener": addr.String(),
	})

	// Check if we're binding to an interface.
	var promisc *Promiscuous
	if !isZeroAddr(addr.IP) {
		// Check that the interface exists, on Windows it may be a bit before the interface
		// becomes available.
		tries := 0
	tryLoop:
		for {
			// Get interface list.
			ifaces, err := net.Interfaces()
			if err != nil {
				return nil, fmt.Errorf("unable to get interface list %s", err)
			}

			// Check the IP list of each interface for a matching IP to bind addr.
			for _, iface := range ifaces {
				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}

				// If this address has the bind address, we can stop here.
				for _, address := range addrs {
					ipAddr, _, _ := net.ParseCIDR(address.String())
					if addr.IP.Equal(ipAddr) {
						break tryLoop
					}
				}
			}

			// If the bind address wasn't found on an interface, try again for 5 minutes.
			tries++
			if tries < 5 {
				llog.Print("Unable to find interface, will check again in 1 minute...")
				time.Sleep(time.Minute)
			} else {
				// If we passed 5 minutes, we should stop...
				break
			}
		}

		// On Windows, there is no public way to configure hardware vxlan offloading.
		// This in term filters packets that are destined to non-broadcast MAC addresses
		// in vxlan packets. Which prevents us from receiving the packets, so we set the
		// interface to promiscuous mode to allow us to receive packets.
		promisc, err = SetInterfacePromiscuous(addr.IP)
		if err != nil {
			return
		}
	}

	// Start listening on the specified address.
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return
	}

	// Save listener details.
	l = new(Listener)
	l.Name = name
	l.net.addr = addr
	l.net.is4 = is4
	l.net.maxMessageSize = maxMessageSize
	l.net.maxMessageSizeC = make(chan int)
	l.net.conn = conn
	l.net.promisc = promisc
	l.log = llog
	l.closed = make(chan struct{})
	l.Permanent = perm
	app.Net.Listeners = append(app.Net.Listeners, l)

	// Start reading packets on the listener.
	go l.packetReader()

	// Inform that we started a listern.
	l.log.Print("Listener started.")
	return
}

// Get the current maximum message size.
func (l *Listener) MaxMessageSize() int {
	l.net.RLock()
	defer l.net.RUnlock()
	return l.net.maxMessageSize
}

// Set the maximum message size to a new value.
func (l *Listener) SetMaxMessageSize(size int) {
	if size <= 1 {
		return
	}
	l.net.Lock()
	l.net.maxMessageSize = size
	l.net.Unlock()
	go func() {
		l.net.maxMessageSizeC <- size
	}()
}

// Close the listener, and its interfaces.
func (l *Listener) Close() (err error) {
	// Ensure proper multi tasking.
	l.net.Lock()
	l.log.Debug("Listener is closing.")

	// Close the connection.
	err = l.net.conn.Close()
	if l.net.promisc != nil {
		l.net.promisc.Close()
	}
	close(l.closed)

	// Wait for packet readers to stop.
	l.net.stopping.Wait()

	// Remove self from app.
	app.Net.Lock()
	defer app.Net.Unlock()
	for i, listener := range app.Net.Listeners {
		if listener == l {
			app.Net.Listeners = append(app.Net.Listeners[:i], app.Net.Listeners[i+1:]...)
			break
		}
	}

	// The interfaces will be acquiring lock here while closing.
	l.net.Unlock()
	for len(l.net.interfaces) >= 1 {
		l.net.interfaces[0].Close()
	}

	l.log.Print("Listener closed.")
	return
}

// Get listener name.
func (l *Listener) PrettyName() string {
	return l.net.addr.String()
}

// Get listener address.
func (l *Listener) Addr() net.Addr {
	return l.net.addr
}

// Read packets and parse.
func (l *Listener) packetReader() {
	// When stopping, we need to wait for this packet reader to finish.
	l.net.stopping.Add(1)
	defer func() {
		l.log.Debug("packet reader - stopped")
		l.net.stopping.Done()
	}()

	// Setup packet decoder.
	var vxlan layers.VXLAN
	var eth layers.Ethernet
	var ip4 layers.IPv4
	var ip6 layers.IPv6
	var arp layers.ARP
	parser := gopacket.NewDecodingLayerParser(layers.LayerTypeVXLAN, &vxlan, &eth, &ip4, &ip6, &arp)
	parser.IgnoreUnsupported = true
	var decoded []gopacket.LayerType

	// Start reading packets, with current buffer size.
	l.log.Debug("packet reader - started")
	buf := make([]byte, l.net.maxMessageSize)
	for {
		// Read packet.
		n, err := l.net.conn.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			l.log.Errorf("received error reading from listener: %v", err)
		}

		// Only process packets larger than a vxlan header.
		if n > 38 {
			// Attempt to parse vxlan and its layers, up to IP and ARP.
			// Parsing any further is a waste of processing power.
			decoded = nil
			packet := buf[:n]
			err = parser.DecodeLayers(packet, &decoded)

			// If we successfully parsed a packet, route it accordingly.
			if err == nil {
				// To ensure the interfaces do not change as we're handling the packet, lock it.
				l.net.RLock()
				for _, ifce := range l.net.interfaces {
					// If this interface has the decoded VNI, pass the packet to it.
					if ifce.vni == vxlan.VNI {
						// Depending on what layer type was decoded, pass to the interface.
						for _, layerType := range decoded {
							if layerType == layers.LayerTypeARP {
								ifce.HandleARP(arp)
							} else if layerType == layers.LayerTypeIPv4 {
								ifce.HandleIPv4(eth, ip4)
							} else if layerType == layers.LayerTypeIPv6 {
								ifce.HandleIPv6(eth, ip6)
							}
						}
					}
				}
				l.net.RUnlock()
			}
		}

		// If the max message size has a change request, make a new buffer and save change.
		select {
		case newSize, ok := <-l.net.maxMessageSizeC:
			if ok {
				buf = make([]byte, newSize)
			}
		default:
		}
	}
}

// Write data to destination.
func (l *Listener) WriteTo(b []byte, addr *net.UDPAddr) (int, error) {
	return l.net.conn.WriteTo(b, addr)
}
