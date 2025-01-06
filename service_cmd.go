package main

import (
	"fmt"

	"github.com/kardianos/service"
)

var ServiceAction = []string{"start", "stop", "status", "restart", "install", "uninstall"}

// Command to manage this service.
type ServiceCmd struct {
	Action struct {
		Action string `arg:"" enum:"${serviceActions}" help:"${serviceActions}" required:""`
	} `arg:""`
}

func (s *ServiceCmd) action() string {
	return s.Action.Action
}

func (s *ServiceCmd) Run() (err error) {
	svc, err := s.service()
	if err != nil {
		return err
	}
	switch s.action() {
	case ServiceAction[0]:
		err = svc.Start()
	case ServiceAction[1]:
		err = svc.Stop()
	case ServiceAction[2]:
		status, err := svc.Status()
		if err == nil {
			switch status {
			case service.StatusRunning:
				fmt.Println("Service is running.")
			case service.StatusStopped:
				fmt.Println("Service is stopped.")
			default:
				fmt.Println("Service is in an unknown state.")
			}
		}
	case ServiceAction[3]:
		err = svc.Restart()
	case ServiceAction[4]:
		err = svc.Install()
	case ServiceAction[5]:
		err = svc.Uninstall()
	}
	if err != nil {
		return err
	}
	if s.action() != ServiceAction[2] {
		fmt.Println("Command executed successfully.")
	}
	return
}

func (s *ServiceCmd) service() (service.Service, error) {
	svcConfig := &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Arguments:   []string{"server"},
	}
	return service.New(s, svcConfig)
}

func (s *ServiceCmd) Start(svc service.Service) error {
	return nil
}

func (s *ServiceCmd) Stop(svc service.Service) error {
	app.Stop <- struct{}{}
	return nil
}
