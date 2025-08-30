package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func (cfg apiConfig) getS3ObjectURL(key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getLocalAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func getAssetPath(mediaType string) string {
	base := make([]byte, 32)
	_, err := rand.Read(base)
	if err != nil {
		panic("Failed to generate a random 32 byte")
	}
	id := base64.RawURLEncoding.EncodeToString(base)
	ext := getMediaTypeExtension(mediaType)
	return fmt.Sprintf("%s.%s", id, ext)
}

func getMediaTypeExtension(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return parts[1]
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command(
		"ffprobe", 
		"-v", "error", 
		"-print_format", "json", 
		"-show_streams", 
		filePath,
	)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffprobe error: %w", err)
	}
	var output struct {
		Streams []struct {
			Width int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err = json.Unmarshal(buffer.Bytes(), &output); err != nil {
		return "", fmt.Errorf("couldn't parse the command's output: %w", err) 
	}

	if len(output.Streams) == 0 {
		return "", fmt.Errorf("no video streams found")
	}

	width, height := output.Streams[0].Width, output.Streams[0].Height
	gcd := gcd(width, height)
	widthRatio := width / gcd 
	heightRatio := height / gcd

	return fmt.Sprintf("%d:%d", widthRatio, heightRatio), nil
}

func gcd(a, b int) int {
	if b == 0 {
		return 1
 	}
	r := a % b 
	for ; r != 0; {
		a, b = b, r 
		r = a % b
	}
	return b 
}