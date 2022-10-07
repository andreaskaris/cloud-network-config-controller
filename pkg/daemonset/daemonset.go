package daemonset

import (
	applyv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	v1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/pointer"
)

func ControlPlaneDaemonSet(name string) *applyv1.DaemonSetApplyConfiguration {
	image := "centos:stream9"
	container := v1.ContainerApplyConfiguration{
		Name:    &name,
		Image:   &image,
		Command: []string{"sleep", "infinity"},
	}
	return &applyv1.DaemonSetApplyConfiguration{
		TypeMetaApplyConfiguration: applymetav1.TypeMetaApplyConfiguration{
			APIVersion: pointer.String("apps/v1"),
			Kind:       pointer.String("DaemonSet"),
		},
		ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
			Name: &name,
			//			Namespace: &namespace,
			Labels: map[string]string{
				"app": name,
			},
			OwnerReferences: []applymetav1.OwnerReferenceApplyConfiguration{},
		},
		Spec: &applyv1.DaemonSetSpecApplyConfiguration{
			Selector: &applymetav1.LabelSelectorApplyConfiguration{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: &v1.PodTemplateSpecApplyConfiguration{
				ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: &v1.PodSpecApplyConfiguration{
					Containers:  []v1.ContainerApplyConfiguration{container},
					HostNetwork: new(bool),
					Affinity:    &v1.AffinityApplyConfiguration{},
					Tolerations: []v1.TolerationApplyConfiguration{},
				},
			},
		},
	}
}

func WorkerDaemonSet(name string) *applyv1.DaemonSetApplyConfiguration {
	image := "centos:stream9"
	container := v1.ContainerApplyConfiguration{
		Name:    &name,
		Image:   &image,
		Command: []string{"sleep", "infinity"},
	}
	return &applyv1.DaemonSetApplyConfiguration{
		TypeMetaApplyConfiguration: applymetav1.TypeMetaApplyConfiguration{
			APIVersion: pointer.String("apps/v1"),
			Kind:       pointer.String("DaemonSet"),
		},
		ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
			Name: &name,
			//			Namespace: &namespace,
			Labels: map[string]string{
				"app": name,
			},
			OwnerReferences: []applymetav1.OwnerReferenceApplyConfiguration{},
		},
		Spec: &applyv1.DaemonSetSpecApplyConfiguration{
			Selector: &applymetav1.LabelSelectorApplyConfiguration{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: &v1.PodTemplateSpecApplyConfiguration{
				ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: &v1.PodSpecApplyConfiguration{
					Containers:  []v1.ContainerApplyConfiguration{container},
					HostNetwork: new(bool),
					Affinity:    &v1.AffinityApplyConfiguration{},
					Tolerations: []v1.TolerationApplyConfiguration{},
				},
			},
		},
	}
}
