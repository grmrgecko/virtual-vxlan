package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/go-systemd/daemon"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/hashicorp/go-version"
	log "github.com/sirupsen/logrus"
)

// Check for update and update if one is available.
func Update(c *UpdateConfig) error {
	log.Println("Checking for update.")
	// Setup source.
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return err
	}

	// Get the path to ourself.
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("could not locate executable path: %s", err)
	}
	updateDir, cmd := filepath.Split(exe)
	oldSavePath := filepath.Join(updateDir, fmt.Sprintf(".%s.old", cmd))

	// Get updater with source and validator.
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:      source,
		Validator:   &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		OldSavePath: oldSavePath,
	})
	if err != nil {
		return err
	}

	// Find the latest release.
	release, found, err := updater.DetectLatest(context.Background(), selfupdate.NewRepositorySlug(c.Owner, c.Repo))
	if err != nil {
		return err
	}
	if !found {
		log.Println("No updates available.")
		return nil
	}

	// Compare the versions.
	thisVersion, err := version.NewVersion(c.CurrentVersion)
	if err != nil {
		return err
	}
	latestVersion, err := version.NewVersion(release.Version())
	if err != nil {
		return err
	}

	// If an update isn't available, end.
	if !thisVersion.LessThan(latestVersion) {
		log.Println("No updates available.")
		return nil
	}
	log.Println("Updating to version:", release.Version())

	// We're updating, tell the app so services can be stopped.
	c.PreUpdate()

	// Perform the update.
	err = updater.UpdateTo(context.Background(), release, exe)

	// If update failed, rollback and tell the app we failed.
abortUpdate:
	if err != nil {
		rerr := os.Rename(oldSavePath, exe)
		if rerr != nil {
			log.Println("Failed to rollback update:", rerr)
		}
		c.AbortUpdate()
		return err
	}
	log.Println("Updated.")

	// If relaunch is requested, then start the new exe and confirm it works.
	if c.ShouldRelaunch {
		// Make process and get its stdout/stderr.
		log.Println("Starting new process.")
		p := exec.Command(exe, os.Args[1:]...)
		p.Env = os.Environ()
		p.Env = append(p.Env, "UPDATER_UPDATE=1")
		stdout, err := p.StdoutPipe()
		if err != nil {
			goto abortUpdate
		}
		stderr, err := p.StderrPipe()
		if err != nil {
			goto abortUpdate
		}

		// Start process.
		err = p.Start()
		if err != nil {
			goto abortUpdate
		}

		// Set timeout to kill process.
		timer := time.AfterFunc(c.StartupTimeout, func() {
			p.Process.Kill()
		})

		// Channels to confirm success or failure.
		success := make(chan bool)
		failure := make(chan error)

		// Scan the stdout for success message.
		go func() {
			stdoutScanner := bufio.NewScanner(stdout)
			for stdoutScanner.Scan() {
				line := stdoutScanner.Text()
				fmt.Fprintln(os.Stdout, line)
				if c.IsSuccessMsg(line) {
					success <- true
					return
				}
			}
		}()

		// Scan the stderr for sucess message.
		go func() {
			stderrScanner := bufio.NewScanner(stderr)
			for stderrScanner.Scan() {
				line := stderrScanner.Text()
				fmt.Fprintln(os.Stderr, line)
				if c.IsSuccessMsg(line) {
					success <- true
					return
				}
			}
		}()

		// Wait for command exit, and pass errors.
		go func() {
			err := p.Wait()
			failure <- err
		}()

		// Wait for one of the channels to be sent information.
		select {
		case <-success:
		case err = <-failure:
			if err == nil {
				err = errors.New("program exited without error")
			}
		}

		// Stop the timer as the process is either going to be left running or is stopped.
		timer.Stop()

		// If stop due to error, abort.
		if err != nil {
			goto abortUpdate
		}

		// The update was successful, so we can remove the old binary.
		os.Remove(oldSavePath)

		// Set the new process stdio to ours.
		p.Stdout = os.Stdout
		p.Stderr = os.Stderr
		p.Stdin = os.Stdin

		// Notify systemd to watch the new process.
		// Those babysitting fees are too high to have it watch us too.
		// Amy said SystemD doesn't do babysitting.
		daemon.SdNotify(false, fmt.Sprintf("MAINPID=%d", p.Process.Pid))

		// Quit this process as the new process will continue running as a fork.
		os.Exit(0)
	}

	// The update was successful, so we can remove the old binary.
	os.Remove(oldSavePath)

	return nil
}

// Check for updates, and apply.
func CheckForUpdate(c *UpdateConfig, relaunch bool) {
	// Set update config local variables.
	c.CurrentVersion = serviceVersion
	c.ShouldRelaunch = relaunch
	c.PreUpdate = func() {
		// If no app defined, stop here.
		if app == nil {
			return
		}

		// Stop all listeners to allow updated service to start.
		for len(app.Net.Listeners) >= 1 {
			app.Net.Listeners[0].Close()
		}

		// Stop the grpc server.
		if app.grpcServer != nil {
			app.grpcServer.Close()
		}
	}
	c.IsSuccessMsg = func(msg string) bool {
		if strings.Contains(msg, "Service started.") {
			return true
		}
		return false
	}
	c.StartupTimeout = 5 * time.Minute
	// If update is aborted, we should restart the service.
	c.AbortUpdate = func() {
		// If no app defined, stop here.
		if app == nil {
			return
		}

		// Read the configuration from file.
		config := ReadConfig()

		// Start the GRPC server for cli communication.
		_, err := NewGRPCServer(config.RPCPath)
		if err != nil {
			log.Fatalln(err)
		}

		// Apply the configuration read.
		err = ApplyConfig(config)
		// If error applying the config, we should fail.
		if err != nil {
			log.Fatalln(err)
		}
	}
	err := Update(c)
	if err != nil {
		log.Println("Failure checking for update:", err)
	}
}

// Every 24 hours, check for updates.
func (a *App) RunUpdateLoop() {
	// If disabled, don't run loop.
	if app.UpdateConfig.Disabled {
		return
	}

	// Randomly check for updates at first start.
	if os.Getenv("UPDATER_UPDATE") != "1" && rand.Intn(20) == 2 {
		CheckForUpdate(app.UpdateConfig, true)
	}

	// Run update check every 24 hours.
	for {
		nextUpdate := time.Hour * 24
		nextUpdate += time.Duration(rand.Intn(18000)) * time.Second
		time.Sleep(nextUpdate)
		CheckForUpdate(app.UpdateConfig, true)
	}
}
