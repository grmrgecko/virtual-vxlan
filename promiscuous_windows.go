package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"unsafe"

	"github.com/google/gopacket/pcap"
	log "github.com/sirupsen/logrus"
)

// Constants for setting promiscuous mode on windows. See Microsoft's documentation below:
// https://learn.microsoft.com/en-us/windows/win32/winsock/sio-rcvall
const (
	SIO_RCVALL = syscall.IOC_IN | syscall.IOC_VENDOR | 1

	RCVALL_OFF             = 0
	RCVALL_ON              = 1
	RCVALL_SOCKETLEVELONLY = 2
	RCVALL_IPLEVEL         = 3
)

// Structure to store connections.
type Promiscuous struct {
	conn net.PacketConn
	pcap *pcap.Handle
}

// Set interface to promiscuous mode, using the interface IP to identify the interface.
func SetInterfacePromiscuous(ifaceIP net.IP) (promisc *Promiscuous, err error) {
	promisc = new(Promiscuous)
	// I have found npcap to be most performant, however due to its commercial nature,
	// I do not include it in this project. You may install it to get its benefit,
	// or the alternative will come into play. I am open to hearing of better solutions
	// to this packet filtering problem, and better ideas.
	err = promisc.tryPCap(ifaceIP)
	if err != nil {
		// If it fails because wpcap.dll is missing, try alternative method.
		if strings.Contains(err.Error(), "wpcap.dll") {
			log.Debug("Missing npcap, putting interface into promiscuous mode using SIO_RCVALL.")
			// Put interface in promiscuous using ICMP listener.
			err = promisc.tryICMPListen(ifaceIP)
			if err != nil {
				promisc = nil
				return
			}
			// If regular error, just return the error.
		} else {
			promisc = nil
			return
		}
	}
	return
}

// Use npcap to put interface in promiscuous mode.
func (p *Promiscuous) tryPCap(ifaceIP net.IP) (err error) {
	// Find the windows interface name for the adapter with the IP we're binding.
	ifs, err := pcap.FindAllDevs()
	if err != nil {
		return
	}
	foundIface := false
	var piface pcap.Interface
	for _, piface = range ifs {
		// Find matching IP address.
		for _, paddr := range piface.Addresses {
			// If we found it, stop to keep interface reference.
			if paddr.IP.Equal(ifaceIP) {
				foundIface = true
				break
			}
		}
		// Stop here to prevent reference from being replaced.
		if foundIface {
			break
		}
	}
	// If we didn't find the interface being bound to, stop here.
	if !foundIface {
		return fmt.Errorf("unable to find the interface with vxlan")
	}

	// Open the pcap connection to put the interface in promiscuous mode.
	log.Debugf("Putting adapter in promiscuous mode: %s (%s)", piface.Description, piface.Name)
	p.pcap, err = pcap.OpenLive(piface.Name, 1, true, pcap.BlockForever)
	if err != nil {
		return
	}
	// To prevent the pcap from receiving packets, filter to just localhost
	// traffic which should result in zero packets to capture.
	err = p.pcap.SetBPFFilter("host 127.0.0.1")
	if err != nil {
		return
	}
	return
}

// Use SIO_RCVALL to put interface in promiscuous mode.
func (p *Promiscuous) tryICMPListen(ifaceIP net.IP) (err error) {
	// We need the syscall handle to put the interface in promiscuous mode with WSAIoctl.
	var socketHandle syscall.Handle

	// Use listen config to get the syscall handle via the control function.
	cfg := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(s uintptr) {
				socketHandle = syscall.Handle(s)
			})
		},
	}

	// Depending on IP address network, setup ICMP network.
	network := "ip4:icmp"
	if ifaceIP.To4() == nil {
		network = "ip6:ipv6-icmp"
	}

	// Use listen packet to start a connection.
	p.conn, err = cfg.ListenPacket(context.Background(), network, ifaceIP.String())
	if err != nil {
		return
	}

	// Put interface in promiscuous mode.
	cbbr := uint32(0)
	flag := uint32(RCVALL_ON)
	size := uint32(unsafe.Sizeof(flag))
	err = syscall.WSAIoctl(socketHandle, SIO_RCVALL, (*byte)(unsafe.Pointer(&flag)), size, nil, 0, &cbbr, nil, 0)
	if err != nil {
		p.conn.Close()
		return
	}
	go p.connReader()
	return
}

// Read and discard packets read.
func (p *Promiscuous) connReader() {
	buf := make([]byte, 500)
	for {
		_, _, err := p.conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			log.Errorf("received error reading in promiscuous: %v", err)
		}
	}
}

// Close promiscuous mode connection.
func (p *Promiscuous) Close() (err error) {
	if p.pcap != nil {
		p.pcap.Close()
	} else {
		err = p.conn.Close()
	}
	return
}
