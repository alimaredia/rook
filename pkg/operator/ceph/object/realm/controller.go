/*
Copyright 2018 The Rook Authors. All rights reserved.

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

// Package objectrealm to manage a rook object store realm.
package objectrealm

import (
	"context"
	"fmt"
	"reflect"

	opcontroller "github.com/rook/rook/pkg/operator/ceph/controller"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/operator/k8sutil"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	controllerName = "ceph-object-store-realm-controller"
)

var logger = capnslog.NewPackageLogger("github.com/rook/rook", controllerName)

var cephObjectStoreRealmKind = reflect.TypeOf(cephv1.CephObjectStoreRealm{}).Name()

// Sets the type meta for the controller main object
var controllerTypeMeta = metav1.TypeMeta{
	Kind:       cephObjectStoreRealmKind,
	APIVersion: fmt.Sprintf("%s/%s", cephv1.CustomResourceGroup, cephv1.Version),
}

// ReconcileObjectStoreRealm reconciles a ObjectStoreRealm object
type ReconcileObjectStoreRealm struct {
	client  client.Client
	scheme  *runtime.Scheme
	context *clusterd.Context
}

// Add creates a new CephObjectStoreRealm Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, context *clusterd.Context) error {
	return add(mgr, newReconciler(mgr, context))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, context *clusterd.Context) reconcile.Reconciler {
	// Add the cephv1 scheme to the manager scheme so that the controller knows about it
	mgrScheme := mgr.GetScheme()
	cephv1.AddToScheme(mgr.GetScheme())

	return &ReconcileObjectStoreRealm{
		client:  mgr.GetClient(),
		scheme:  mgrScheme,
		context: context,
	}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes on the CephObjectStoreRealm CRD object
	err = c.Watch(&source.Kind{Type: &cephv1.CephObjectStoreRealm{TypeMeta: controllerTypeMeta}}, &handler.EnqueueRequestForObject{}, opcontroller.WatchControllerPredicate())
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a CephObjectStoreRealm object and makes changes based on the state read
// and what is in the CephObjectStoreRealm.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileObjectStoreRealm) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// workaround because the rook logging mechanism is not compatible with the controller-runtime loggin interface
	reconcileResponse, err := r.reconcile(request)
	if err != nil {
		logger.Errorf("failed to reconcile %v", err)
	}

	return reconcileResponse, err
}

func (r *ReconcileObjectStoreRealm) reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the CephObjectStoreRealm instance
	cephObjectStoreRealm := &cephv1.CephObjectStoreRealm{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cephObjectStoreRealm)
	if err != nil {
		if kerrors.IsNotFound(err) {
			logger.Debug("CephObjectStoreRealm resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errors.Wrap(err, "failed to get CephObjectStoreRealm")
	}

	// The CR was just created, initializing status fields
	if cephObjectStoreRealm.Status == nil {
		cephObjectStoreRealm.Status = &cephv1.Status{}
		cephObjectStoreRealm.Status.Phase = k8sutil.Created
		err := opcontroller.UpdateStatus(r.client, cephObjectStoreRealm)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to set status")
		}
	}

	// Make sure a CephCluster is present otherwise do nothing
	_, isReadyToReconcile, cephClusterExists, reconcileResponse := opcontroller.IsReadyToReconcile(r.client, r.context, request.NamespacedName, controllerName)
	if !isReadyToReconcile {
		// This handles the case where the Ceph Cluster is gone and we want to delete that CR
		//
		// Also, only remove the finalizer if the CephCluster is gone
		// If not, we should wait for it to be ready
		// This handles the case where the operator is not ready to accept Ceph command but the cluster exists
		if !cephObjectStoreRealm.GetDeletionTimestamp().IsZero() && !cephClusterExists {
			// Remove finalizer
			err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreRealm)
			if err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
			}

			// Return and do not requeue. Successful deletion.
			return reconcile.Result{}, nil
		}
		return reconcileResponse, nil
	}

	// Set a finalizer so we can do cleanup before the object goes away
	err = opcontroller.AddFinalizerIfNotPresent(r.client, cephObjectStoreRealm)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to add finalizer")
	}

	// DELETE: the CR was deleted
	if !cephObjectStoreRealm.GetDeletionTimestamp().IsZero() {
		logger.Debugf("deleting realm CR %q", cephObjectStoreRealm.Name)

		// Remove finalizer
		err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreRealm)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
		}

		// Return and do not requeue. Successful deletion.
		return reconcile.Result{}, nil
	}

	// validate the realm settings
	err = ValidateRealmCR(cephObjectStoreRealm)
	if err != nil {
		cephObjectStoreRealm.Status.Phase = k8sutil.ReconcileFailedStatus
		err := opcontroller.UpdateStatus(r.client, cephObjectStoreRealm)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to set status")
		}
		return reconcile.Result{}, errors.Wrapf(err, "invalid name CR %q spec", cephObjectStoreRealm.Name)
	}

	// Start object reconciliation, updating status for this
	cephObjectStoreRealm.Status.Phase = k8sutil.ReconcilingStatus
	err = opcontroller.UpdateStatus(r.client, cephObjectStoreRealm)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to set status")
	}

	// CREATE/UPDATE CEPH REALM
	reconcileResponse, err = r.reconcileCephRealm(cephObjectStoreRealm)
	if err != nil {
		cephObjectStoreRealm.Status.Phase = k8sutil.ReconcileFailedStatus
		errStatus := opcontroller.UpdateStatus(r.client, cephObjectStoreRealm)
		if errStatus != nil {
			logger.Errorf("failed to set status. %v", errStatus)
		}
		return reconcileResponse, err
	}

	// Set Ready status, we are done reconciling
	cephObjectStoreRealm.Status.Phase = k8sutil.ReadyStatus
	err = opcontroller.UpdateStatus(r.client, cephObjectStoreRealm)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to set status")
	}

	// Return and do not requeue
	logger.Debug("done reconciling")
	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreRealm) reconcileCephRealm(cephObjectStoreRealm *cephv1.CephObjectStoreRealm) (reconcile.Result, error) {
	err := createCephRealm(r.context, cephObjectStoreRealm.Name, cephObjectStoreRealm.Namespace)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to create object store realm %q", cephObjectStoreRealm.Name)
	}

	return reconcile.Result{}, nil
}

// ValidateRealmCR validates the realm arguments
func ValidateRealmCR(u *cephv1.CephObjectStoreRealm) error {
	if u.Name == "" {
		return errors.New("missing name")
	}
	if u.Namespace == "" {
		return errors.New("missing namespace")
	}
	return nil
}
