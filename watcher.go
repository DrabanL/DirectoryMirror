package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"path/filepath"
)

func RunScanLoop(configs Configurations) {
	fmt.Printf("Watching '%s' and mirroring into '%s' every %vms\r\n", configs.General.SourceDirectory, configs.General.DestinationDirectory, configs.General.LoopIntervalMS)

	// run infinite loop, to scan for changes continuously
	for {
		// get files in source and destination directory
		srcFiles := getDirFiles(configs.General.SourceDirectory)
		destFiles := getDirFiles(configs.General.DestinationDirectory)

		// use a WaitGroup to be able to wait for all jobs to end before running the next iteration
		var wg sync.WaitGroup
		// write\remove any files in destination directory, based on current source directory contents
		processChanges(configs, srcFiles, destFiles, &wg)
		// wait for all created jobs to end
		wg.Wait()

		// wait some time before running the next iteration
		time.Sleep(time.Duration(configs.General.LoopIntervalMS) * time.Millisecond)
	}
}

func processChanges(configs Configurations, srcFiles map[string]os.FileInfo, destFiles map[string]os.FileInfo, wg *sync.WaitGroup) {
	// iterate every file in source directory, and mirror any changes to destination directory
	for srcPath, srcFile := range srcFiles {
		// since we will write any updates of the specific path to the destination directory, should remove any idential (relative) path
		// in destination files container so it will not be mistakenly removed later (any files in destFiles container will later be removed)
		delete(destFiles, srcPath)

		// to allow for concurrent processing, write files in new coroutine
		go writeFile(filepath.Join(configs.General.SourceDirectory, srcPath), srcFile, configs.General.DestinationDirectory+srcPath, wg)
	}

	// any files which still remain in destFiles array, should be removed since no reference of them was iterated previously in srcFiles array
	for dstPath, dstFile := range destFiles {
		// to allow for concurrent processing, delete files in new coroutine
		go deleteFile(dstFile, filepath.Join(configs.General.DestinationDirectory, dstPath), wg)
	}
}

func validateDirExistance(srcPath, destPath string) {
	// get source file info
	srcPathInfo, _ := os.Stat(srcPath)

	// make sure directory has been specified
	if srcPathInfo.IsDir() {
		if _, err := os.Stat(destPath); err == nil {
			// no error, so directory exists, but make sure it matches the source directory permissions
			os.Chmod(destPath, srcPathInfo.Mode().Perm())
		} else if errors.Is(err, fs.ErrNotExist) { // check if the error is of expected type (ErrNotExist)
			// directory does not exist, so create it with source directory permissions
			os.Mkdir(destPath, srcPathInfo.Mode().Perm())

			fmt.Printf("%v | Write | %s\r\n", time.Now().Format("15:04:05"), destPath)
		} else {
			// unexpected error
			panic(err)
		}
	} else {
		// extract file's parent directory name from provided path, and validate its existance
		validateDirExistance(filepath.Dir(srcPath), filepath.Dir(destPath))
	}
}

func writeFile(srcPath string, srcFile os.FileInfo, path string, wg *sync.WaitGroup) {
	// count this func context as job to perform (runs in coroutine)
	wg.Add(1)
	// signal job done at end of func
	defer wg.Done()

	// make sure destination directory exists
	validateDirExistance(srcPath, path)

	// ignore directories
	if !srcFile.IsDir() {
		// check if file has changed using 'last modified' time
		srcFileModTime := srcFile.ModTime()
		// check destination file mod time
		if file, err := os.Stat(path); err == nil {
			// file exists, but compare last modification time against source file
			if file.ModTime() == srcFileModTime {
				// file is unchanged
				return
			}
		} else if !errors.Is(err, fs.ErrNotExist) { // check if the error is of expected type (ErrNotExist)
			// unexpected error
			panic(err)
		}

		// at this point, file does not exist (or removed previously) so create it (copy source file)
		copyFile(srcPath, path)
		// set same permission as source file
		os.Chmod(path, srcFile.Mode().Perm())
		// set same 'last modified' value as source file so it wont be falsely detected as 'changed' on next iteration
		os.Chtimes(path, srcFileModTime, srcFileModTime)

		fmt.Printf("%v | Write | %s\r\n", time.Now().Format("15:04:05"), path)
	}
}

func copyFile(src string, dst string) {
	// try to get source file info
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		panic(err)
	}

	// make sure its a file and not something else (directory)
	if !sourceFileStat.Mode().IsRegular() {
		return
	}

	// try to open source file for read
	source, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	// make sure to close file before end of context
	defer source.Close()

	// try to create dest file
	destination, err := os.Create(dst)
	if err != nil {
		panic(err)
	}
	// make sure to close file before end of context
	defer destination.Close()

	// copy src binary contents to dst
	written, err := io.Copy(destination, source)
	if err != nil {
		panic(err)
	}

	// make sure all bytes were written
	if written != sourceFileStat.Size() {
		panic(fmt.Sprintf("written != sourceFileStat.Size(); %v != %v", written, sourceFileStat.Size()))
	}
}

func deleteFile(file os.FileInfo, path string, wg *sync.WaitGroup) {
	// count this func context as job to perform (runs in coroutine)
	wg.Add(1)
	// signal job done at end of func
	defer wg.Done()

	// remove by type
	if file.IsDir() {
		// directory
		os.RemoveAll(path)
	} else {
		// file
		os.Remove(path)
	}

	fmt.Printf("%v | Remove | %s\r\n", time.Now().Format("15:04:05"), path)
}

func getDirFiles(srcDir string) map[string]os.FileInfo {
	// create a container for files
	files := make(map[string]os.FileInfo)
	// try to get all directory files (including subdirs or subfiles)
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		// ignore root path dir
		if srcDir != path {
			// get relative file path
			relativePath := strings.Replace(path, srcDir, "", 1)
			// add file to container
			files[relativePath] = info
		}

		return nil
	})

	if err != nil {
		panic(err)
	}

	return files
}
