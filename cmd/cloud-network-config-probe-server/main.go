package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

const (
	// The name of the configmap used for leader election
	resourceLockNamePrefix    = "cloud-network-config-probe-server"
	controllerNameEnvVar      = "CONTROLLER_NAME"
	controllerNamespaceEnvVar = "CONTROLLER_NAMESPACE"
	controllerImageEnvVar     = "CONTROLLER_IMAGE"
	controllerNodeNameEnvVar  = "NODE_NAME"
)

var (
	kubeConfig          string
	platformCfg         cloudprovider.CloudProviderConfig
	secretName          string
	configName          string
	controllerName      string
	controllerNamespace string
	controllerImage     string
	controllerNodeName  string
	resourceLockName    string
)

func main() {
	// set up a global context used for shutting down the leader election and
	// subsequently all controllers.
	// ctx, cancelFunc := context.WithCancel(context.Background())
	ctx, _ := context.WithCancel(context.Background())

	// set up signals so we handle the first shutdown signal gracefully
	// stopCh := signals.SetupSignalHandler(cancelFunc)

	// Skip passing the master URL, if debugging this controller: provide the
	// kubeconfig to your cluster. In all other cases: clientcmd will just infer
	// the in-cluster config from the environment variables in the pod.
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		klog.Exitf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Exitf("Error building kubernetes clientset: %s", err.Error())
	}

	rl, err := resourcelock.New(
		resourcelock.ConfigMapsLeasesResourceLock,
		controllerNamespace,
		resourceLockName,
		kubeClient.CoreV1(),
		kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: controllerName,
		})
	if err != nil {
		klog.Exitf("Error building resource lock: %s", err.Error())
	}

	// set up leader election, the only reason for this is to make sure we only
	// have one replica of this controller at any given moment in time. On
	// upgrades there could be small windows where one replica of the deployment
	// stops on one node while another starts on another. In such a case we
	// could have both running at the same time. This prevents that from
	// happening and ensures we only have one replica "controlling", always.
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            rl,
		ReleaseOnCancel: true,
		LeaseDuration:   137 * time.Second, // leader election values from https://github.com/openshift/library-go/pull/1104
		RenewDeadline:   107 * time.Second,
		RetryPeriod:     26 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// Listen for incoming connections.
				l, err := net.Listen("tcp", ":9999")
				if err != nil {
					fmt.Println("Error listening:", err.Error())
					os.Exit(1)
				}
				// Close the listener when the application closes.
				defer l.Close()
				klog.Info("Listening on :9999")
				for {
					// Listen for an incoming connection.
					conn, err := l.Accept()
					if err != nil {
						fmt.Println("Error accepting: ", err.Error())
						os.Exit(1)
					}
					// Handle connections in a new goroutine.
					go handleRequest(conn)
				}
			},
			OnStoppedLeading: func() {
			},
		},
	})
	klog.Info("Finished executing controlled shutdown")
}

func handleRequest(conn net.Conn) {
	defer conn.Close()

	_, err := conn.Write([]byte("OK\n"))
	if err != nil {
		klog.Infof("Could not write to connection: %v", err)
		return
	}
}

func init() {
	klog.InitFlags(nil)

	// These are arguments for this controller
	flag.StringVar(&secretName, "secret-name", "", "The cloud provider secret name - used for talking to the cloud API.")
	flag.StringVar(&configName, "config-name", "kube-cloud-config", "The cloud provider config name - used for talking to the cloud API.")
	flag.StringVar(&platformCfg.PlatformType, "platform-type", "", "The cloud provider platform type this component is running on.")
	flag.StringVar(&platformCfg.Region, "platform-region", "", "The cloud provider platform region the cluster is deployed in, required for AWS")
	flag.StringVar(&platformCfg.APIOverride, "platform-api-url", "", "The cloud provider API URL to use (instead of whatever default).")
	flag.StringVar(&platformCfg.CredentialDir, "secret-override", "/etc/secret/cloudprovider", "The cloud provider secret location override, useful when running this component locally against a cluster")
	flag.StringVar(&platformCfg.ConfigDir, "config-override", "/kube-cloud-config", "The cloud provider config location override, useful when running this component locally against a cluster")
	flag.StringVar(&platformCfg.AzureEnvironment, "platform-azure-environment", "AzurePublicCloud", "The Azure environment name, used to select API endpoints")
	flag.StringVar(&platformCfg.AWSCAOverride, "platform-aws-ca-override", "", "Path to a separate CA bundle to use when connecting to the AWS API")
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.Parse()

	// These are populated by the downward API
	controllerNamespace = os.Getenv(controllerNamespaceEnvVar)
	controllerName = os.Getenv(controllerNameEnvVar)
	controllerImage = os.Getenv(controllerImageEnvVar)
	controllerNodeName = os.Getenv(controllerNodeNameEnvVar)
	resourceLockName = strings.Join([]string{resourceLockNamePrefix, controllerNodeName}, "-")
	if controllerNamespace == "" || controllerName == "" {
		klog.Exit("Controller ENV variables are empty: %q: %s, %q: %s, cannot initialize controller", controllerNamespaceEnvVar, controllerNamespace, controllerNameEnvVar, controllerName)
	}
}
