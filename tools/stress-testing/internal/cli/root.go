package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
)

type RootOptions struct {
	Kubeconfig string
	Context    string
	QPS        float32
	Burst      int
}

func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := RootOptions{
		QPS:   500,
		Burst: 1000,
	}

	if home := homedir.HomeDir(); home != "" {
		opts.Kubeconfig = filepath.Join(home, ".kube", "config")
	}

	rootCmd := &cobra.Command{
		Use:           "krostress",
		Short:         "Stress-test a live KRO controller and capture performance data",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	rootCmd.PersistentFlags().StringVar(&opts.Kubeconfig, "kubeconfig", opts.Kubeconfig, "path to kubeconfig")
	rootCmd.PersistentFlags().StringVar(&opts.Context, "context", "", "kubeconfig context")
	rootCmd.PersistentFlags().Float32Var(&opts.QPS, "qps", opts.QPS, "client QPS")
	rootCmd.PersistentFlags().IntVar(&opts.Burst, "burst", opts.Burst, "client burst")

	rootCmd.AddCommand(newStressCommand(&opts))
	rootCmd.AddCommand(newObserveCommand(&opts))
	rootCmd.AddCommand(newPprofCommand(&opts))
	rootCmd.AddCommand(newRunCommand(&opts))

	return rootCmd
}
