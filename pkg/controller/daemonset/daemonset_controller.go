package controller

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	applyv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
	dsdef "github.com/openshift/cloud-network-config-controller/pkg/daemonset"
)

var (
	// daemonsetControllerAgentType is the DaemonSet controller's dedicated resource type
	daemonsetControllerAgentType = reflect.TypeOf(&appsv1.DaemonSet{})
	// daemonsetControllerAgentName is the controller name for the DaemonSet controller
	daemonsetControllerAgentName = "daemonset"
)

// DaemonSetController is the controller implementation for DaemonSet resources
// This controller is used to watch for daemonset rotations by the cloud-
// credentials-operator for what concerns the cloud API daemonset
type DaemonSetController struct {
	controller.CloudNetworkConfigController
	daemonsetLister appslisters.DaemonSetLister
	kubeclientset   kubernetes.Interface
	namespace       string
}

// NewDaemonSetController returns a new DaemonSet controller
func NewDaemonSetController(
	controllerContext context.Context,
	kubeclientset kubernetes.Interface,
	daemonsetInformer appsinformers.DaemonSetInformer,
	daemonsetNamespace string) *controller.CloudNetworkConfigController {

	daemonsetController := &DaemonSetController{
		daemonsetLister: daemonsetInformer.Lister(),
		kubeclientset:   kubeclientset,
		namespace:       daemonsetNamespace,
	}

	if err := daemonsetController.applyDaemonSet(dsdef.WorkerDaemonSet("worker")); err != nil {
		// TODO: fixme
		panic(err)
	}
	if err := daemonsetController.applyDaemonSet(dsdef.ControlPlaneDaemonSet("controlplane")); err != nil {
		// TODO: fixme
		panic(err)
	}

	controller := controller.NewCloudNetworkConfigController(
		[]cache.InformerSynced{daemonsetInformer.Informer().HasSynced},
		daemonsetController,
		daemonsetControllerAgentName,
		daemonsetControllerAgentType,
	)

	daemonsetFilter := func(obj interface{}) bool {
		if daemonset, ok := obj.(*appsv1.DaemonSet); ok {
			return daemonset.Namespace == daemonsetNamespace
		}
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			if daemonset, ok := tombstone.Obj.(*appsv1.DaemonSet); ok {
				return daemonset.Namespace == daemonsetNamespace
			}
		}
		return false
	}

	daemonsetInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: daemonsetFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			// Only handle updates and deletes
			//  - Add events can be avoided since the daemonset should already be created.
			//  - If the daemonset is deleted, then the DeleteFunc will take care of recreating it.
			AddFunc: controller.Enqueue,
			UpdateFunc: func(old, new interface{}) {
				oldDaemonSet, _ := old.(*appsv1.DaemonSet)
				newDaemonSet, _ := new.(*appsv1.DaemonSet)

				// Don't process resync or objects that are marked for deletion
				if oldDaemonSet.ResourceVersion == newDaemonSet.ResourceVersion ||
					!newDaemonSet.GetDeletionTimestamp().IsZero() {
					return
				}

				// Only enqueue on data change
				if !reflect.DeepEqual(oldDaemonSet.Spec, newDaemonSet.Spec) {
					controller.Enqueue(new)
				}
			},
			DeleteFunc: controller.Enqueue,
		},
	})
	return controller
}

func (d *DaemonSetController) SyncHandler(key string) error {
	_, daemonsetName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	switch daemonsetName {
	case "worker":
		if err := d.applyDaemonSet(dsdef.WorkerDaemonSet(daemonsetName)); err != nil {
			return err
		}
	case "controlplane":
		if err := d.applyDaemonSet(dsdef.ControlPlaneDaemonSet(daemonsetName)); err != nil {
			return err
		}
	}
	return nil
}

func (d *DaemonSetController) applyDaemonSet(daemonset *applyv1.DaemonSetApplyConfiguration) error {
	if _, err := d.kubeclientset.AppsV1().DaemonSets(d.namespace).Apply(
		context.TODO(), daemonset, metav1.ApplyOptions{FieldManager: "application/apply-patch"}); err != nil {
		return err
	}
	return nil
}

// getDaemonSet retrieves the object from the API server
func (d *DaemonSetController) getDaemonSet(name string) (*appsv1.DaemonSet, error) {
	// ctx, cancel := context.WithTimeout(d.ctx, controller.ClientTimeout)
	// defer cancel()
	// This object will repeatedly be updated during this sync, hence we need to
	// retrieve the object from the API server as opposed to the informer cache
	// for every sync, otherwise we risk acting on an old object
	daemonset, err := d.daemonsetLister.DaemonSets(d.namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the object was deleted while we were processing the request
			// there's nothing more to do, the finalizer portion of this sync
			// should have handled the last cleanup
			klog.Infof("DaemonSet: %q in work queue no longer exists", name)
			return nil, nil
		}
		return nil, err
	}
	return daemonset, nil
}
