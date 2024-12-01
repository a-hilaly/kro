package publish

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	optFile string
	optTag  string
)

// Config matches the registry config format
type Config struct {
	Auths map[string]Auth `json:"auths"`
}

// Auth holds registry authentication details
type Auth struct {
	Auth string `json:"auth"`
}

var Command = &cobra.Command{
	Use:   "publish [flags] [repository]",
	Short: "Publish a ResourceGroup to a container registry",
	Long: `Publish a ResourceGroup package to a container registry.
Example:
  kro publish -f image.tar 123456789012.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest`,
	RunE: runPublish,
}

func init() {
	Command.Flags().StringVarP(&optFile, "file", "f", "", "ResourceGroup package file")
	Command.Flags().StringVarP(&optTag, "tag", "t", "", "Image tag (e.g. latest)")
	Command.MarkFlagRequired("file")
}

func runPublish(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("repository URL is required")
	}
	repository := args[0]

	// Split repository and tag if provided in repository arg
	if optTag == "" {
		parts := strings.Split(repository, ":")
		if len(parts) > 1 {
			repository = parts[0]
			optTag = parts[1]
		} else {
			optTag = "latest"
		}
	}

	// Read the tar file
	content, err := os.ReadFile(optFile)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Get credentials from config
	registry := strings.Split(repository, "/")[0]
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load registry config: %w", err)
	}

	auth, ok := config.Auths[registry]
	if !ok {
		return fmt.Errorf("no credentials found for %s, please run 'kro registry login' first", registry)
	}

	// Push the image
	if err := pushImage(repository, optTag, content, auth.Auth); err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}

	fmt.Printf("Successfully published %s:%s\n", repository, optTag)
	return nil
}

func pushImage(repository, tag string, content []byte, auth string) error {
	client := &http.Client{}

	// Parse repository and build proper path
	parts := strings.Split(repository, "/")
	registry := parts[0]
	repoName := strings.Join(parts[1:], "/")

	// Calculate digest for the content
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))

	// First initiate upload for the blob
	uploadURL := fmt.Sprintf("https://%s/v2/%s/blobs/uploads/", registry, repoName)
	req, err := http.NewRequest("POST", uploadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to initiate upload: %s: %s", resp.Status, string(body))
	}

	// Get upload URL and add digest
	location := resp.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("no upload URL received")
	}
	if !strings.Contains(location, "?") {
		location += "?"
	} else {
		location += "&"
	}
	location += fmt.Sprintf("digest=%s", digest)

	// Push the blob content
	req, err = http.NewRequest("PUT", location, bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(content)))

	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to push content: %s: %s", resp.Status, string(body))
	}

	// Create proper OCI manifest
	manifest := struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
		Config        struct {
			MediaType string `json:"mediaType"`
			Size      int    `json:"size"`
			Digest    string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			MediaType string `json:"mediaType"`
			Size      int    `json:"size"`
			Digest    string `json:"digest"`
		} `json:"layers"`
	}{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: struct {
			MediaType string `json:"mediaType"`
			Size      int    `json:"size"`
			Digest    string `json:"digest"`
		}{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Size:      len(content),
			Digest:    digest,
		},
		Layers: []struct {
			MediaType string `json:"mediaType"`
			Size      int    `json:"size"`
			Digest    string `json:"digest"`
		}{{
			MediaType: "application/vnd.oci.image.layer.v1.tar",
			Size:      len(content),
			Digest:    digest,
		}},
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Push the manifest
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repoName, tag)
	req, err = http.NewRequest("PUT", manifestURL, bytes.NewReader(manifestJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")

	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to push manifest: %s: %s", resp.Status, string(body))
	}

	return nil
}

func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(home, ".kro", "registry", "config.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Auths: make(map[string]Auth)}, nil
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
