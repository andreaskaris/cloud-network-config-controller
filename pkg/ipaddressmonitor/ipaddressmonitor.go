package ipaddressmonitor

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

type IPAddressMonitor struct {
	ipAddresses            sets.String
	destinationIPAddresses sets.String
	lock                   sync.Mutex
}

func NewIPAddressMonitor(destinationIPAddresses ...string) *IPAddressMonitor {
	iam := &IPAddressMonitor{
		ipAddresses:            sets.NewString(),
		destinationIPAddresses: sets.NewString(destinationIPAddresses...),
	}
	return iam
}

func (i *IPAddressMonitor) AddDestinationAddresses(destinationIPAddresses ...string) {
	i.lock.Lock()
	defer i.lock.Unlock()

	klog.Infof("Adding destination IPs %v to IPAddressMonitor", destinationIPAddresses)
	i.destinationIPAddresses.Insert(destinationIPAddresses...)
}

func (i *IPAddressMonitor) RemoveDestinationAddresses(destinationIPAddresses ...string) {
	i.lock.Lock()
	defer i.lock.Unlock()

	klog.Infof("Removing destination IPs %v to IPAddressMonitor", destinationIPAddresses)
	i.destinationIPAddresses.Delete(destinationIPAddresses...)
}

func (i *IPAddressMonitor) Add(ip string) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	klog.Infof("Adding source IP %s to IPAddressMonitor", ip)
	i.ipAddresses.Insert(ip)

	return nil
}

func (i *IPAddressMonitor) Remove(ip string) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	if i.ipAddresses.Has(ip) {
		klog.Infof("Removing IP %s from IPAddressMonitor", ip)
		i.ipAddresses.Delete(ip)
	}

	return nil
}

func (i *IPAddressMonitor) Run(ctx context.Context) {
	klog.Info("Starting IPAddressMonitor")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			klog.Info("Stopping IPAddressMonitor")
			return
		case <-ticker.C:
			i.probeAll()
		}
	}
}

func (i *IPAddressMonitor) probeAll() {
	i.lock.Lock()
	i.lock.Unlock()

	klog.Infof("Probing all destinations %v from all source IP addresses %v",
		i.destinationIPAddresses.List(), i.ipAddresses.List())
	for _, v := range i.ipAddresses.List() {
		i.probe(v)
	}
}

func (i *IPAddressMonitor) probe(ip string) bool {
	for _, d := range i.destinationIPAddresses.List() {
		klog.Infof("Probing %s from %s", d, ip)
		dialer := net.Dialer{
			LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 0},
			Timeout:   time.Second,
		}
		conn, err := dialer.Dial("tcp", net.JoinHostPort(d, fmt.Sprint(9999)))
		if err != nil {
			klog.Infof("Connecting error: %v, %s, %s", err, d, ip)
			continue
		}
		if conn != nil {
			defer conn.Close()
			klog.Infof("Opened %s from %s", net.JoinHostPort(d, fmt.Sprint(9999)), ip)
			return true
		}
	}
	return false
}
