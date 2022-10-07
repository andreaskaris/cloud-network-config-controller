package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	cloudnetworkinformers "github.com/openshift/client-go/cloudnetwork/informers/externalversions"
	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	cloudprivateipaddresscontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/cloudprivateipaddress"
	signals "github.com/openshift/cloud-network-config-controller/pkg/signals"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

const (
	// The name of the configmap used for leader election
	resourceLockNamePrefix    = "cloud-network-config-address-controller"
	controllerNameEnvVar      = "CONTROLLER_NAME"
	controllerNamespaceEnvVar = "CONTROLLER_NAMESPACE"
	controllerImageEnvVar     = "CONTROLLER_IMAGE"
	controllerNodeNameEnvVar  = "NODE_NAME"
	controllerInterfaceEnvVar = "CONTROLLER_INTERFACE"
)

var (
	kubeConfig          string
	platformCfg         cloudprovider.CloudProviderConfig
	resourceLockName    string
	controllerName      string
	controllerNamespace string
	controllerImage     string
	controllerNodeName  string
	controllerInterface string
)

func main() {
	// set up wait group used for spawning all our individual controllers
	// on the bottom of this function
	wg := &sync.WaitGroup{}

	// set up a global context used for shutting down the leader election and
	// subsequently all controllers.
	ctx, cancelFunc := context.WithCancel(context.Background())

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler(cancelFunc)

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
				cloudNetworkClient, err := cloudnetworkclientset.NewForConfig(cfg)
				if err != nil {
					klog.Exitf("Error building cloudnetwork clientset: %s", err.Error())
				}

				kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, time.Minute*2, kubeinformers.WithNamespace(controllerNamespace))
				cloudNetworkInformerFactory := cloudnetworkinformers.NewSharedInformerFactory(cloudNetworkClient, time.Minute*2)

				cloudPrivateIPAddressController := cloudprivateipaddresscontroller.NewCloudPrivateIPAddressController(
					ctx,
					cloudNetworkClient,
					cloudNetworkInformerFactory.Cloud().V1().CloudPrivateIPConfigs(),
					kubeInformerFactory.Core().V1().Nodes(),
					controllerNodeName,
					controllerInterface,
				)
				cloudNetworkInformerFactory.Start(stopCh)
				kubeInformerFactory.Start(stopCh)

				wg.Add(1)
				go func() {
					defer wg.Done()
					if err = cloudPrivateIPAddressController.Run(stopCh); err != nil {
						klog.Exitf("Error running CloudPrivateIPAddress controller: %s", err.Error())
					}
				}()
			},
			// There are two cases to consider for shutting down our controller.
			//  1. Cloud credential or configmap rotation - which our secret controller
			//     and configmap controller watch for and cancel the global context.
			//     That will trigger an end to the leader election loop and call
			//     OnStoppedLeading which will send a SIGTERM and shut down all controllers.
			//  2. Leader election rotation - which will send a SIGTERM and
			//     shut down all controllers.
			OnStoppedLeading: func() {
				klog.Info("Stopped leading, sending SIGTERM and shutting down controller")
				_ = signals.ShutDown()
				// This only needs to wait if we were ever leader
				wg.Wait()
			},
		},
	})
	klog.Info("Finished executing controlled shutdown")
}

func init() {
	klog.InitFlags(nil)

	// These are populated by the downward API
	controllerNamespace = os.Getenv(controllerNamespaceEnvVar)
	controllerName = os.Getenv(controllerNameEnvVar)
	controllerImage = os.Getenv(controllerImageEnvVar)
	controllerNodeName = os.Getenv(controllerNodeNameEnvVar)
	controllerInterface = os.Getenv(controllerInterfaceEnvVar)
	resourceLockName = strings.Join([]string{resourceLockNamePrefix, controllerNodeName}, "-")
	if controllerNamespace == "" || controllerName == "" {
		klog.Exit(fmt.Sprintf("Controller ENV variables are empty: %q: %s, %q: %s, cannot initialize controller", controllerNamespaceEnvVar, controllerNamespace, controllerNameEnvVar, controllerName))
	}
}
