/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
 */

package tun

/* Implementation of the TUN device interface for linux
 */

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/rwcancel"
)

const (
	cloneDevicePath = "/dev/net/tun"
	ifReqSize       = unix.IFNAMSIZ + 64
)

type NativeTun struct {
	tunFile                 *os.File
	index                   int32      // if index
	errors                  chan error // async error handling
	events                  chan Event // device related events
	netlinkSock             int
	netlinkCancel           *rwcancel.RWCancel
	hackListenerClosed      sync.Mutex
	statusListenersShutdown chan struct{}

	closeOnce sync.Once

	nameOnce  sync.Once // guards calling initNameCache, which sets following fields
	nameCache string    // name of interface
	nameErr   error
}

func (tun *NativeTun) File() *os.File {
	return tun.tunFile
}

func (tun *NativeTun) routineHackListener() {
	defer tun.hackListenerClosed.Unlock()
	/* This is needed for the detection to work across network namespaces
	 * If you are reading this and know a better method, please get in touch.
	 */
	last := 0
	const (
		up   = 1
		down = 2
	)
	for {
		sysconn, err := tun.tunFile.SyscallConn()
		if err != nil {
			return
		}
		err2 := sysconn.Control(func(fd uintptr) {
			_, err = unix.Write(int(fd), nil)
		})
		if err2 != nil {
			return
		}
		switch err {
		case unix.EINVAL:
			if last != up {
				// If the tunnel is up, it reports that write() is
				// allowed but we provided invalid data.
				tun.events <- EventUp
				last = up
			}
		case unix.EIO:
			if last != down {
				// If the tunnel is down, it reports that no I/O
				// is possible, without checking our provided data.
				tun.events <- EventDown
				last = down
			}
		default:
			return
		}
		select {
		case <-time.After(time.Second):
			// nothing
		case <-tun.statusListenersShutdown:
			return
		}
	}
}

func createNetlinkSocket() (int, error) {
	sock, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return -1, err
	}
	saddr := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR,
	}
	err = unix.Bind(sock, saddr)
	if err != nil {
		return -1, err
	}
	return sock, nil
}

func (tun *NativeTun) routineNetlinkListener() {
	defer func() {
		unix.Close(tun.netlinkSock)
		tun.hackListenerClosed.Lock()
		close(tun.events)
		tun.netlinkCancel.Close()
	}()

	for msg := make([]byte, 1<<16); ; {
		var err error
		var msgn int
		for {
			msgn, _, _, _, err = unix.Recvmsg(tun.netlinkSock, msg[:], nil, 0)
			if err == nil || !rwcancel.RetryAfterError(err) {
				break
			}
			if !tun.netlinkCancel.ReadyRead() {
				tun.errors <- fmt.Errorf("netlink socket closed: %w", err)
				return
			}
		}
		if err != nil {
			tun.errors <- fmt.Errorf("failed to receive netlink message: %w", err)
			return
		}

		select {
		case <-tun.statusListenersShutdown:
			return
		default:
		}

		wasEverUp := false
		for remain := msg[:msgn]; len(remain) >= unix.SizeofNlMsghdr; {

			hdr := *(*unix.NlMsghdr)(unsafe.Pointer(&remain[0]))

			if int(hdr.Len) > len(remain) {
				break
			}

			switch hdr.Type {
			case unix.NLMSG_DONE:
				remain = []byte{}

			case unix.RTM_NEWLINK:
				info := *(*unix.IfInfomsg)(unsafe.Pointer(&remain[unix.SizeofNlMsghdr]))
				remain = remain[hdr.Len:]

				if info.Index != tun.index {
					// not our interface
					continue
				}

				if info.Flags&unix.IFF_RUNNING != 0 {
					tun.events <- EventUp
					wasEverUp = true
				}

				if info.Flags&unix.IFF_RUNNING == 0 {
					// Don't emit EventDown before we've ever emitted EventUp.
					// This avoids a startup race with HackListener, which
					// might detect Up before we have finished reporting Down.
					if wasEverUp {
						tun.events <- EventDown
					}
				}

				tun.events <- EventMTUUpdate

			default:
				remain = remain[hdr.Len:]
			}
		}
	}
}

func getIFIndex(name string) (int32, error) {
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM|unix.SOCK_CLOEXEC,
		0,
	)
	if err != nil {
		return 0, err
	}

	defer unix.Close(fd)

	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCGIFINDEX),
		uintptr(unsafe.Pointer(&ifr[0])),
	)

	if errno != 0 {
		return 0, errno
	}

	return *(*int32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])), nil
}

func (tun *NativeTun) setMTU(n int) error {
	name, err := tun.Name()
	if err != nil {
		return err
	}

	// open datagram socket
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM|unix.SOCK_CLOEXEC,
		0,
	)
	if err != nil {
		return err
	}

	defer unix.Close(fd)

	// do ioctl call
	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	*(*uint32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = uint32(n)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCSIFMTU),
		uintptr(unsafe.Pointer(&ifr[0])),
	)

	if errno != 0 {
		return fmt.Errorf("failed to set MTU of TUN device: %w", errno)
	}

	return nil
}

func (tun *NativeTun) SetMTU(mtu int) error {
	return tun.setMTU(mtu)
}

func (tun *NativeTun) SetIPAddresses(addresses []netip.Prefix) error {
	link, err := netlink.LinkByIndex(int(tun.index))
	if err != nil {
		return err
	}
	var setAddr []*netlink.Addr
	for _, address := range addresses {
		addr, err := netlink.ParseAddr(address.String())
		if err != nil {
			return err
		}
		setAddr = append(setAddr, addr)
	}
	addrList, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	for _, address := range addrList {
		found := -1
		for i, addr := range setAddr {
			if addr.Equal(address) {
				found = i
			}
		}
		if found == -1 {
			err = netlink.AddrDel(link, &address)
			if err != nil {
				return err
			}
		} else {
			setAddr = append(setAddr[:found], setAddr[found+1:]...)
		}
	}
	for _, addr := range setAddr {
		err = netlink.AddrAdd(link, addr)
		if err != nil {
			return err
		}
	}
	return nil
}

func (tun *NativeTun) GetIPAddresses() ([]netip.Prefix, error) {
	link, err := netlink.LinkByIndex(int(tun.index))
	if err != nil {
		return nil, err
	}
	var prefixes []netip.Prefix
	addrList, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	for _, address := range addrList {
		prefix, err := netip.ParsePrefix(address.String())
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func (tun *NativeTun) AddRoute(destination netip.Prefix, gateway netip.Addr, metric int) error {
	family := netlink.FAMILY_V4
	if gateway.Is6() {
		family = netlink.FAMILY_V6
	}
	staticRoute := &netlink.Route{
		Family:    family,
		LinkIndex: int(tun.index),
		Dst: &net.IPNet{
			IP:   destination.Addr().AsSlice(),
			Mask: net.CIDRMask(destination.Bits(), destination.Addr().BitLen()),
		},
		Gw:       gateway.AsSlice(),
		Priority: metric,
	}
	return netlink.RouteAdd(staticRoute)
}

func (tun *NativeTun) RemoveRoute(destination netip.Prefix, gateway netip.Addr) error {
	family := netlink.FAMILY_V4
	if gateway.Is6() {
		family = netlink.FAMILY_V6
	}
	staticRoute := &netlink.Route{
		Family:    family,
		LinkIndex: int(tun.index),
		Dst: &net.IPNet{
			IP:   destination.Addr().AsSlice(),
			Mask: net.CIDRMask(destination.Bits(), destination.Addr().BitLen()),
		},
		Gw: gateway.AsSlice(),
	}
	return netlink.RouteDel(staticRoute)
}

func (tun *NativeTun) MTU() (int, error) {
	name, err := tun.Name()
	if err != nil {
		return 0, err
	}

	// open datagram socket
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM|unix.SOCK_CLOEXEC,
		0,
	)
	if err != nil {
		return 0, err
	}

	defer unix.Close(fd)

	// do ioctl call

	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCGIFMTU),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	if errno != 0 {
		return 0, fmt.Errorf("failed to get MTU of TUN device: %w", errno)
	}

	return int(*(*int32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ]))), nil
}

func (tun *NativeTun) Name() (string, error) {
	tun.nameOnce.Do(tun.initNameCache)
	return tun.nameCache, tun.nameErr
}

func (tun *NativeTun) initNameCache() {
	tun.nameCache, tun.nameErr = tun.nameSlow()
}

func (tun *NativeTun) nameSlow() (string, error) {
	sysconn, err := tun.tunFile.SyscallConn()
	if err != nil {
		return "", err
	}
	var ifr [ifReqSize]byte
	var errno syscall.Errno
	err = sysconn.Control(func(fd uintptr) {
		_, _, errno = unix.Syscall(
			unix.SYS_IOCTL,
			fd,
			uintptr(unix.TUNGETIFF),
			uintptr(unsafe.Pointer(&ifr[0])),
		)
	})
	if err != nil {
		return "", fmt.Errorf("failed to get name of TUN device: %w", err)
	}
	if errno != 0 {
		return "", fmt.Errorf("failed to get name of TUN device: %w", errno)
	}
	return unix.ByteSliceToString(ifr[:]), nil
}

func (tun *NativeTun) Write(b []byte) (int, error) {
	n, err := tun.tunFile.Write(b)
	if errors.Is(err, syscall.EBADFD) {
		return 0, os.ErrClosed
	}
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (tun *NativeTun) Read(b []byte) (int, error) {
	select {
	case err := <-tun.errors:
		return 0, err
	default:
		n, err := tun.tunFile.Read(b)
		if errors.Is(err, syscall.EBADFD) {
			err = os.ErrClosed
		}
		if err != nil {
			return 0, err
		}
		return n, nil
	}
}

func (tun *NativeTun) Events() <-chan Event {
	return tun.events
}

func (tun *NativeTun) Close() error {
	var err1, err2 error
	tun.closeOnce.Do(func() {
		if tun.statusListenersShutdown != nil {
			close(tun.statusListenersShutdown)
			if tun.netlinkCancel != nil {
				err1 = tun.netlinkCancel.Cancel()
			}
		} else if tun.events != nil {
			close(tun.events)
		}
		err2 = tun.tunFile.Close()
	})
	if err1 != nil {
		return err1
	}
	return err2
}

func (tun *NativeTun) initFromFlags(name string) error {
	sc, err := tun.tunFile.SyscallConn()
	if err != nil {
		return err
	}
	if e := sc.Control(func(fd uintptr) {
		var (
			ifr *unix.Ifreq
		)
		ifr, err = unix.NewIfreq(name)
		if err != nil {
			return
		}
		err = unix.IoctlIfreq(int(fd), unix.TUNGETIFF, ifr)
		if err != nil {
			return
		}
	}); e != nil {
		return e
	}
	return err
}

// CreateTUN creates a Device with the provided name and MTU.
func CreateTUN(name string, mtu int) (Device, error) {
	nfd, err := unix.Open(cloneDevicePath, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("CreateTUN(%q) failed; %s does not exist", name, cloneDevicePath)
		}
		return nil, err
	}

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return nil, err
	}
	// IFF_VNET_HDR enables the "tun status hack" via routineHackListener()
	// where a null write will return EINVAL indicating the TUN is up.
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI | unix.IFF_VNET_HDR)
	err = unix.IoctlIfreq(nfd, unix.TUNSETIFF, ifr)
	if err != nil {
		return nil, err
	}

	err = unix.SetNonblock(nfd, true)
	if err != nil {
		unix.Close(nfd)
		return nil, err
	}

	// Note that the above -- open,ioctl,nonblock -- must happen prior to handing it to netpoll as below this line.

	fd := os.NewFile(uintptr(nfd), cloneDevicePath)
	return CreateTUNFromFile(fd, mtu)
}

// CreateTUNFromFile creates a Device from an os.File with the provided MTU.
func CreateTUNFromFile(file *os.File, mtu int) (Device, error) {
	tun := &NativeTun{
		tunFile:                 file,
		events:                  make(chan Event, 5),
		errors:                  make(chan error, 5),
		statusListenersShutdown: make(chan struct{}),
	}

	name, err := tun.Name()
	if err != nil {
		return nil, err
	}

	err = tun.initFromFlags(name)
	if err != nil {
		return nil, err
	}

	// start event listener
	tun.index, err = getIFIndex(name)
	if err != nil {
		return nil, err
	}

	tun.netlinkSock, err = createNetlinkSocket()
	if err != nil {
		return nil, err
	}
	tun.netlinkCancel, err = rwcancel.NewRWCancel(tun.netlinkSock)
	if err != nil {
		unix.Close(tun.netlinkSock)
		return nil, err
	}

	tun.hackListenerClosed.Lock()
	go tun.routineNetlinkListener()
	go tun.routineHackListener() // cross namespace

	err = tun.setMTU(mtu)
	if err != nil {
		unix.Close(tun.netlinkSock)
		return nil, err
	}

	return tun, nil
}

// CreateUnmonitoredTUNFromFD creates a Device from the provided file
// descriptor.
func CreateUnmonitoredTUNFromFD(fd int) (Device, string, error) {
	err := unix.SetNonblock(fd, true)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), "/dev/tun")
	tun := &NativeTun{
		tunFile: file,
		events:  make(chan Event, 5),
		errors:  make(chan error, 5),
	}
	name, err := tun.Name()
	if err != nil {
		return nil, "", err
	}
	err = tun.initFromFlags(name)
	if err != nil {
		return nil, "", err
	}
	return tun, name, err
}
