package cmd

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/awslabs/kro/api/v1alpha1"
)

var (
	optInputFile  string
	optOutputFile string
)

func init() {
	Command.PersistentFlags().StringVarP(&optInputFile, "file", "f", "", "input ResourceGroup file")
	Command.PersistentFlags().StringVarP(&optOutputFile, "output", "o", "", "output file (default: stdout)")
	Command.MarkPersistentFlagRequired("file")
}

var Command = &cobra.Command{
	Use:   "package",
	Short: "Package a ResourceGroup into an OCI image",
	RunE:  runPackage,
}

func runPackage(cmd *cobra.Command, args []string) error {
	// Read and validate ResourceGroup
	content, err := os.ReadFile(optInputFile)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	var rg v1alpha1.ResourceGroup
	if err := yaml.UnmarshalStrict(content, &rg); err != nil {
		return fmt.Errorf("failed to parse ResourceGroup: %w", err)
	}

	// Create layer containing ResourceGroup
	layerBuf := new(bytes.Buffer)
	layerDigest, size, err := createLayer(layerBuf, "resourcegroup.yaml", content)
	if err != nil {
		return fmt.Errorf("failed to create layer: %w", err)
	}

	// Create image config
	now := time.Now()
	config := v1.Image{
		Created: &now,
		Config: v1.ImageConfig{
			Labels: map[string]string{
				"kro.run/type": "resourcegroup",
				"kro.run/name": rg.Name,
			},
		},
		RootFS: v1.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{layerDigest},
		},
	}

	configJson, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	configDigest := digest.FromBytes(configJson)

	// Create manifest
	manifest := v1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Config: v1.Descriptor{
			MediaType: v1.MediaTypeImageConfig,
			Digest:    configDigest,
			Size:      int64(len(configJson)),
		},
		Layers: []v1.Descriptor{{
			MediaType: v1.MediaTypeImageLayer,
			Digest:    layerDigest,
			Size:      size,
		}},
	}

	// Write output
	var output io.Writer
	if optOutputFile != "" {
		f, err := os.Create(optOutputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		output = f
	} else {
		output = os.Stdout
	}

	tw := tar.NewWriter(output)
	defer tw.Close()

	// Write manifest
	manifestJson, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := writeTarFile(tw, "manifest.json", manifestJson); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	// Write config
	configFileName := strings.TrimPrefix(configDigest.String(), "sha256:")
	if err := writeTarFile(tw, configFileName, configJson); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Write layer
	layerFileName := strings.TrimPrefix(layerDigest.String(), "sha256:")
	if err := writeTarFile(tw, layerFileName, layerBuf.Bytes()); err != nil {
		return fmt.Errorf("failed to write layer: %w", err)
	}
	return nil
}

func createLayer(w io.Writer, filename string, content []byte) (digest.Digest, int64, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	// Create proper tar header
	header := &tar.Header{
		Name: filename,
		Mode: 0644,
		Size: int64(len(content)),
	}

	// Write header properly
	if err := tw.WriteHeader(header); err != nil {
		return "", 0, fmt.Errorf("failed to write tar header: %w", err)
	}

	// Write actual content
	if _, err := tw.Write(content); err != nil {
		return "", 0, fmt.Errorf("failed to write content: %w", err)
	}

	if err := tw.Close(); err != nil {
		return "", 0, fmt.Errorf("failed to close tar writer: %w", err)
	}

	layerContent := buf.Bytes()
	dgst := digest.FromBytes(layerContent)
	size := int64(len(layerContent))

	_, err := io.Copy(w, buf)
	if err != nil {
		return "", 0, fmt.Errorf("failed to write layer content: %w", err)
	}

	return dgst, size, nil
}

func writeTarFile(tw *tar.Writer, name string, content []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Now(),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("failed to write tar content: %w", err)
	}

	return nil
}
