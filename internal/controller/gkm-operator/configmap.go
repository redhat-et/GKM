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
	"io"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/redhat-et/GKM/pkg/utils"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=gkm.io,resources=configmaps/finalizers,verbs=update

// GKMConfigMapReconciler reconciles a GKM ConfigMap object
type GKMConfigMapReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Logger              logr.Logger
	CsiDriverYamlFile   string
	CsiDriverRegistered bool
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
		if updated := controllerutil.AddFinalizer(gkmConfigMap, utils.GKMOperatorFinalizer); updated {
			if err := r.Update(ctx, gkmConfigMap); err != nil {
				r.Logger.Error(err, "failed adding gkm-operator finalizer to GKM ConfigMap")
				return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryOperatorConfigMapFailure}, nil
			}
		}
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
	// one-shot try to create GKM's CSIDriver object if it doesn't exist, does not re-trigger reconcile.
	if !r.CsiDriverRegistered {
		if result, err := r.RegisterCSIDriver(ctx); err != nil {
			return result, err
		}
	}

	agentLogLevel := gkmConfigMap.Data[utils.ConfigMapIndexAgentLogLevel]
	agentImage := gkmConfigMap.Data[utils.ConfigMapIndexAgentImage]
	csiLogLevel := gkmConfigMap.Data[utils.ConfigMapIndexCsiLogLevel]
	csiImage := gkmConfigMap.Data[utils.ConfigMapIndexCsiImage]
	noGpu := gkmConfigMap.Data[utils.ConfigMapIndexNoGpu]

	r.Logger.Info("ConfigMap Values",
		"agentLogLevel", agentLogLevel,
		"agentImage", agentImage,
		"csiLogLevel", csiLogLevel,
		"csiImage", csiImage,
		"noGpu", noGpu,
	)

	if !gkmConfigMap.DeletionTimestamp.IsZero() {
		if r.CsiDriverRegistered {
			if result, err := r.DeregisterCSIDriver(ctx); err != nil {
				return result, err
			}
		}

		r.Logger.Info("Deleting GKM DaemonSet (ToDo) and ConfigMap")

		gkmCsiDriver := &storagev1.CSIDriver{}

		// one-shot try to delete the GKM CSIDriver object if it exists.
		if err := r.Get(
			ctx,
			types.NamespacedName{Namespace: corev1.NamespaceAll, Name: utils.CsiDriverName},
			gkmCsiDriver,
		); err == nil {
			r.Logger.Info("Deleting GKM CSIDriver object")
			if err := r.Delete(ctx, gkmCsiDriver); err != nil {
				r.Logger.Error(err, "Failed to delete GKM CSIDriver object")
				return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryOperatorConfigMapFailure}, nil
			}
		}

		controllerutil.RemoveFinalizer(gkmConfigMap, utils.GKMOperatorFinalizer)
		err := r.Update(ctx, gkmConfigMap)
		if err != nil {
			r.Logger.Error(err, "failed removing gkm-operator finalizer from GKM ConfigMap")
			return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryOperatorConfigMapFailure}, nil
		}

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

// RegisterCSIDriver calls KubeAPI Server to register the CSIDriver with Kubernetes. This
// only needs to be done once.
//
// This function only returns err if the call to KubeAPI fails.
func (r *GKMConfigMapReconciler) RegisterCSIDriver(ctx context.Context) (ctrl.Result, error) {
	gkmCsiDriver := &storagev1.CSIDriver{}
	if err := r.Get(
		ctx,
		types.NamespacedName{Namespace: corev1.NamespaceAll, Name: utils.CsiDriverName},
		gkmCsiDriver,
	); err != nil {
		if errors.IsNotFound(err) {
			gkmCsiDriver = LoadCsiDriver(r.CsiDriverYamlFile)

			r.Logger.Info("Creating GKM CSIDriver object")
			if err := r.Create(ctx, gkmCsiDriver); err != nil {
				r.Logger.Error(err, "Failed to create GKM CSIDriver object")
				return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, err
			}
		} else {
			r.Logger.Error(err, "Failed to get CSIDriver object", "Driver Name", utils.CsiDriverName)
			return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, err
		}
	}

	r.CsiDriverRegistered = true
	return ctrl.Result{Requeue: false}, nil
}

// DeregisterCSIDriver calls KubeAPI Server to deregister the CSIDriver with Kubernetes. This
// only needs to be done once.
//
// This function only returns err if the call to KubeAPI fails.
func (r *GKMConfigMapReconciler) DeregisterCSIDriver(ctx context.Context) (ctrl.Result, error) {
	gkmCsiDriver := &storagev1.CSIDriver{}

	// one-shot try to delete the GKM CSIDriver object if it exists.
	if err := r.Get(
		ctx,
		types.NamespacedName{Namespace: corev1.NamespaceAll, Name: utils.CsiDriverName},
		gkmCsiDriver,
	); err == nil {
		r.Logger.Info("Deleting GKM CSIDriver object")
		if err := r.Delete(ctx, gkmCsiDriver); err != nil {
			r.Logger.Error(err, "Failed to delete CSIDriver object")
			return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, err
		}
	}

	r.CsiDriverRegistered = false
	return ctrl.Result{Requeue: false}, nil
}

func LoadCsiDriver(path string) *storagev1.CSIDriver {
	// Load static CSIDriver yaml from disk
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, nil)

	return obj.(*storagev1.CSIDriver)
}
