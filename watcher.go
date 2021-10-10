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
		// set count of jobs as sum of files in both directories
		wg.Add(len(srcFiles) + len(destFiles))

		// get a list of operations (functions) to execute (files to write\remove in destination directory, based on current source directory contents)
		jobFuncs := processChanges(configs, srcFiles, destFiles, &wg)

		// check if concurrent workers limit is set (0 to disable)
		if configs.General.MaxConcurrentWorkers < 1 {
			// no limit, so run every operation in its own goroutine
			for _, jobFunc := range jobFuncs {
				// to allow for concurrent processing, run operation in new coroutine
				go jobFunc()
			}

			// wait for all created jobs to end
			wg.Wait()
		} else {
			// to enforce concurrent limit of goroutines, will use a buffered channel of functions

			// create a buffered channel of functions, in length of goroutine limit
			workerChannels := make(chan func(), configs.General.MaxConcurrentWorkers)
			// create a channel to signal end of operation
			doneSignal := make(chan int)
			// run multiple (within limit) continues goroutines which will pool operations (functions to execute) from channel
			for i := 0; i < configs.General.MaxConcurrentWorkers; i++ {
				// since the functions is continues, run it in new coroutine not to block execution
				go func() {
					// loop indefinitely
					for {
						select {
						case jobFunc := <-workerChannels:
							// operation is available, so run it in current routine
							jobFunc()
						case <-doneSignal:
							// done, so can break
							return
						}
					}
				}()
			}

			// schedule all operations onto the buffered channel
			for _, jobFunc := range jobFuncs {
				workerChannels <- jobFunc
			}

			// wait for all created jobs to end
			wg.Wait()

			// processed all jobs, so signal all goroutines to break
			for i := 0; i < configs.General.MaxConcurrentWorkers; i++ {
				doneSignal <- 0
			}
		}

		// wait some time before running the next iteration
		time.Sleep(time.Duration(configs.General.LoopIntervalMS) * time.Millisecond)
	}
}

func processChanges(configs Configurations, srcFiles map[string]os.FileInfo, destFiles map[string]os.FileInfo, wg *sync.WaitGroup) []func() {
	// create a container for operations
	var jobFunctions []func()

	// iterate every file in source directory, and mirror any changes to destination directory
	for srcPath, srcFile := range srcFiles {
		// since we will write any updates of the specific path to the destination directory, should remove any idential (relative) path
		// in destination files container so it will not be mistakenly removed later (any files in destFiles container will later be removed)
		if _, exists := destFiles[srcPath]; exists {
			delete(destFiles, srcPath)

			// since we remove record from container, count as -1 in WaitGroup counter
			wg.Done()
		}

		// since operation context will run at later time, parameters must be cached locally otherwise when the function executes, it will be called with corrupted data
		p1 := filepath.Join(configs.General.SourceDirectory, srcPath)
		p2 := srcFile
		p3 := configs.General.DestinationDirectory + srcPath

		// append 'write' operation to functions list
		jobFunctions = append(jobFunctions, func() {
			// run the operation with cached values
			writeFile(p1, p2, p3, wg)
		})
	}

	// any files which still remain in destFiles array, should be removed since no reference of them was iterated previously in srcFiles array
	for dstPath, dstFile := range destFiles {
		// since operation context will run at later time, parameters must be cached locally otherwise when the function executes, it will be called with corrupted data
		p1 := dstFile
		p2 := filepath.Join(configs.General.DestinationDirectory, dstPath)

		// append 'delete' operation to functions list
		jobFunctions = append(jobFunctions, func() {
			// run the operation with cached values
			deleteFile(p1, p2, wg)
		})
	}

	return jobFunctions
}

func validateDirExistance(srcPath, destPath string) {
	// get source file info
	srcPathInfo, err := os.Stat(srcPath)
	if err != nil {
		panic(err)
	}

	// make sure directory has been specified
	if srcPathInfo.IsDir() {
		if _, err := os.Stat(destPath); err == nil {
			// no error, so directory exists, but make sure it matches the source directory permissions
			err = os.Chmod(destPath, srcPathInfo.Mode().Perm())
			if err != nil {
				panic(err)
			}
		} else if errors.Is(err, fs.ErrNotExist) { // check if the error is of expected type (ErrNotExist)
			// directory does not exist, so create it with source directory permissions
			err = os.MkdirAll(destPath, srcPathInfo.Mode().Perm())
			if err != nil {
				panic(err)
			}

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
		err := os.Chmod(path, srcFile.Mode().Perm())
		if err != nil {
			panic(err)
		}
		// set same 'last modified' value as source file so it wont be falsely detected as 'changed' on next iteration
		err = os.Chtimes(path, srcFileModTime, srcFileModTime)
		if err != nil {
			panic(err)
		}

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
	// signal job done at end of func
	defer wg.Done()

	// remove by type
	if file.IsDir() {
		// directory
		err := os.RemoveAll(path)
		if err != nil {
			panic(err)
		}
	} else {
		// file
		err := os.Remove(path)
		if err != nil {
			panic(err)
		}
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
