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

package v1beta3

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var emqxenterpriselog = logf.Log.WithName("emqxenterprise-resource")

func (r *EmqxEnterprise) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-apps-emqx-io-v1beta3-emqxenterprise,mutating=true,failurePolicy=fail,sideEffects=None,groups=apps.emqx.io,resources=emqxenterprises,verbs=create;update,versions=v1beta3,name=mutating.enterprise.emqx.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &EmqxEnterprise{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *EmqxEnterprise) Default() {
	emqxenterpriselog.Info("default", "name", r.Name)

	if r.Labels == nil {
		r.Labels = make(map[string]string)
	}
	r.Labels["apps.emqx.io/managed-by"] = "emqx-operator"
	r.Labels["apps.emqx.io/instance"] = r.GetName()

	if r.Spec.EmqxTemplate.ACL == nil {
		r.Spec.EmqxTemplate.ACL = []string{
			`{allow, {user, "dashboard"}, subscribe, ["$SYS/#"]}.`,
			`{allow, {ipaddr, "127.0.0.1"}, pubsub, ["$SYS/#", "#"]}.`,
			`{deny, all, subscribe, ["$SYS/#", {eq, "#"}]}.`,
			`{allow, all}.`,
		}
	}

	modules := &EmqxEnterpriseModuleList{
		Items: r.Spec.EmqxTemplate.Modules,
	}
	modules.Default()
	r.Spec.EmqxTemplate.Modules = modules.Items

	if r.Spec.EmqxTemplate.EmqxConfig == nil {
		r.Spec.EmqxTemplate.EmqxConfig = make(EmqxConfig)
	}
	r.Spec.EmqxTemplate.EmqxConfig.Default(r)
	r.Spec.EmqxTemplate.ServiceTemplate.Default(r)

	if r.Spec.EmqxTemplate.ReadinessProbe == nil {
		r.Spec.EmqxTemplate.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/status",
					Port: intstr.FromInt(8081),
				},
			},
			InitialDelaySeconds: int32(10),
			PeriodSeconds:       int32(5),
			FailureThreshold:    int32(30),
		}
	}

	if r.Spec.EmqxTemplate.SecurityContext == nil {
		emqxUserGroup := int64(1000)
		fsGroupChangeAlways := corev1.FSGroupChangeAlways

		r.Spec.EmqxTemplate.SecurityContext = &corev1.PodSecurityContext{
			RunAsUser:           &emqxUserGroup,
			RunAsGroup:          &emqxUserGroup,
			FSGroup:             &emqxUserGroup,
			FSGroupChangePolicy: &fsGroupChangeAlways,
			SupplementalGroups:  []int64{emqxUserGroup},
		}
	}
}

//+kubebuilder:webhook:path=/validate-apps-emqx-io-v1beta3-emqxenterprise,mutating=false,failurePolicy=fail,sideEffects=None,groups=apps.emqx.io,resources=emqxenterprises,verbs=create;update,versions=v1beta3,name=validator.enterprise.emqx.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &EmqxEnterprise{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *EmqxEnterprise) ValidateCreate() error {
	emqxenterpriselog.Info("validate create", "name", r.Name)

	if err := validateTag(r.Spec.EmqxTemplate.Image); err != nil {
		emqxenterpriselog.Error(err, "validate create failed")
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *EmqxEnterprise) ValidateUpdate(old runtime.Object) error {
	emqxenterpriselog.Info("validate update", "name", r.Name)

	if err := validateTag(r.Spec.EmqxTemplate.Image); err != nil {
		emqxenterpriselog.Error(err, "validate update failed")
		return err
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *EmqxEnterprise) ValidateDelete() error {
	emqxenterpriselog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
