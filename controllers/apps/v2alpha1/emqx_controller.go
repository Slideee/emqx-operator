/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apps

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv2alpha1 "github.com/emqx/emqx-operator/apis/apps/v2alpha1"
	appsv1 "k8s.io/api/apps/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
)

// EMQXReconciler reconciles a EMQX object
type EMQXReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps.emqx.io,resources=emqxes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.emqx.io,resources=emqxes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.emqx.io,resources=emqxes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EMQX object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *EMQXReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	instance := &appsv2alpha1.EMQX{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if k8sErrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	headlessSvc, coreSvc, repliantSvc := generateService(*instance)
	deploy := generateDeployment(*instance)
	sts := generateStatefulSet(*instance)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EMQXReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv2alpha1.EMQX{}).
		Complete(r)
}

func generateService(instance appsv2alpha1.EMQX) (headlessSvc, coreSvc, replicantSvc corev1.Service) {
	headlessSvc = corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-headless", instance.Name),
			Namespace: instance.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			ClusterIP:                corev1.ClusterIPNone,
			SessionAffinity:          corev1.ServiceAffinityNone,
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       "ekka",
					Port:       4370,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(4370),
				},
			},
			Selector: instance.Spec.CoreTemplate.Labels,
		},
	}
	coreSvc = corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-core", instance.Name),
			Namespace:   instance.Namespace,
			Labels:      instance.Spec.CoreTemplate.Spec.ServiceTemplate.Labels,
			Annotations: instance.Spec.CoreTemplate.Spec.ServiceTemplate.Annotations,
		},
		Spec: instance.Spec.CoreTemplate.Spec.ServiceTemplate.Spec,
	}

	replicantSvc = corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-replicant", instance.Name),
			Namespace:   instance.Namespace,
			Labels:      instance.Spec.ReplicantTemplate.Spec.ServiceTemplate.Labels,
			Annotations: instance.Spec.ReplicantTemplate.Spec.ServiceTemplate.Annotations,
		},
		Spec: instance.Spec.ReplicantTemplate.Spec.ServiceTemplate.Spec,
	}

	return
}

func generateStatefulSet(instance appsv2alpha1.EMQX) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-core", instance.Name),
			Namespace: instance.GetNamespace(),
			Labels:    instance.Spec.CoreTemplate.Labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: fmt.Sprintf("%s-core", instance.Name),
			Replicas:    instance.Spec.CoreTemplate.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: instance.Spec.CoreTemplate.Labels,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      instance.Spec.CoreTemplate.Labels,
					Annotations: instance.Spec.CoreTemplate.Annotations,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: instance.Spec.ImagePullSecrets,
					SecurityContext:  instance.Spec.SecurityContext,
					Affinity:         instance.Spec.CoreTemplate.Spec.Affinity,
					Tolerations:      instance.Spec.CoreTemplate.Spec.ToleRations,
					NodeName:         instance.Spec.CoreTemplate.Spec.NodeName,
					NodeSelector:     instance.Spec.CoreTemplate.Spec.NodeSelector,
					InitContainers:   instance.Spec.CoreTemplate.Spec.InitContainers,
					Containers: append(instance.Spec.CoreTemplate.Spec.ExtraContainers, corev1.Container{
						Name:            "emqx",
						Image:           instance.Spec.CoreTemplate.Spec.Image,
						ImagePullPolicy: corev1.PullPolicy(instance.Spec.CoreTemplate.Spec.ImagePullPolicy),
						Env: []corev1.EnvVar{
							{
								Name:  "EMQX_NODE__DB_ROLE",
								Value: "core",
							},
							{
								Name:  "EMQX_CLUSTER__DISCOVERY_STRATEGY",
								Value: "dns",
							},
							{
								Name:  "EMQX_CLUSTER__DNS__NAME",
								Value: "emqx-headless.default.svc.cluster.local",
							},
							{
								Name:  "EMQX_CLUSTER__DNS__RECORD_TYPE",
								Value: "srv",
							},
						},
						Args:           instance.Spec.CoreTemplate.Spec.Args,
						Resources:      instance.Spec.CoreTemplate.Spec.Resources,
						ReadinessProbe: instance.Spec.CoreTemplate.Spec.ReadinessProbe,
						LivenessProbe:  instance.Spec.CoreTemplate.Spec.LivenessProbe,
						StartupProbe:   instance.Spec.CoreTemplate.Spec.StartupProbe,
					}),
				},
			},
		},
	}
	return sts
}

func generateDeployment(instance appsv2alpha1.EMQX) *appsv1.Deployment {
	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-replicant", instance.Name),
			Namespace: instance.GetNamespace(),
			Labels:    instance.Spec.ReplicantTemplate.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: instance.Spec.ReplicantTemplate.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: instance.Spec.ReplicantTemplate.Labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      instance.Spec.ReplicantTemplate.Labels,
					Annotations: instance.Spec.ReplicantTemplate.Annotations,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: instance.Spec.ImagePullSecrets,
					SecurityContext:  instance.Spec.SecurityContext,
					Affinity:         instance.Spec.ReplicantTemplate.Spec.Affinity,
					Tolerations:      instance.Spec.ReplicantTemplate.Spec.ToleRations,
					NodeName:         instance.Spec.ReplicantTemplate.Spec.NodeName,
					NodeSelector:     instance.Spec.ReplicantTemplate.Spec.NodeSelector,
					InitContainers:   instance.Spec.ReplicantTemplate.Spec.InitContainers,
					Containers: append(instance.Spec.ReplicantTemplate.Spec.ExtraContainers, corev1.Container{
						Name:            "emqx",
						Image:           instance.Spec.CoreTemplate.Spec.Image,
						ImagePullPolicy: corev1.PullPolicy(instance.Spec.CoreTemplate.Spec.ImagePullPolicy),
						Env: []corev1.EnvVar{
							{
								Name:  "EMQX_NODE__DB_ROLE",
								Value: "replicant",
							},
							{
								Name: "EMQX_HOST",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "status.podIP",
									},
								},
							},
							{
								Name:  "EMQX_CLUSTER__DISCOVERY_STRATEGY",
								Value: "static",
							},
							{
								Name:  "EMQX_CLUSTER__STATIC__SEEDS",
								Value: fmt.Sprintf("[%s]", "emqx@emqx-core-0.emqx-headless.default.svc.cluster.local,emqx@emqx-core-1.emqx-headless.default.svc.cluster.local,emqx@emqx-core-2.emqx-headless.default.svc.cluster.local"),
							},
						},
						Args:           instance.Spec.CoreTemplate.Spec.Args,
						Resources:      instance.Spec.CoreTemplate.Spec.Resources,
						ReadinessProbe: instance.Spec.CoreTemplate.Spec.ReadinessProbe,
						LivenessProbe:  instance.Spec.CoreTemplate.Spec.LivenessProbe,
						StartupProbe:   instance.Spec.CoreTemplate.Spec.StartupProbe,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "emqx-data",
								MountPath: "/opt/emqx/data",
							},
						},
					}),
					Volumes: []corev1.Volume{
						{
							Name: "emqx-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
	return deploy
}