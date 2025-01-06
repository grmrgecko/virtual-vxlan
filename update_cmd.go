package main

// Command to manage this service.
type UpdateCmd struct{}

func (s *UpdateCmd) Run() (err error) {
	config := ReadMinimalConfig()
	CheckForUpdate(config.Update, false)
	return
}
