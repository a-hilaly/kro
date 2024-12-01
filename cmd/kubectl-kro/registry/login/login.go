package login

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type Config struct {
	Auths map[string]Auth `json:"auths"`
}

type Auth struct {
	Auth string `json:"auth"`
}

var Command = &cobra.Command{
	Use:   "login [flags] [registry-url]",
	Short: "Log in to a container registry using AWS credentials",
	Long: `Log in to a container registry using AWS credentials from stdin.
Example:
  aws ecr get-login-password --region us-west-2 | kro registry login 123456789012.dkr.ecr.us-west-2.amazonaws.com
  aws ecr-public get-login-password --region us-east-1 | kro registry login public.ecr.aws`,
	RunE: runLogin,
}

var (
	optUsername      string
	optPasswordStdin bool
)

func init() {
	Command.Flags().StringVar(&optUsername, "username", "AWS", "Registry username")
	Command.Flags().BoolVar(&optPasswordStdin, "password-stdin", false, "Take password from stdin")

	// Since we're always taking password from stdin in our implementation
	Command.Flags().MarkHidden("password-stdin")
	Command.Flags().MarkHidden("username")
}

func runLogin(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("registry URL is required")
	}
	registryURL := args[0]

	// Read password from stdin
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("no input provided")
	}
	token := scanner.Text()

	if optUsername != "AWS" {
		return fmt.Errorf("only AWS authentication is supported")
	}

	if err := storeCredentials(registryURL, token); err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	fmt.Printf("Login Succeeded for %s\n", registryURL)
	return nil
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kro", "registry", "config.json"), nil
}

func loadConfig() (*Config, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
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

	if config.Auths == nil {
		config.Auths = make(map[string]Auth)
	}

	return &config, nil
}

func saveConfig(config *Config) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func storeCredentials(registry, password string) error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("AWS:%s", password)))
	config.Auths[registry] = Auth{Auth: auth}

	return saveConfig(config)
}
