/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
 */

package tun

import (
	"net/netip"
	"os"
)

type Event int

const (
	EventUp = 1 << iota
	EventDown
	EventMTUUpdate
)

type Device interface {
	// File returns the file descriptor of the device.
	File() *os.File

	// Read one packet from the Device (without any additional headers).
	// On a successful read it returns the number of bytes read.
	Read(b []byte) (int, error)

	// Write one packet to the device (without any additional headers).
	// On a successful write it returns the number of bytes written.
	Write(b []byte) (int, error)

	// Set MTU on the device.
	SetMTU(int) error

	// MTU returns the MTU of the Device.
	MTU() (int, error)

	// Name returns the current name of the Device.
	Name() (string, error)

	// Set IP Addresses for the device.
	SetIPAddresses(addresses []netip.Prefix) error

	// Get IP Addresses for the device.
	GetIPAddresses() ([]netip.Prefix, error)

	// Add static route.
	AddRoute(destination netip.Prefix, gateway netip.Addr, metric int) error

	// Remove static route.
	RemoveRoute(destination netip.Prefix, gateway netip.Addr) error

	// Events returns a channel of type Event, which is fed Device events.
	Events() <-chan Event

	// Close stops the Device and closes the Event channel.
	Close() error
}
