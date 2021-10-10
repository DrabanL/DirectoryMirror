package main

import (
	"fmt"
	"os"
)

func main() {
	// we expect one or more config files provided via args

	// make sure at least one config was specified
	if len(os.Args) < 2 {
		panic("Config file name argument is missing")
	}

	// first argument is the application path, so ignore it and get other args
	configFiles := os.Args[1:]

	// iterate every configuration and initialize watcher job for it
	for _, config := range ReadFromFile(configFiles) {
		// run watcher job in coroutine to allow multiple jobs to run concurrently
		go RunScanLoop(config)
	}

	fmt.Println("Running, press Enter key to terminate")

	// use scanln to allow the application to continue running until user wish to terminate
	fmt.Scanln()
}
