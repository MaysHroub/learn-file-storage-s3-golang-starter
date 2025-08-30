package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

func processVideoForFastStart(filePath string) (string, error) {
	processedFilepath := fmt.Sprintf("%s.processing", filePath)
	cmd := exec.Command(
		"ffmpeg",
		"-i", filePath,
		"-codec", "copy",
		"-movflags", "faststart",
		"-f", "mp4",
		processedFilepath,
	)
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to execute command ffmpeg: %w", err)
	}
	fileInfo, err := os.Stat(processedFilepath)
	if err != nil {
		return "", fmt.Errorf("couldn't get the stat of processed file: %w", err)
	}
	if fileInfo.Size() == 0 {
		return "", fmt.Errorf("processed file is empty")
	}
	return processedFilepath, nil 
}