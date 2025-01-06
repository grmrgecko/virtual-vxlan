package main

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/kkyr/fig"
	"github.com/shibukawa/configdir"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Just bare minimal configuration for reading in cli calls.
type ConfigMinimal struct {
	RPCPath string        `fig:"rpc_path" yaml:"rpc_path"`
	Update  *UpdateConfig `fig:"update" yaml:"update"`
	Log     *LogConfig    `fig:"log" yaml:"log"`
}

// Main configuration structure.
type Config struct {
	ConfigMinimal
	Listeners []ListenerConfig `fig:"listeners" yaml:"listeners"`
}

type LogConfig struct {
	Level string `fig:"level" yaml:"level" enum:"debug,info,warn,error" default:"info"`
	Type  string `fig:"type" yaml:"type" enum:"json,console" default:"console"`
}

// Configuration for updating.
type UpdateConfig struct {
	Owner          string            `fig:"owner" yaml:"owner"`
	Repo           string            `fig:"repo" yaml:"repo"`
	Disabled       bool              `fig:"disabled" yaml:"disabled"`
	CurrentVersion string            `fig:"-" yaml:"-"`
	ShouldRelaunch bool              `fig:"-" yaml:"-"`
	PreUpdate      func()            `fig:"-" yaml:"-"`
	IsSuccessMsg   func(string) bool `fig:"-" yaml:"-"`
	StartupTimeout time.Duration     `fig:"-" yaml:"-"`
	AbortUpdate    func()            `fig:"-" yaml:"-"`
}

// Listener configuration structure.
type ListenerConfig struct {
	Name           string            `fig:"name" yaml:"name"`
	Address        string            `fig:"address" yaml:"address"`
	MaxMessageSize int               `fig:"max_message_size" yaml:"max_message_size"`
	Interfaces     []InterfaceConfig `fig:"interfaces" yaml:"interfaces"`
}

// Interface configuration strucuture.
type InterfaceConfig struct {
	Name           string           `fig:"name" yaml:"name"`
	VNI            uint32           `fig:"vni" yaml:"vni"`
	MTU            int              `fig:"mtu" yaml:"mtu"`
	MACAddress     string           `fig:"mac_address" yaml:"mac_address"`
	IPAddressCIDRS []string         `fig:"ip_addresess_cidrs" yaml:"ip_addresess_cidrs"`
	ARPEntries     []ARPEntryConfig `fig:"arp_entries" yaml:"arp_entries"`
	MACEntries     []MACEntryConfig `fig:"mac_entries" yaml:"mac_entries"`
}

// Permanent ARP entries.
type ARPEntryConfig struct {
	IPAddress  string `fig:"ip_address" yaml:"ip_address"`
	MACAddress string `fig:"mac_address" yaml:"mac_address"`
}

// Permanent MAC entries.
type MACEntryConfig struct {
	MACAddress  string `fig:"mac_address" yaml:"mac_address"`
	Destination string `fig:"destination" yaml:"destination"`
}

// Applies common filters to the read configuration.
func (c *Config) ApplyFilters() {
	// If the RPC path isn't set, set it to temp dir.
	if c.RPCPath == "" {
		c.RPCPath = filepath.Join(os.TempDir(), "virtual-vxlan.sock")
	}
	// Check if the RPC socket already exists.
	_, err := os.Stat(c.RPCPath)
	if err == nil {
		// If the socket exists, see if its listening.
		_, err = net.Dial("unix", c.RPCPath)

		// If its not listening, remove it to allow us to start.
		if err != nil {
			os.Remove(c.RPCPath)
		}
	}
}

// Get the config path/
func ConfigPath() (fileDir, fileName string) {
	// Find the configuration directory.
	configDirs := configdir.New(serviceVendor, serviceName)
	folders := configDirs.QueryFolders(configdir.System)
	if len(folders) == 0 {
		log.Fatalf("Unable to find config path.")
	}

	// Find the file name.
	fileName = defaultConfigFile
	fileDir = folders[0].Path
	if flags.ConfigPath != "" {
		fileDir, fileName = filepath.Split(flags.ConfigPath)
	}
	return
}

// Makes the default config for reading.
func DefaultConfig() *Config {
	config := new(Config)
	config.Update = &UpdateConfig{
		Owner: "grmrgecko",
		Repo:  "virtual-vxlan",
	}
	config.Log = flags.Log
	return config
}

// Read configuration file and return the current config.
func ReadMinimalConfig() *Config {
	// Setup default minimal config.
	config := DefaultConfig()

	// Find the file name.
	fileDir, fileName := ConfigPath()

	// Read the configuration file if it exists.
	err := fig.Load(&config.ConfigMinimal, fig.File(fileName), fig.Dirs(fileDir))
	// On error, just print as we want to return a default config.
	if err != nil {
		log.Debug("Unable to load config file:", err)
	}

	// Apply config filters.
	config.ApplyFilters()

	// Apply any log configurations loaded from file.
	config.Log.Apply()
	return config
}

// Read configuration file and return the current config.
func ReadConfig() *Config {
	// Setup default config.
	config := DefaultConfig()

	// Find the file name.
	fileDir, fileName := ConfigPath()

	// Read the configuration file if it exists.
	err := fig.Load(config, fig.File(fileName), fig.Dirs(fileDir))
	// On error, just print as we want to return a default config.
	if err != nil {
		log.Debug("Unable to load config file:", err)
	}

	// Apply config filters.
	config.ApplyFilters()

	// Apply any log configurations loaded from file.
	config.Log.Apply()
	return config
}

// Apply the supplied configuration file to this app instance.
func ApplyConfig(config *Config) (err error) {
	// Find listeners and interfaces that are permanent
	// and not in the config being applied. Remove them
	// so that we only have configured permanent entries.
	{
		var listenersToRemove []*Listener
		var interfacesToRemove []*Interface
		// Lock to prevent other actions.
		app.Net.Lock()
		for _, listener := range app.Net.Listeners {
			if listener.Permanent {
				// The address is the pretty name.
				addr := listener.PrettyName()

				// Find listeners in the config that match this address.
				var found *ListenerConfig
				for _, list := range config.Listeners {
					if addr == list.Address && listener.Name == list.Name {
						found = &list
						break
					}
				}

				// If we found a listener, check its interfaces.
				if found != nil {
					// Look for interfaces on this listener which
					// are no longer in the config.
					listener.net.RLock()
					for _, iface := range listener.net.interfaces {
						if iface.Permanent {
							// Match by both name and VNI, either change
							// and we need to remake it.
							name := iface.Name()
							vni := iface.VNI()

							// Loop to find.
							foundIfce := false
							for _, ifce := range found.Interfaces {
								if ifce.Name == name && ifce.VNI == vni {
									foundIfce = true
								}
							}
							// If we didn't find this interface, add to remove list.
							if !foundIfce {
								interfacesToRemove = append(interfacesToRemove, iface)
							}
						}
					}
					listener.net.RUnlock()
				} else {
					// This listener wasn't found, lets remove it.
					listenersToRemove = append(listenersToRemove, listener)
				}
			}
		}

		// Unlock net as we're done looking at lists and we
		// need it unlocked to remove items.
		app.Net.Unlock()

		// Remove listeners not found.
		for _, list := range listenersToRemove {
			list.Close()
		}

		// Remove interfaces not found.
		for _, ifce := range interfacesToRemove {
			ifce.Close()
		}
	}

	// Loop through listeners in the config to add/change the
	// configurations of both listeners and their interfaces.
	for _, listener := range config.Listeners {
		// First check to see if an existing listener is there.
		var l *Listener
		app.Net.Lock()
		for _, list := range app.Net.Listeners {
			if list.PrettyName() == listener.Address {
				l = list
				l.Permanent = true
				break
			}
		}
		app.Net.Unlock()

		// If no existing listeners, add a new listener.
		if l == nil {
			l, err = NewListener(listener.Name, listener.Address, listener.MaxMessageSize, true)
			if err != nil {
				return fmt.Errorf("failed to start listener: %s %v", listener.Address, err)
			}
		} else {
			// If the listener was already existing, update the max message size.
			l.SetMaxMessageSize(listener.MaxMessageSize)
		}

		// Loop through interfaces on this listener to add/upate them.
		for _, iface := range listener.Interfaces {
			// See if this interface is already on the listener.
			var i *Interface
			l.net.RLock()
			for _, ifce := range l.net.interfaces {
				if ifce.VNI() == iface.VNI {
					i = ifce
					i.Permanent = true
				}
			}
			l.net.RUnlock()

			// If this interface isn't existing, add it.
			if i == nil {
				i, err = NewInterface(iface.Name, iface.VNI, iface.MTU, l, true)
				if err != nil {
					return fmt.Errorf("failed to make interface: %s %v", iface.Name, err)
				}
			} else {
				// If the interface is existing, update the MTU.
				i.SetMTU(iface.MTU)
			}

			// Parse the interface's MAC address.
			mac, err := net.ParseMAC(iface.MACAddress)
			if err != nil {
				return fmt.Errorf("failed tp parse MAC: %s %v", iface.MACAddress, err)
			}

			// Set the interface's MAC address.
			err = i.SetMACAddress(mac)
			if err != nil {
				return fmt.Errorf("failed to set MAC address %s on interface %s: %v", iface.MACAddress, iface.Name, err)
			}

			// Parse the interface's IP addresses CIDRs.
			var prefixes []netip.Prefix
			for _, addr := range iface.IPAddressCIDRS {
				prefix, err := netip.ParsePrefix(addr)
				if err != nil {
					return fmt.Errorf("failed to parse CIDR: %s %v", addr, err)
				}
				prefixes = append(prefixes, prefix)
			}

			// If IP addresses are set for this interface, set them.
			if len(prefixes) != 0 {
				err = i.SetIPAddresses(prefixes)
				if err != nil {
					return fmt.Errorf("failed to set IP addresses on interface: %s %v", iface.Name, err)
				}
			}

			// Flush the ARP table of any permanent entry.
			i.tables.Lock()
			found := true
			for found {
				found = false
				for p, ent := range i.tables.arp {
					if ent.Permanent {
						found = true
						i.tables.arp = append(i.tables.arp[:p], i.tables.arp[p+1:]...)
						break
					}
				}
			}
			i.tables.Unlock()

			// Add permanent ARP entries from the config.
			for _, ent := range iface.ARPEntries {
				addr, err := netip.ParseAddr(ent.IPAddress)
				if err != nil {
					return fmt.Errorf("failed to parse IP: %s %v", ent.IPAddress, err)
				}
				mac, err := net.ParseMAC(ent.MACAddress)
				if err != nil {
					return fmt.Errorf("failed to parse MAC: %s %v", ent.MACAddress, err)
				}
				i.AddStaticARPEntry(addr, mac, true)
			}

			// Flush the MAC table of any permanent entry.
			i.tables.Lock()
			found = true
			for found {
				found = false
				for p, ent := range i.tables.mac {
					if ent.Permanent {
						found = true
						i.tables.mac = append(i.tables.mac[:p], i.tables.mac[p+1:]...)
						break
					}
				}
			}
			i.tables.Unlock()

			// Add permanent entries from this config.
			for _, ent := range iface.MACEntries {
				mac, err := net.ParseMAC(ent.MACAddress)
				if err != nil {
					return fmt.Errorf("failed to parse MAC: %s %v", ent.MACAddress, err)
				}
				dst := net.ParseIP(ent.Destination)
				if dst == nil {
					return fmt.Errorf("failed to parse destination: %s", ent.Destination)
				}
				i.AddMACEntry(mac, dst, true)
			}
		}
	}

	// No errors occurred, configuration is applied.
	return nil
}

// Take the current application state and save permanent configurations
// to the configuration file.
func SaveConfig() error {
	// Start a new configuration file.
	config := new(Config)
	config.RPCPath = app.grpcServer.RPCPath
	config.Update = app.UpdateConfig
	config.Log = flags.Log

	// Look the global app config during this process.
	app.Net.Lock()
	defer app.Net.Unlock()

	// Loop through listeners and add permanent listeners to the config.
	for _, listener := range app.Net.Listeners {
		// If this listener is permanent, add it.
		if listener.Permanent {
			// Make a listener config.
			listnr := ListenerConfig{
				Address:        listener.PrettyName(),
				MaxMessageSize: listener.MaxMessageSize(),
				Name:           listener.Name,
			}

			// Loop through interfaces on this listener and add to the config.
			listener.net.Lock()
			for _, iface := range listener.net.interfaces {
				// Only add interface if its permanent.
				if iface.Permanent {
					// Get the MAC Address.
					mac, err := iface.GetMACAddress()
					if err != nil {
						return err
					}
					// Get the IP addresses on this interface.
					ipAddrs, err := iface.GetIPAddresses()
					if err != nil {
						return err
					}

					// Make the config for this interface.
					ifce := InterfaceConfig{
						Name:       iface.Name(),
						VNI:        iface.VNI(),
						MTU:        iface.MTU(),
						MACAddress: mac.String(),
					}

					// Add the CIDRs for this interface.
					for _, addr := range ipAddrs {
						ifce.IPAddressCIDRS = append(ifce.IPAddressCIDRS, addr.String())
					}

					// Get and add permanent ARP entries to the config.
					for _, ent := range iface.GetARPEntries() {
						if ent.Permanent {
							entry := ARPEntryConfig{
								IPAddress:  ent.Addr.String(),
								MACAddress: ent.MAC.String(),
							}
							ifce.ARPEntries = append(ifce.ARPEntries, entry)
						}
					}

					// Get and add permanent MAC enties to the config.
					for _, ent := range iface.GetMACEntries() {
						if ent.Permanent {
							entry := MACEntryConfig{
								MACAddress:  ent.MAC.String(),
								Destination: ent.Dst.IP.String(),
							}
							ifce.MACEntries = append(ifce.MACEntries, entry)
						}
					}

					// Add this interface config to the listener config.
					listnr.Interfaces = append(listnr.Interfaces, ifce)
				}
			}
			listener.net.Unlock()

			// Add this listener to the list of listeners in the config.
			config.Listeners = append(config.Listeners, listnr)
		}
	}

	// Encode YAML data.
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	// Find the file name.
	fileDir, fileName := ConfigPath()

	// Verify directory exists.
	if _, ferr := os.Stat(fileDir); ferr != nil {
		err = os.MkdirAll(fileDir, 0755)
		if err != nil {
			log.Error("Failed to make directory:", err)
		}
	}

	// Write the configuration file.
	err = os.WriteFile(filepath.Join(fileDir, fileName), data, 0644)
	return err
}

func (l *LogConfig) Apply() {
	switch l.Level {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	default:
		log.SetLevel(log.ErrorLevel)
	}
	switch l.Type {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	default:
		log.SetFormatter(&log.TextFormatter{})
	}
}
