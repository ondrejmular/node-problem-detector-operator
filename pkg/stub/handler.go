package stub

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/openshift/node-problem-detector-operator/pkg/apis/node-problem-detector/v1alpha1"
	"github.com/openshift/node-problem-detector-operator/pkg/assets"

	ossecurityv1 "github.com/openshift/api/security/v1"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	defaultPluginPermissions int32 = 0766
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct {
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.NodeProblemDetector:
		cm := newNPDConfig(o)
		err := sdk.Create(cm)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create node-problem-detector configmap : %v", err)
		}

		cmPlugins := newNPDPlugins(o)
		err = sdk.Create(cmPlugins)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create node-problem-detector-plugins configmap : %v", err)
		}

		sa := newServiceAccount(o)
		err = sdk.Create(sa)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create node-problem-detector serviceaccount : %v", err)
		}

		crb := newClusterRoleBinding(o)
		err = sdk.Create(crb)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create clusterrolebinding for node-problem-detector serviceaccount : %v", err)
		}

		scc := &ossecurityv1.SecurityContextConstraints{
			TypeMeta: metav1.TypeMeta{
				Kind:       "SecurityContextConstraints",
				APIVersion: "security.openshift.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "privileged",
			},
		}
		err = sdk.Get(scc)
		if err != nil {
			return fmt.Errorf("failed to get privileged securitycontextconstraints : %v", err)
		}

		sccUser := "system:serviceaccount:" + o.Namespace + ":node-problem-detector"
		sccUserFound := false
		for _, user := range scc.Users {
			if user == sccUser {
				sccUserFound = true
				break
			}
		}
		if !sccUserFound {
			scc.Users = append(scc.Users, sccUser)
			sdk.Update(scc)
			if err != nil {
				return fmt.Errorf("failed to add %v to privileged scc : %v", sccUser, err)
			}
		}

		ds := newNPDDS(o)
		err = sdk.Create(ds)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create node-problem-detector daemonset : %v", err)
		}

		err = sdk.Get(ds)
		if err != nil {
			return fmt.Errorf("failed to get node-problem-detector daemonset : %v", err)
		}
		// ensure that the params match
		changed := false
		for _, c := range ds.Spec.Template.Spec.Containers {
			if c.Name != "node-problem-detector" {
				continue
			}
			ipp := corev1.PullPolicy(o.Spec.ImagePullPolicy)
			if c.ImagePullPolicy != ipp {
				c.ImagePullPolicy = ipp
				changed = true
			}

			if changed {
				sdk.Update(ds)
				if err != nil {
					return fmt.Errorf("failed to update node-problem-detector daemonset : %v", err)
				}
				break
			}
		}
	}
	return nil
}

func newServiceAccount(cr *v1alpha1.NodeProblemDetector) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-problem-detector",
			Namespace: cr.Namespace,
		},
	}
}

func newClusterRoleBinding(cr *v1alpha1.NodeProblemDetector) *rbacv1.ClusterRoleBinding {
	h := fnv.New32a()
	h.Write([]byte(cr.Namespace))
	hval := strconv.FormatUint(uint64(h.Sum32()), 10)

	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "system:node-problem-detector-" + hval,
			Namespace: cr.Namespace,
		},
		RoleRef: rbacv1.RoleRef{
			Name: "system:node-problem-detector",
			Kind: "ClusterRole",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "node-problem-detector",
				Namespace: cr.Namespace,
			},
		},
	}
}

// newNPDDS creates the Node Problem Detector daemonset
func newNPDDS(cr *v1alpha1.NodeProblemDetector) *appsv1.DaemonSet {
	labels := map[string]string{
		"app": "node-problem-detector",
	}
	terminationGracePeriodSeconds := int64(30)
	privileged := true

	return &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cr, schema.GroupVersionKind{
					Group:   v1alpha1.SchemeGroupVersion.Group,
					Version: v1alpha1.SchemeGroupVersion.Version,
					Kind:    "NodeProblemDetector",
				}),
			},
			Labels: labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							// added a custom plugin monitor config for file existence monitor
							Command: []string{"/node-problem-detector", "--logtostderr", "--system-log-monitors=/etc/npd/kernel-monitor.json,/etc/npd/docker-monitor.json", "--custom-plugin-monitors=/etc/npd/kubelet-monitor.json,/etc/npd/file-existence-monitor.json"},
							Env: []corev1.EnvVar{
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
							},
							// Don't have access to openshift image therefore using an upstream image
							// Image:           "openshift/ose-node-problem-detector:v4.0",
							Image:           "k8s.gcr.io/node-problem-detector:v0.8.1",
							ImagePullPolicy: corev1.PullPolicy(cr.Spec.ImagePullPolicy),
							Name:            "node-problem-detector",
							Resources:       corev1.ResourceRequirements{},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							TerminationMessagePath:   "/dev/termination-log",
							TerminationMessagePolicy: "File",
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/host/log",
									Name:      "log",
									ReadOnly:  true,
								},
								{
									MountPath: "/etc/localtime",
									Name:      "localtime",
									ReadOnly:  true,
								},
								{
									MountPath: "/etc/npd",
									Name:      "config",
								},
								{
									MountPath: "/etc/npd-plugins",
									Name:      "plugins",
								},
								// Mounting host's root FS for being able to monitor files on the host
								{
									MountPath: "/rootfs",
									Name:      "rootfs",
								},
							},
						},
					},
					HostNetwork:                   true,
					RestartPolicy:                 "Always",
					SecurityContext:               &corev1.PodSecurityContext{},
					ServiceAccountName:            "node-problem-detector",
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					Volumes: []corev1.Volume{
						{
							Name: "log",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/log",
								},
							},
						},
						{
							Name: "localtime",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/localtime",
								},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "node-problem-detector",
									},
								},
							},
						},
						{
							Name: "plugins",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									DefaultMode: &defaultPluginPermissions,
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "node-problem-detector-plugins",
									},
								},
							},
						},
						{
							Name: "rootfs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/",
								},
							},
						},
					},
				},
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: "RollingUpdate",
			},
		},
	}
}

func newNPDConfig(cr *v1alpha1.NodeProblemDetector) *corev1.ConfigMap {
	data := map[string]string{
		"docker-monitor.json":         assets.ReadAsset(assets.ConfigDockerMonitor),
		"kernel-monitor.json":         assets.ReadAsset(assets.ConfigKernelMonitor),
		"kubelet-monitor.json":        assets.ReadAsset(assets.ConfigKubeletMonitor),
		"file-existence-monitor.json": assets.ReadAsset(assets.ConfigFileExistenceMonitor),
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-problem-detector",
			Namespace: cr.Namespace,
		},
		Data: data,
	}
}

func newNPDPlugins(cr *v1alpha1.NodeProblemDetector) *corev1.ConfigMap {
	data := map[string]string{
		"kubelet-health.sh":       assets.ReadAsset(assets.PluginKubeletHealth),
		"check-file-existence.sh": assets.ReadAsset(assets.PluginCheckFileExistence),
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-problem-detector-plugins",
			Namespace: cr.Namespace,
		},
		Data: data,
	}
}
