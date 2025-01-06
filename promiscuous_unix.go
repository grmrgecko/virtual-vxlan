//go:build !windows

package main

import "net"

// I am focusing on Windows development currently, may fill this out if I find Linux needing promiscuous mode as well.
// Also may work on darwin support/bsd, depending on how I feel.
// For now, this is just a stub to make code happy.

// Structure to store connections.
type Promiscuous struct {
}

// Set interface to promiscuous mode, using the interface IP to identify the interface.
func SetInterfacePromiscuous(ifaceIP net.IP) (promisc *Promiscuous, err error) {
	return new(Promiscuous), nil
}

// Close promiscuous mode connection.
func (p *Promiscuous) Close() error {
	return nil
}
