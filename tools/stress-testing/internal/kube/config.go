package kube

import (
	"fmt"
	"net/http"
	"path/filepath"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Options struct {
	Kubeconfig string
	Context    string
	QPS        float32
	Burst      int
}

type Clients struct {
	Config     *rest.Config
	HTTPClient *http.Client
	Dynamic    dynamic.Interface
	Discovery  discovery.DiscoveryInterface
	Kubernetes kubernetes.Interface
}

func NewClients(opts Options) (*Clients, error) {
	config, err := RESTConfig(opts)
	if err != nil {
		return nil, err
	}

	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}

	return &Clients{
		Config:     config,
		HTTPClient: httpClient,
		Dynamic:    dynamicClient,
		Discovery:  discoveryClient,
		Kubernetes: kubeClient,
	}, nil
}

func RESTConfig(opts Options) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loadingRules.ExplicitPath = opts.Kubeconfig
	} else if home := homedir.HomeDir(); home != "" {
		loadingRules.ExplicitPath = filepath.Join(home, ".kube", "config")
	}

	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	if opts.QPS > 0 {
		config.QPS = opts.QPS
	}
	if opts.Burst > 0 {
		config.Burst = opts.Burst
	}

	return config, nil
}
