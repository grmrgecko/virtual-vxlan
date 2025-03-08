/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
 */

package tun

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
	_ "unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const (
	rateMeasurementGranularity = uint64((time.Second / 2) / time.Nanosecond)
	spinloopRateThreshold      = 800000000 / 8                                   // 800mbps
	spinloopDuration           = uint64(time.Millisecond / 80 / time.Nanosecond) // ~1gbit/s
)

type rateJuggler struct {
	current       atomic.Uint64
	nextByteCount atomic.Uint64
	nextStartTime atomic.Int64
	changing      atomic.Bool
}

type NativeTun struct {
	wt        *wintun.Adapter
	name      string
	handle    windows.Handle
	rate      rateJuggler
	session   wintun.Session
	readWait  windows.Handle
	events    chan Event
	running   sync.WaitGroup
	closeOnce sync.Once
	close     atomic.Bool
	mtu       int
}

var (
	WintunTunnelType = "vxlan"
	WintunGUIDPrefix = "virtual-vxlan Windows GUID v1"
)

//go:linkname procyield runtime.procyield
func procyield(cycles uint32)

//go:linkname nanotime runtime.nanotime
func nanotime() int64

// Create an 128bit GUID using interface name.
func generateGUIDByDeviceName(name string) (*windows.GUID, error) {
	hash := md5.New()
	_, err := hash.Write([]byte(WintunGUIDPrefix + name))
	if err != nil {
		return nil, err
	}
	sum := hash.Sum(nil)

	return (*windows.GUID)(unsafe.Pointer(&sum[0])), nil
}

// CreateTUN creates a Wintun interface with the given name. Should a Wintun
// interface with the same name exist, it is reused.
func CreateTUN(ifname string, mtu int) (Device, error) {
	guid, err := generateGUIDByDeviceName(ifname)
	if err != nil {
		return nil, err
	}
	return CreateTUNWithRequestedGUID(ifname, guid, mtu)
}

// CreateTUNWithRequestedGUID creates a Wintun interface with the given name and
// a requested GUID. Should a Wintun interface with the same name exist, it is reused.
func CreateTUNWithRequestedGUID(ifname string, requestedGUID *windows.GUID, mtu int) (Device, error) {
	wt, err := wintun.CreateAdapter(ifname, WintunTunnelType, requestedGUID)
	if err != nil {
		return nil, fmt.Errorf("error creating interface: %w", err)
	}

	forcedMTU := 1420
	if mtu > 0 {
		forcedMTU = mtu
	}

	tun := &NativeTun{
		wt:     wt,
		name:   ifname,
		handle: windows.InvalidHandle,
		events: make(chan Event, 10),
		mtu:    forcedMTU,
	}

	err = tun.setMTU(windows.AF_INET, forcedMTU)
	if err != nil {
		wt.Close()
		close(tun.events)
		return nil, fmt.Errorf("error setting MTU for IPv4: %w", err)
	}
	err = tun.setMTU(windows.AF_INET6, forcedMTU)
	if err != nil {
		wt.Close()
		close(tun.events)
		return nil, fmt.Errorf("error setting MTU for IPv6: %w", err)
	}

	tun.session, err = wt.StartSession(0x800000) // Ring capacity, 8 MiB
	if err != nil {
		tun.wt.Close()
		close(tun.events)
		return nil, fmt.Errorf("error starting session: %w", err)
	}
	tun.readWait = tun.session.ReadWaitEvent()
	return tun, nil
}

func (tun *NativeTun) Name() (string, error) {
	return tun.name, nil
}

func (tun *NativeTun) File() *os.File {
	return nil
}

func (tun *NativeTun) Events() <-chan Event {
	return tun.events
}

func (tun *NativeTun) Close() error {
	var err error
	tun.closeOnce.Do(func() {
		tun.close.Store(true)
		windows.SetEvent(tun.readWait)
		tun.running.Wait()
		tun.session.End()
		if tun.wt != nil {
			tun.wt.Close()
		}
		close(tun.events)
	})
	return err
}

func (tun *NativeTun) setMTU(family winipcfg.AddressFamily, mtu int) error {
	luid := winipcfg.LUID(tun.LUID())
	ipif, err := luid.IPInterface(family)
	if err != nil {
		return err
	}
	ipif.NLMTU = uint32(mtu)
	return ipif.Set()
}

func (tun *NativeTun) SetMTU(mtu int) error {
	if tun.close.Load() {
		return nil
	}
	err := tun.setMTU(windows.AF_INET, mtu)
	if err != nil {
		return fmt.Errorf("error setting MTU for IPv4: %v", err)
	}
	err = tun.setMTU(windows.AF_INET6, mtu)
	if err != nil {
		return fmt.Errorf("error setting MTU for IPv6: %v", err)
	}
	update := tun.mtu != mtu
	tun.mtu = mtu
	if update {
		tun.events <- EventMTUUpdate
	}
	return nil
}

func (tun *NativeTun) MTU() (int, error) {
	return tun.mtu, nil
}

// Note: Read() and Write() assume the caller comes only from a single thread; there's no locking.

func (tun *NativeTun) Read(b []byte) (int, error) {
	tun.running.Add(1)
	defer tun.running.Done()
retry:
	if tun.close.Load() {
		return 0, os.ErrClosed
	}
	start := nanotime()
	shouldSpin := tun.rate.current.Load() >= spinloopRateThreshold && uint64(start-tun.rate.nextStartTime.Load()) <= rateMeasurementGranularity*2
	for {
		if tun.close.Load() {
			return 0, os.ErrClosed
		}
		packet, err := tun.session.ReceivePacket()
		switch err {
		case nil:
			n := copy(b, packet)
			tun.session.ReleaseReceivePacket(packet)
			tun.rate.update(uint64(n))
			return n, nil
		case windows.ERROR_NO_MORE_ITEMS:
			if !shouldSpin || uint64(nanotime()-start) >= spinloopDuration {
				windows.WaitForSingleObject(tun.readWait, windows.INFINITE)
				goto retry
			}
			procyield(1)
			continue
		case windows.ERROR_HANDLE_EOF:
			return 0, os.ErrClosed
		case windows.ERROR_INVALID_DATA:
			return 0, errors.New("send ring corrupt")
		}
		return 0, fmt.Errorf("Read failed: %w", err)
	}
}

func (tun *NativeTun) Write(b []byte) (int, error) {
	tun.running.Add(1)
	defer tun.running.Done()
	if tun.close.Load() {
		return 0, os.ErrClosed
	}

	packetSize := len(b)
	tun.rate.update(uint64(packetSize))

	packet, err := tun.session.AllocateSendPacket(packetSize)
	switch err {
	case nil:
		// TODO: Explore options to eliminate this copy.
		copy(packet, b)
		tun.session.SendPacket(packet)
		return packetSize, nil
	case windows.ERROR_HANDLE_EOF:
		return 0, os.ErrClosed
	case windows.ERROR_BUFFER_OVERFLOW:
		return 0, nil // Dropping when ring is full.
	}
	return 0, fmt.Errorf("Write failed: %w", err)
}

// LUID returns Windows interface instance ID.
func (tun *NativeTun) LUID() uint64 {
	tun.running.Add(1)
	defer tun.running.Done()
	if tun.close.Load() {
		return 0
	}
	return tun.wt.LUID()
}

func (tun *NativeTun) SetIPAddresses(addresses []netip.Prefix) error {
	luid := winipcfg.LUID(tun.LUID())

	err := luid.SetIPAddresses(addresses)
	if err != nil {
		return fmt.Errorf("failed to set address: %w", err)
	}

	return nil
}

func addrFromSocketAddress(sockAddr windows.SocketAddress) netip.Addr {
	ip := sockAddr.IP()
	ip4 := ip.To4()
	var addr netip.Addr
	if ip4 != nil {
		addr = netip.AddrFrom4([4]byte(ip4))
	} else {
		addr = netip.AddrFrom16([16]byte(ip))
	}
	return addr
}

func (tun *NativeTun) GetIPAddresses() ([]netip.Prefix, error) {
	luid := winipcfg.LUID(tun.LUID())
	ipAdaters, err := winipcfg.GetAdaptersAddresses(windows.AF_UNSPEC, winipcfg.GAAFlagIncludePrefix|winipcfg.GAAFlagSkipAnycast|winipcfg.GAAFlagSkipMulticast|winipcfg.GAAFlagSkipDNSServer|winipcfg.GAAFlagSkipFriendlyName|winipcfg.GAAFlagSkipDNSInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get IP adapters: %w", err)
	}
	var prefixes []netip.Prefix
	for _, ipAdater := range ipAdaters {
		if ipAdater.LUID == luid {
			unicast := ipAdater.FirstUnicastAddress
			for unicast != nil {
				addr := addrFromSocketAddress(unicast.Address)
				prefix := ipAdater.FirstPrefix
				for prefix != nil {
					pAddr := addrFromSocketAddress(prefix.Address)
					if (pAddr.Is4() && prefix.PrefixLength != 32) || (pAddr.Is6() && prefix.PrefixLength != 128) {
						nPrefix := netip.PrefixFrom(pAddr, int(prefix.PrefixLength))
						if nPrefix.Contains(addr) {
							prefixes = append(prefixes, netip.PrefixFrom(addr, int(prefix.PrefixLength)))
							prefix = nil
							continue
						}
					}
					prefix = prefix.Next
				}
				unicast = unicast.Next
			}
		}
	}
	return prefixes, nil
}

func (tun *NativeTun) AddRoute(destination netip.Prefix, gateway netip.Addr, metric int) error {
	luid := winipcfg.LUID(tun.LUID())
	return luid.AddRoute(destination, gateway, uint32(metric))
}

func (tun *NativeTun) RemoveRoute(destination netip.Prefix, gateway netip.Addr) error {
	luid := winipcfg.LUID(tun.LUID())
	return luid.DeleteRoute(destination, gateway)
}

// RunningVersion returns the running version of the Wintun driver.
func (tun *NativeTun) RunningVersion() (version uint32, err error) {
	return wintun.RunningVersion()
}

func (rate *rateJuggler) update(packetLen uint64) {
	now := nanotime()
	total := rate.nextByteCount.Add(packetLen)
	period := uint64(now - rate.nextStartTime.Load())
	if period >= rateMeasurementGranularity {
		if !rate.changing.CompareAndSwap(false, true) {
			return
		}
		rate.nextStartTime.Store(now)
		rate.current.Store(total * uint64(time.Second/time.Nanosecond) / period)
		rate.nextByteCount.Store(0)
		rate.changing.Store(false)
	}
}
