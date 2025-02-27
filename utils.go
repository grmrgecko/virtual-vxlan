package main

import (
	"crypto/rand"
	"net"
)

// Generate an random MAC in the locally administered OUI.
func generateRandomMAC() net.HardwareAddr {
	// Start a new MAC address.
	mac := make(net.HardwareAddr, 6)

	// Just replace all bytes in MAC with random bytes.
	rand.Read(mac)
	// Set OUI to locally administered space.
	mac[0] = 0x0a
	mac[1] = 0x00

	return mac
}

// Check if IP address is all zero.
func isZeroAddr(ip net.IP) bool {
	for _, b := range ip {
		if b != 0x0 {
			return false
		}
	}
	return true
}
