package pprof

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func Collect(ctx context.Context, endpoint, outputDir, profileType string, cpuSeconds int) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL(endpoint, profileType, cpuSeconds), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch %s profile: %w", profileType, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return "", fmt.Errorf("pprof returned %d: %s", response.StatusCode, string(body))
	}

	filename := fmt.Sprintf("%s-%s.prof", profileType, time.Now().UTC().Format("20060102-150405"))
	outputPath := filepath.Join(outputDir, filename)

	file, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, response.Body); err != nil {
		return "", fmt.Errorf("write profile: %w", err)
	}

	return outputPath, nil
}

func ListProfiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".prof" {
			continue
		}
		profiles = append(profiles, filepath.Join(dir, entry.Name()))
	}
	return profiles, nil
}

func buildURL(endpoint, profileType string, cpuSeconds int) string {
	switch profileType {
	case "cpu":
		if cpuSeconds <= 0 {
			cpuSeconds = 30
		}
		return fmt.Sprintf("%s/debug/pprof/profile?seconds=%d", endpoint, cpuSeconds)
	default:
		return fmt.Sprintf("%s/debug/pprof/%s", endpoint, profileType)
	}
}
