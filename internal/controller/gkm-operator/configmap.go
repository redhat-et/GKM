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

package gkmOperator

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/redhat-et/GKM/pkg/utils"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=gkm.io,resources=configmaps/finalizers,verbs=update

// GKMConfigMapReconciler reconciles a GKM ConfigMap object
type GKMConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GKMCache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *GKMConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("configMap")
	r.Logger.Info("ConfigMap Reconcile ENTER")

	gkmConfigMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, gkmConfigMap); err != nil {
		if !errors.IsNotFound(err) {
			r.Logger.Error(err, "failed getting GKM ConfigMap", "ReconcileObject", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	} else {
		return r.ReconcileGKMConfigMap(ctx, req, gkmConfigMap)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GKMConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch the gkm-daemon Config Map to configure the GKM deployment across the whole cluster
		For(&corev1.ConfigMap{},
			builder.WithPredicates(gkmConfigPredicate())).
		Complete(r)
}

func (r *GKMConfigMapReconciler) ReconcileGKMConfigMap(ctx context.Context, req ctrl.Request, gkmConfigMap *corev1.ConfigMap,
) (ctrl.Result, error) {
	agentLogLevel := gkmConfigMap.Data[utils.ConfigMapIndexAgentLogLevel]
	agentImage := gkmConfigMap.Data[utils.ConfigMapIndexAgentImage]
	extractLogLevel := gkmConfigMap.Data[utils.ConfigMapIndexExtractLogLevel]
	extractImage := gkmConfigMap.Data[utils.ConfigMapIndexExtractImage]
	noGpu := gkmConfigMap.Data[utils.ConfigMapIndexNoGpu]
	kindCluster := gkmConfigMap.Data[utils.ConfigMapIndexKindCluster]

	r.Logger.Info("ConfigMap Values",
		"agentImage", agentImage,
		"agentLogLevel", agentLogLevel,
		"extractImage", extractImage,
		"extractLogLevel", extractLogLevel,
		"noGpu", noGpu,
		"kindCluster", kindCluster,
	)

	if !gkmConfigMap.DeletionTimestamp.IsZero() {
		r.Logger.Info("Deleting GKM DaemonSet (ToDo) and ConfigMap")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// Only reconcile on GKM ConfigMap events.
func gkmConfigPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == utils.GKMConfigName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == utils.GKMConfigName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == utils.GKMConfigName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == utils.GKMConfigName
		},
	}
}
