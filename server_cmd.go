package main

import (
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/coreos/go-systemd/daemon"
	"github.com/kardianos/service"
	log "github.com/sirupsen/logrus"
)

// Flags for the server command.
type ServerCmd struct {
}

// The main App structure.
type App struct {
	Net struct {
		Listeners []*Listener
		sync.Mutex
	}
	ControllerMac net.HardwareAddr
	grpcServer    *GRPCServer
	Stop          chan struct{}
	UpdateConfig  *UpdateConfig
}

var app *App

// Run the server.
func (a *ServerCmd) Run() error {
	// Start a new app structure.
	app = new(App)
	app.ControllerMac = net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	app.Stop = make(chan struct{})
	{
		// Read the configuration from file.
		config := ReadConfig()
		app.UpdateConfig = config.Update

		// Start the GRPC server for cli communication.
		_, err := NewGRPCServer(config.RPCPath)
		if err != nil {
			return err
		}

		// Apply the configuration in the background, to allow service start to notify
		// the service managers fast.
		go func() {
			err = ApplyConfig(config)
			// If error applying the config, log.
			if err != nil {
				log.Println("An error occurred applying configuration:", err)
			}
		}()
	}

	// Send notification that the service is ready.
	daemon.SdNotify(false, daemon.SdNotifyReady)

	// Setup service.
	if !service.Interactive() {
		s := new(ServiceCmd)
		svc, err := s.service()
		if err != nil {
			return err
		}
		go svc.Run()
	}

	// Run the update loop to check for updates.
	go app.RunUpdateLoop()

	// Inform that the service has started.
	log.Println("Service started.")

	// Monitor common signals.
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Run program signal handler.
sigLoop:
	for {
		// Check for a signal.
		select {
		case sig := <-c:
			switch sig {
			// If hangup signal receivied, reload the configurations.
			case syscall.SIGHUP:
				log.Println("Reloading configurations.")

				// Read the config.
				config := ReadConfig()
				app.UpdateConfig = config.Update

				// Apply any changes.
				err := ApplyConfig(config)
				if err != nil {
					log.Println(err)
				}

				// The default signal is either termination or interruption,
				// so we should stop the loop.
			default:
				break sigLoop
			}
			// If the app stops itself, mark as done.
		case <-app.Stop:
			break sigLoop
		}
	}

	// We're quitting, close out all listeners.
	for len(app.Net.Listeners) >= 1 {
		app.Net.Listeners[0].Close()
	}

	// Stop the grpc server.
	if app.grpcServer != nil {
		app.grpcServer.Close()
	}

	return nil
}
