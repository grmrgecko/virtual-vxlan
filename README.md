# virtual-vxlan

Virtual VXLAN is a tool written to allow VXLAN interfaces to be used on Windows. I may also add support for other operating systems as well, but for now this is a Windows and Linux only project due to immediate needs. The tool uses [Wintun](https://www.wintun.net/) and the [WireGuard-go tun drivers](https://github.com/WireGuard/wireguard-go/tree/master/tun) to make virtual interfaces, then it listens for vxlan packets over UDP and does translations between the tun interface and the vxlan listener.

## Install

You can either download the latest binary from the releases, or you can build this project. For better performance on Windows, install [Npcap](https://npcap.com/#download).

## Building

You can build as follows:

```bash
make deps
make
```

## Running as a service

This project includes service support built in, simply install and start it as follows:

```bash
virtual-vxlan service install
virtual-vxlan service start
```

## Running from cli

If you are developing the software, or need more debug output. It may be worth running from the cli.

```bash
virtual-vxlan server --log-level=debug
```

## Config

The configuration is mainly managed by the service itself, however you may set manual configurations according to the `config.go` file.

## Usage

The cli has extensive help available via the following:

```bash
virtual-vxlan --help
```

Basic setup of a vxlan service is as follows.

- Start the service and/or server.

- Add vxlan listener:
```bash
virtual-vxlan listener vxlan add --address='10.0.0.2:4789' --permanent
```

Note: It is important to use the IP adddress of an interface on the system to allow the interface to be put into promiscuous mode, which is required as otherwise hardware vxlan filtering will block the packets from being received and sent. This is a known issue on Windows, however it has not been tested on other operating systems.

- Add tun interface to the vxlan listener:
```bash
virtual-vxlan listener vxlan interface vxlan20 add --vni=20 --permanent
```

- Set an IP address on the interface:
```bash
virtual-vxlan listener vxlan interface vxlan20 set-ip-addresses --ip-address=192.168.30.0/24
```

- Set a default destination for vxlan packets:
```bash
virtual-vxlan listener vxlan interface vxlan20 add-mac-entry --mac="00:00:00:00:00:00" --destination="10.0.0.3" --permanent
```

- Save the configuration:
```bash
virtual-vxlan config save
```
