package daemonset

import (
	corev1 "k8s.io/api/core/v1"
	applyv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/pointer"
)

func ControlPlaneDaemonSet(name, namespace, image string) *applyv1.DaemonSetApplyConfiguration {
	container := applycorev1.ContainerApplyConfiguration{
		Name:    &name,
		Image:   &image,
		Command: []string{"/usr/bin/cloud-network-config-probe-server"},
		Env: []applycorev1.EnvVarApplyConfiguration{
			{
				Name: pointer.String("NODE_NAME"),
				ValueFrom: &applycorev1.EnvVarSourceApplyConfiguration{
					FieldRef: &applycorev1.ObjectFieldSelectorApplyConfiguration{
						FieldPath: pointer.String("spec.nodeName"),
					},
				},
			},
			{
				Name:  pointer.String("CONTROLLER_NAME"),
				Value: pointer.String("cloud-network-config-probe-server"),
			},
			{
				Name:  pointer.String("CONTROLLER_INTERFACE"),
				Value: pointer.String("br-ex"),
			},
			{
				Name:  pointer.String("CONTROLLER_NAMESPACE"),
				Value: &namespace,
			},
		},
	}
	toleration := corev1.TolerationOpExists
	nodeSelector := corev1.NodeSelectorOpExists
	return &applyv1.DaemonSetApplyConfiguration{
		TypeMetaApplyConfiguration: applymetav1.TypeMetaApplyConfiguration{
			APIVersion: pointer.String("apps/v1"),
			Kind:       pointer.String("DaemonSet"),
		},
		ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
			Name: &name,
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
			Template: &applycorev1.PodTemplateSpecApplyConfiguration{
				ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: &applycorev1.PodSpecApplyConfiguration{
					Containers:         []applycorev1.ContainerApplyConfiguration{container},
					ServiceAccountName: pointer.String("cloud-network-config-controller"),
					HostNetwork:        pointer.Bool(true),
					Affinity: &applycorev1.AffinityApplyConfiguration{
						NodeAffinity: &applycorev1.NodeAffinityApplyConfiguration{
							RequiredDuringSchedulingIgnoredDuringExecution: &applycorev1.NodeSelectorApplyConfiguration{
								NodeSelectorTerms: []applycorev1.NodeSelectorTermApplyConfiguration{
									{
										MatchExpressions: []applycorev1.NodeSelectorRequirementApplyConfiguration{
											{
												Key:      pointer.String("node-role.kubernetes.io/control-plane"),
												Operator: &nodeSelector,
											},
										},
									},
									{
										MatchExpressions: []applycorev1.NodeSelectorRequirementApplyConfiguration{
											{
												Key:      pointer.String("node-role.kubernetes.io/master"),
												Operator: &nodeSelector,
											},
										},
									},
								},
							},
						},
					},
					Tolerations: []applycorev1.TolerationApplyConfiguration{
						{
							Operator: &toleration,
						},
					},
				},
			},
		},
	}
}

func WorkerDaemonSet(name, namespace, image string) *applyv1.DaemonSetApplyConfiguration {
	container := applycorev1.ContainerApplyConfiguration{
		Name:    &name,
		Image:   &image,
		Command: []string{"/usr/bin/cloud-network-config-address-controller"},
		SecurityContext: &applycorev1.SecurityContextApplyConfiguration{
			Capabilities: &applycorev1.CapabilitiesApplyConfiguration{
				Add: []corev1.Capability{"NET_ADMIN"},
			},
		},
		Env: []applycorev1.EnvVarApplyConfiguration{
			{
				Name: pointer.String("NODE_NAME"),
				ValueFrom: &applycorev1.EnvVarSourceApplyConfiguration{
					FieldRef: &applycorev1.ObjectFieldSelectorApplyConfiguration{
						FieldPath: pointer.String("spec.nodeName"),
					},
				},
			},
			{
				Name:  pointer.String("CONTROLLER_NAME"),
				Value: pointer.String("cloud-network-config-address-controller"),
			},
			{
				Name:  pointer.String("CONTROLLER_INTERFACE"),
				Value: pointer.String("br-ex"),
			},
			{
				Name:  pointer.String("CONTROLLER_NAMESPACE"),
				Value: &namespace,
			},
		},
	}
	return &applyv1.DaemonSetApplyConfiguration{
		TypeMetaApplyConfiguration: applymetav1.TypeMetaApplyConfiguration{
			APIVersion: pointer.String("apps/v1"),
			Kind:       pointer.String("DaemonSet"),
		},
		ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
			Name: &name,
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
			Template: &applycorev1.PodTemplateSpecApplyConfiguration{
				ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: &applycorev1.PodSpecApplyConfiguration{
					Containers:         []applycorev1.ContainerApplyConfiguration{container},
					HostNetwork:        pointer.Bool(true),
					Affinity:           &applycorev1.AffinityApplyConfiguration{},
					Tolerations:        []applycorev1.TolerationApplyConfiguration{},
					ServiceAccountName: pointer.String("cloud-network-config-controller"),
				},
			},
		},
	}
}
