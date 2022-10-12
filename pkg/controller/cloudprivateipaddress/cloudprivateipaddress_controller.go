package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"

	cloudnetworkv1 "github.com/openshift/api/cloudnetwork/v1"
	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	cloudnetworkscheme "github.com/openshift/client-go/cloudnetwork/clientset/versioned/scheme"
	cloudnetworkinformers "github.com/openshift/client-go/cloudnetwork/informers/externalversions/cloudnetwork/v1"
	cloudnetworklisters "github.com/openshift/client-go/cloudnetwork/listers/cloudnetwork/v1"
	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
	"github.com/openshift/cloud-network-config-controller/pkg/ipaddressmonitor"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corelisters "k8s.io/client-go/listers/core/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

var (
	// cloudPrivateIPAddressControllerAgentType is the CloudPrivateIPConfig controller's dedicated resource type
	cloudPrivateIPAddressControllerAgentType = reflect.TypeOf(&cloudnetworkv1.CloudPrivateIPConfig{})
	// cloudPrivateIPAddressControllerAgentName is the controller name for the CloudPrivateIPConfig controller
	cloudPrivateIPAddressControllerAgentName = "cloud-private-ip-config"
	// cloudPrivateIPConfigFinalizer is the name of the finalizer blocking
	// object deletion until the cloud confirms that the IP has been removed
	cloudPrivateIPConfigFinalizer = "cloudprivateipconfig.cloud.network.openshift.io/finalizer"
	// cloudResponseReasonPending indicates a pending response from the cloud API
	cloudResponseReasonPending = "CloudResponsePending"
	// cloudResponseReasonError indicates an error response from the cloud API
	cloudResponseReasonError = "CloudResponseError"
	// cloudResponseReasonSuccess indicates a successful response from the cloud API
	cloudResponseReasonSuccess = "CloudResponseSuccess"
)

// CloudPrivateIPAddressController is the controller implementation for CloudPrivateIPConfig resources
type CloudPrivateIPAddressController struct {
	controller.CloudNetworkConfigController
	cloudNetworkClient         cloudnetworkclientset.Interface
	cloudPrivateIPConfigLister cloudnetworklisters.CloudPrivateIPConfigLister
	nodesLister                corelisters.NodeLister
	// CloudProviderClient is a client interface allowing the controller
	// access to the cloud API
	cloudProviderClient cloudprovider.CloudProviderIntf
	// controllerContext is the passed-down global context. It's used and passed
	// down to all API client calls as to make sure all in-flight calls get
	// cancelled if the main context is
	ctx              context.Context
	nodeName         string
	interfaceName    string
	addressLock      sync.Mutex
	ipAddressMonitor *ipaddressmonitor.IPAddressMonitor
}

// NewCloudPrivateIPAddressController returns a new CloudPrivateIPConfig controller
func NewCloudPrivateIPAddressController(
	controllerContext context.Context,
	cloudNetworkClientset cloudnetworkclientset.Interface,
	cloudPrivateIPConfigInformer cloudnetworkinformers.CloudPrivateIPConfigInformer,
	nodeInformer coreinformers.NodeInformer,
	ipAddressMonitor *ipaddressmonitor.IPAddressMonitor,
	nodeName, interfaceName string) *controller.CloudNetworkConfigController {

	utilruntime.Must(cloudnetworkscheme.AddToScheme(scheme.Scheme))

	cloudPrivateIPAddressController := &CloudPrivateIPAddressController{
		nodesLister:                nodeInformer.Lister(),
		cloudNetworkClient:         cloudNetworkClientset,
		cloudPrivateIPConfigLister: cloudPrivateIPConfigInformer.Lister(),
		ctx:                        controllerContext,
		nodeName:                   nodeName,
		interfaceName:              interfaceName,
		ipAddressMonitor:           ipAddressMonitor,
	}

	controller := controller.NewCloudNetworkConfigController(
		[]cache.InformerSynced{cloudPrivateIPConfigInformer.Informer().HasSynced, nodeInformer.Informer().HasSynced},
		cloudPrivateIPAddressController,
		cloudPrivateIPAddressControllerAgentName,
		cloudPrivateIPAddressControllerAgentType,
	)

	cloudPrivateIPConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.Enqueue,
		UpdateFunc: func(old, new interface{}) {
			oldCloudPrivateIPConfig, _ := old.(*cloudnetworkv1.CloudPrivateIPConfig)
			newCloudPrivateIPConfig, _ := new.(*cloudnetworkv1.CloudPrivateIPConfig)
			// Enqueue our own transitions from delete -> add. On delete we will
			// unset the status node as to indicate that we finished removing
			// the IP address from its current node, that will trigger this so
			// that we process the sync adding the IP to the new node.
			if oldCloudPrivateIPConfig.Status.Node != newCloudPrivateIPConfig.Status.Node {
				controller.Enqueue(new)
			}
		},
		DeleteFunc: controller.Enqueue,
	})
	return controller
}

func (c *CloudPrivateIPAddressController) SyncHandler(key string) error {
	// var status *cloudnetworkv1.CloudPrivateIPConfigStatus

	cloudPrivateIPConfig, err := c.getCloudPrivateIPConfig(key)
	if err != nil {
		return err
	}
	// When syncing objects which have been completely deleted: we must make
	// sure to not continue processing the object - also absolutely make sure that the IP was deleted
	// from the node.
	if cloudPrivateIPConfig == nil {
		ip := cloudPrivateIPConfigNameToIP(key)
		// If we get an IsNotFound error, then make sure to remove the CloudPrivateIPConfig from
		// this node.
		if err := c.removeIPAddressMonitor(ip.String()); err != nil {
			return err
		}
		if err := c.removeIPAddress(ip.String()); err != nil {
			return err
		}
		return nil
	}

	// If the CloudPrivateIPConfig was found, then we need to add it to the target node.
	// Any other node shall not have it, so delete it if it's attached to the node.
	// Convert the key to an IP address.
	ip := cloudPrivateIPConfigNameToIP(cloudPrivateIPConfig.Name)
	if cloudPrivateIPConfig.Status.Node == c.nodeName {
		if err := c.addIPAddress(ip.String()); err != nil {
			return err
		}
		if err := c.addIPAddressMonitor(ip.String()); err != nil {
			return err
		}
	} else {
		if err := c.removeIPAddressMonitor(ip.String()); err != nil {
			return err
		}
		if err := c.removeIPAddress(ip.String()); err != nil {
			return err
		}
	}

	return nil
}

func (c *CloudPrivateIPAddressController) addIPAddressMonitor(ip string) error {
	return c.ipAddressMonitor.Add(ip)
}

func (c *CloudPrivateIPAddressController) removeIPAddressMonitor(ip string) error {
	return c.ipAddressMonitor.Remove(ip)
}

func (c *CloudPrivateIPAddressController) addIPAddress(ip string) error {
	c.addressLock.Lock()
	defer c.addressLock.Unlock()

	hasAddr, err := c.hadAddress(ip)
	if err != nil {
		return err
	}
	if hasAddr {
		return nil
	}

	klog.Infof("CloudPrivateIPAddress %q will be added to node: %q", ip, c.nodeName)
	intf, err := netlink.LinkByName(c.interfaceName)
	if err != nil {
		return err
	}
	addr, err := netlink.ParseAddr(fmt.Sprintf(ip + "/32"))
	if err != nil {
		return err
	}
	return netlink.AddrAdd(intf, addr)
}

func (c *CloudPrivateIPAddressController) removeIPAddress(ip string) error {
	c.addressLock.Lock()
	defer c.addressLock.Unlock()

	hasAddr, err := c.hadAddress(ip)
	if err != nil {
		return err
	}
	if !hasAddr {
		return nil
	}

	klog.Infof("CloudPrivateIPAddress %q will be deleted from node: %q", ip, c.nodeName)
	intf, err := netlink.LinkByName(c.interfaceName)
	if err != nil {
		return err
	}
	addr, err := netlink.ParseAddr(fmt.Sprintf(ip + "/32"))
	if err != nil {
		return err
	}
	return netlink.AddrDel(intf, addr)
}

func (c *CloudPrivateIPAddressController) hadAddress(ip string) (bool, error) {
	intf, err := netlink.LinkByName(c.interfaceName)
	if err != nil {
		return false, err
	}
	addrList, err := netlink.AddrList(intf, netlink.FAMILY_ALL)
	if err != nil {
		return false, err
	}
	for _, a := range addrList {
		if a.IP.String() == ip {
			return true, nil
		}
	}
	return false, nil
}

// updateCloudPrivateIPConfigStatus copies and updates the provided object and returns
// the new object. The return value can be useful for recursive updates
func (c *CloudPrivateIPAddressController) updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig *cloudnetworkv1.CloudPrivateIPConfig, status *cloudnetworkv1.CloudPrivateIPConfigStatus) (*cloudnetworkv1.CloudPrivateIPConfig, error) {
	updatedCloudPrivateIPConfig := &cloudnetworkv1.CloudPrivateIPConfig{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ctx, cancel := context.WithTimeout(c.ctx, controller.ClientTimeout)
		defer cancel()
		var err error
		cloudPrivateIPConfig.Status = *status
		updatedCloudPrivateIPConfig, err = c.cloudNetworkClient.CloudV1().CloudPrivateIPConfigs().UpdateStatus(ctx, cloudPrivateIPConfig, metav1.UpdateOptions{})
		return err
	})
	return updatedCloudPrivateIPConfig, err
}

type FinalizerPatch struct {
	Op    string   `json:"op"`
	Path  string   `json:"path"`
	Value []string `json:"value"`
}

// patchCloudPrivateIPConfigFinalizer patches the object and returns
// the new object. The return value can be useful for recursive updates
func (c *CloudPrivateIPAddressController) patchCloudPrivateIPConfigFinalizer(cloudPrivateIPConfig *cloudnetworkv1.CloudPrivateIPConfig) (*cloudnetworkv1.CloudPrivateIPConfig, error) {
	patchedCloudPrivateIPConfig := &cloudnetworkv1.CloudPrivateIPConfig{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		p := []FinalizerPatch{
			{
				Op:    "replace",
				Path:  "/metadata/finalizers",
				Value: cloudPrivateIPConfig.Finalizers,
			},
		}
		op, err := json.Marshal(&p)
		if err != nil {
			return fmt.Errorf("error serializing finalizer patch: %+v for CloudPrivateIPConfig: %s, err: %v", cloudPrivateIPConfig.Finalizers, cloudPrivateIPConfig.Name, err)
		}
		patchedCloudPrivateIPConfig, err = c.patchCloudPrivateIPConfig(cloudPrivateIPConfig.Name, op)
		return err
	})
	return patchedCloudPrivateIPConfig, err
}

func (c *CloudPrivateIPAddressController) patchCloudPrivateIPConfig(name string, patchData []byte) (*cloudnetworkv1.CloudPrivateIPConfig, error) {
	ctx, cancel := context.WithTimeout(c.ctx, controller.ClientTimeout)
	defer cancel()
	return c.cloudNetworkClient.CloudV1().CloudPrivateIPConfigs().Patch(ctx, name, types.JSONPatchType, patchData, metav1.PatchOptions{})
}

// getCloudPrivateIPConfig retrieves the object from the API server
func (c *CloudPrivateIPAddressController) getCloudPrivateIPConfig(name string) (*cloudnetworkv1.CloudPrivateIPConfig, error) {
	ctx, cancel := context.WithTimeout(c.ctx, controller.ClientTimeout)
	defer cancel()
	// This object will repeatedly be updated during this sync, hence we need to
	// retrieve the object from the API server as opposed to the informer cache
	// for every sync, otherwise we risk acting on an old object
	cloudPrivateIPConfig, err := c.cloudNetworkClient.CloudV1().CloudPrivateIPConfigs().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the object was deleted while we were processing the request
			// there's nothing more to do, the finalizer portion of this sync
			// should have handled the last cleanup
			klog.Infof("CloudPrivateIPConfig: %q in work queue no longer exists", name)
			return nil, nil
		}
		return nil, err
	}
	return cloudPrivateIPConfig, nil
}

// cloudPrivateIPConfigNameToIP converts the resource name to net.IP. Given a
// limitation in the Kubernetes API server (see:
// https://github.com/kubernetes/kubernetes/pull/100950)
// CloudPrivateIPConfig.metadata.name cannot represent an IPv6 address. To
// work-around this limitation it was decided that the network plugin creating
// the CR will fully expand the IPv6 address and replace all colons with dots,
// ex:

// The IPv6 address fc00:f853:ccd:e793::54 will be represented
// as: fc00.f853.0ccd.e793.0000.0000.0000.0054

// We thus need to replace every fifth character's dot with a colon.
func cloudPrivateIPConfigNameToIP(name string) net.IP {
	// handle IPv4: this is enough since it will be serialized just fine
	if ip := net.ParseIP(name); ip != nil {
		return ip
	}
	// handle IPv6
	name = strings.ReplaceAll(name, ".", ":")
	return net.ParseIP(name)
}
