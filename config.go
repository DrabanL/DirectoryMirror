package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Configurations struct {
	General GeneralConfigurations
}

type GeneralConfigurations struct {
	SourceDirectory      string
	DestinationDirectory string
	LoopIntervalMS       int
	MaxConcurrentWorkers int
}

func ReadFromFile(filePaths []string) []Configurations {
	// create a container for our configs
	configs := make([]Configurations, 0)

	// iterate every config file path and attempt to read it
	for _, arg := range filePaths {
		// read configuration from file, transform it to configuration type, and add to config container
		configs = append(configs, fromFile(arg))
	}

	return configs
}

func fromFile(name string) Configurations {
	// get the directory of provided path
	configDir := filepath.Dir(name)

	// check if path was provided
	if len(configDir) < 1 {
		// since no path has been specified, use root app path
		viper.AddConfigPath(".")
		// in case no path has been provided, the value should be the file name itself
		viper.SetConfigName(name)
	} else {
		// path has been specified, so use that
		viper.AddConfigPath(configDir)
		// get file name by replacing dir with empty string
		configFileName := strings.Replace(name, configDir, "", 1)
		viper.SetConfigName(configFileName)
	}

	// set the expected config file type
	viper.SetConfigType("yml")

	// try to read the file
	if err := viper.ReadInConfig(); err != nil {
		errMsg := fmt.Sprintf("Error reading config file; %s\r\n", err)
		panic(errMsg)
	}

	// set defaults, if was not provided
	viper.SetDefault("general.loopIntervalMS", 60000)
	viper.SetDefault("general.maxConcurrentWorkers", 100)

	var config Configurations
	// try to transform to configuration type
	err := viper.Unmarshal(&config)
	if err != nil {
		errMsg := fmt.Sprintf("Error decoding config file; %s\r\n", err)
		panic(errMsg)
	}

	// make sure mandatory configs has been set

	if len(config.General.DestinationDirectory) < 1 {
		panic("Destination directory is not configured")
	}
	if len(config.General.SourceDirectory) < 1 {
		panic("Source directory is not configured")
	}

	return config
}
