package install

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/awslabs/kro/api/v1alpha1"
	kroclient "github.com/awslabs/kro/internal/client"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var Command = &cobra.Command{
	Use:   "install [registry-url]",
	Short: "Install a ResourceGroup from a container registry",
	Long: `Install a ResourceGroup package from a container registry.
Example:
  kro install 123456789012.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest`,
	RunE: runInstall,
}

var (
	optNamespace string
)

func init() {
	Command.Flags().StringVarP(&optNamespace, "namespace", "n", "default", "Target namespace")
}

func runInstall(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("repository URL is required")
	}

	// Parse repository and tag
	repo := args[0]
	parts := strings.Split(repo, ":")
	repository := parts[0]
	tag := "latest"
	if len(parts) > 1 {
		tag = parts[1]
	}

	// Parse registry
	registry := strings.Split(repository, "/")[0]

	// Get registry credentials
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load registry config: %w", err)
	}

	auth, ok := config.Auths[registry]
	if !ok {
		return fmt.Errorf("no credentials found for %s, please run 'kro registry login' first", registry)
	}

	// Pull the ResourceGroup content
	content, err := pullResourceGroup(repository, tag, auth.Auth)
	if err != nil {
		return fmt.Errorf("failed to pull ResourceGroup: %w", err)
	}

	// Parse the ResourceGroup
	var rg v1alpha1.ResourceGroup
	if err := yaml.UnmarshalStrict(content, &rg); err != nil {
		return fmt.Errorf("failed to parse ResourceGroup: %w", err)
	}

	// Create kubernetes client
	client, err := kroclient.NewSet(kroclient.Config{})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	obj := &unstructured.Unstructured{}
	rgData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&rg)
	if err != nil {
		return fmt.Errorf("failed to convert ResourceGroup to unstructured: %w", err)
	}
	obj.SetUnstructuredContent(rgData)

	// Create the ResourceGroup in the cluster
	gvr := schema.GroupVersionResource{
		Group:    v1alpha1.GroupVersion.Group,
		Version:  v1alpha1.GroupVersion.Version,
		Resource: "resourcegroups",
	}
	_, err = client.Dynamic().Resource(gvr).Namespace(optNamespace).Create(
		cmd.Context(),
		obj,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create ResourceGroup: %w", err)
	}

	fmt.Printf("Successfully installed ResourceGroup %s in namespace %s\n", rg.Name, optNamespace)
	return nil
}

func pullResourceGroup(repository, tag, auth string) ([]byte, error) {
	client := &http.Client{}

	// Parse repository parts
	registry := strings.Split(repository, "/")[0]
	repoName := strings.Join(strings.Split(repository, "/")[1:], "/")
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repoName, tag)

	// Get the manifest
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get manifest: %s: %s", resp.Status, string(body))
	}

	// Parse the manifest to get the package digest
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	// Pull the package blob
	packageDigest := manifest.Config.Digest
	packageURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repoName, packageDigest)
	req, err = http.NewRequest("GET", packageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get package: %s: %s", resp.Status, string(body))
	}

	// Read the package tar
	packageTar := tar.NewReader(resp.Body)

	// Find the layer.tar file in the package
	layerFileName := "layer.tar"
	var layerReader io.Reader
	for {
		hdr, err := packageTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read package: %w", err)
		}

		if hdr.Name == layerFileName {
			layerReader = packageTar
			break
		}
	}

	if layerReader == nil {
		return nil, fmt.Errorf("layer.tar not found in package")
	}

	// Extract the resourcegroup.yaml file from the layer.tar
	layerTar := tar.NewReader(layerReader)
	for {
		hdr, err := layerTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read layer: %w", err)
		}

		if hdr.Name == "resourcegroup.yaml" {
			var content bytes.Buffer
			if _, err := io.Copy(&content, layerTar); err != nil {
				return nil, fmt.Errorf("failed to read resourcegroup.yaml: %w", err)
			}
			return content.Bytes(), nil
		}
	}

	return nil, fmt.Errorf("resourcegroup.yaml not found in layer")
}

type Config struct {
	Auths map[string]Auth `json:"auths"`
}

type Auth struct {
	Auth string `json:"auth"`
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
