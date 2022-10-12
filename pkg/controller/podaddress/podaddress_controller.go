package controller

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
	"github.com/openshift/cloud-network-config-controller/pkg/ipaddressmonitor"
)

var (
	// podControllerAgentType is the Pod controller's dedicated resource type
	podControllerAgentType = reflect.TypeOf(&corev1.Pod{})
	// podControllerAgentName is the controller name for the Pod controller
	podControllerAgentName = "pod"
)

// PodAddressController is the controller implementation for Pod resources
// This controller is used to watch for pod rotations by the cloud-
// credentials-operator for what concerns the cloud API pod
type PodAddressController struct {
	controller.CloudNetworkConfigController
	podLister        corelisters.PodLister
	kubeclientset    kubernetes.Interface
	namespace        string
	ipAddressMonitor *ipaddressmonitor.IPAddressMonitor
}

// NewPodAddressController returns a new Pod controller
func NewPodAddressController(
	controllerContext context.Context,
	kubeclientset kubernetes.Interface,
	podInformer coreinformers.PodInformer,
	ipAddressMonitor *ipaddressmonitor.IPAddressMonitor,
	podNamespace string) *controller.CloudNetworkConfigController {

	podController := &PodAddressController{
		podLister:        podInformer.Lister(),
		kubeclientset:    kubeclientset,
		namespace:        podNamespace,
		ipAddressMonitor: ipAddressMonitor,
	}

	controller := controller.NewCloudNetworkConfigController(
		[]cache.InformerSynced{podInformer.Informer().HasSynced},
		podController,
		podControllerAgentName,
		podControllerAgentType,
	)

	podFilter := func(obj interface{}) bool {
		if pod, ok := obj.(*corev1.Pod); ok {
			return pod.Namespace == podNamespace
		}
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			if pod, ok := tombstone.Obj.(*corev1.Pod); ok {
				return pod.Namespace == podNamespace
			}
		}
		return false
	}

	podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: podFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			// Only handle updates and deletes
			//  - Add events can be avoided since the pod should already be created.
			//  - If the pod is deleted, then the DeleteFunc will take care of recreating it.
			AddFunc: controller.Enqueue,
			UpdateFunc: func(old, new interface{}) {
				oldPod, _ := old.(*corev1.Pod)
				newPod, _ := new.(*corev1.Pod)

				// Don't process resync or objects that are marked for deletion
				if oldPod.ResourceVersion == newPod.ResourceVersion ||
					!newPod.GetDeletionTimestamp().IsZero() {
					return
				}

				// Only enqueue on data change
				if !reflect.DeepEqual(oldPod.Spec, newPod.Spec) {
					controller.Enqueue(new)
				}
			},
			DeleteFunc: controller.Enqueue,
		},
	})
	return controller
}

func (d *PodAddressController) SyncHandler(key string) error {
	_, podName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// TODO: add remove logic, improve add logic.
	pod, err := d.getPod(podName)
	if err != nil {
		return err
	}
	for l, v := range pod.Labels {
		if l == "app" && v == "controlplane" {
			d.ipAddressMonitor.AddDestinationAddresses(pod.Status.PodIP)
		}
	}

	return nil
}

// getPod retrieves the object from the API server
func (d *PodAddressController) getPod(name string) (*corev1.Pod, error) {
	// ctx, cancel := context.WithTimeout(d.ctx, controller.ClientTimeout)
	// defer cancel()
	// This object will repeatedly be updated during this sync, hence we need to
	// retrieve the object from the API server as opposed to the informer cache
	// for every sync, otherwise we risk acting on an old object
	pod, err := d.podLister.Pods(d.namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the object was deleted while we were processing the request
			// there's nothing more to do, the finalizer portion of this sync
			// should have handled the last cleanup
			klog.Infof("Pod: %q in work queue no longer exists", name)
			return nil, nil
		}
		return nil, err
	}
	return pod, nil
}
