package kube

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type Forwarder struct {
	localPort int
	stopCh    chan struct{}
	closeOnce sync.Once
}

func (f *Forwarder) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", f.localPort)
}

func (f *Forwarder) Close() {
	f.closeOnce.Do(func() {
		close(f.stopCh)
	})
}

func PortForwardService(
	ctx context.Context,
	config *rest.Config,
	client kubernetes.Interface,
	namespace string,
	serviceName string,
	remotePort int,
) (*Forwarder, error) {
	pod, err := servicePod(ctx, client, namespace, serviceName)
	if err != nil {
		return nil, err
	}

	return PortForwardPod(ctx, config, namespace, pod.Name, remotePort)
}

func PortForwardPod(
	ctx context.Context,
	config *rest.Config,
	namespace string,
	podName string,
	remotePort int,
) (*Forwarder, error) {
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, fmt.Errorf("create port-forward transport: %w", err)
	}

	serverURL, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("parse API host: %w", err)
	}
	serverURL.Path = fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)

	localPort, err := freePort()
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)
	stderr := &bytes.Buffer{}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)
	forwarder, err := portforward.NewOnAddresses(
		dialer,
		[]string{"127.0.0.1"},
		[]string{fmt.Sprintf("%d:%d", localPort, remotePort)},
		stopCh,
		readyCh,
		&bytes.Buffer{},
		stderr,
	)
	if err != nil {
		close(stopCh)
		return nil, fmt.Errorf("create port-forwarder: %w", err)
	}

	go func() {
		errCh <- forwarder.ForwardPorts()
	}()

	select {
	case <-readyCh:
		return &Forwarder{localPort: localPort, stopCh: stopCh}, nil
	case err := <-errCh:
		close(stopCh)
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("port-forward failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-time.After(15 * time.Second):
		close(stopCh)
		return nil, fmt.Errorf("timed out waiting for port-forward readiness")
	case <-ctx.Done():
		close(stopCh)
		return nil, ctx.Err()
	}
}

func servicePod(ctx context.Context, client kubernetes.Interface, namespace, serviceName string) (*corev1.Pod, error) {
	service, err := client.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get service %s/%s: %w", namespace, serviceName, err)
	}
	if len(service.Spec.Selector) == 0 {
		return nil, fmt.Errorf("service %s/%s has no selector", namespace, serviceName)
	}

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(service.Spec.Selector).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list service pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("service %s/%s has no matching pods", namespace, serviceName)
	}

	sort.SliceStable(pods.Items, func(i, j int) bool {
		left := podScore(&pods.Items[i])
		right := podScore(&pods.Items[j])
		if left != right {
			return left > right
		}
		return pods.Items[i].CreationTimestamp.Time.After(pods.Items[j].CreationTimestamp.Time)
	})

	return &pods.Items[0], nil
}

func podScore(pod *corev1.Pod) int {
	score := 0
	if pod.Status.Phase == corev1.PodRunning {
		score += 2
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			score++
			break
		}
	}
	return score
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate local port: %w", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", listener.Addr())
	}
	return addr.Port, nil
}
