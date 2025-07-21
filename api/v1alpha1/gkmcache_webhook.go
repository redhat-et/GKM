/*
Copyright 2025.

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

package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var gkmcachelog = logf.Log.WithName("gkmcache-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *GKMCache) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-gkm-io-v1alpha1-gkmcache,mutating=true,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=gkmcaches,verbs=create;update,versions=v1alpha1,name=mgkmcache.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &GKMCache{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *GKMCache) Default() {
	gkmcachelog.Info("default", "name", r.Name)

	// Hardcode example auto-populate ResolvedDigest if empty TODO update this for real
	if r.Status.ResolvedDigest == "" {
		r.Status.ResolvedDigest = "sha256:defaulted-digest-demo"
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-gkm-io-v1alpha1-gkmcache,mutating=false,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=gkmcaches,verbs=create;update,versions=v1alpha1,name=vgkmcache.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &GKMCache{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *GKMCache) ValidateCreate() (admission.Warnings, error) {
	gkmcachelog.Info("validate create", "name", r.Name)

	var warnings admission.Warnings

	// Example: Reject if Image field is empty
	if r.Spec.Image == "" {
		warnings = append(warnings, "Image not set")
		return warnings, fmt.Errorf("spec.image must be set")
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *GKMCache) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	gkmcachelog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *GKMCache) ValidateDelete() (admission.Warnings, error) {
	gkmcachelog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}
