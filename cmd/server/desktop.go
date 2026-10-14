package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type launchOptions struct {
	desktop, version bool
	clientPath       string
}

func parseLaunchOptions(args []string) (launchOptions, error) {
	var options launchOptions
	flags := flag.NewFlagSet("omnihub-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.desktop, "desktop", false, "open native Qt client after the service is ready")
	flags.BoolVar(&options.version, "version", false, "print build identity")
	flags.StringVar(&options.clientPath, "client-path", "", "native executable override (requires --desktop)")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 || (!options.desktop && options.clientPath != "") {
		return options, errors.New("usage: omnihub-server [--desktop [--client-path executable]] [--version]")
	}
	return options, nil
}

func startDesktop(clientPath string) error {
	if clientPath == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		name := "omnihub_client"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		clientPath = filepath.Join(filepath.Dir(executable), name)
	}
	if !filepath.IsAbs(clientPath) {
		return errors.New("client path must be absolute")
	}
	info, err := os.Stat(clientPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("native client must be a regular executable")
	}
	command := exec.Command(clientPath)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	go func() {
		if err := command.Wait(); err != nil {
			log.Printf("native client exited: %v", err)
		}
	}()
	return nil
}
