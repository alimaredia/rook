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

// Package objectzonegroup to manage a rook object store zonegroup.
package objectzonegroup

import (
	"context"
	"fmt"
	"reflect"
	"time"

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
	controllerName = "ceph-object-store-zonegroup-controller"
	waitTime       = 10 * time.Second
)

var WaitForRequeueIfObjectStoreRealmNotReady = reconcile.Result{Requeue: true, RequeueAfter: waitTime}

var logger = capnslog.NewPackageLogger("github.com/rook/rook", controllerName)

var cephObjectStoreZoneGroupKind = reflect.TypeOf(cephv1.CephObjectStoreZoneGroup{}).Name()

// Sets the type meta for the controller main object
var controllerTypeMeta = metav1.TypeMeta{
	Kind:       cephObjectStoreZoneGroupKind,
	APIVersion: fmt.Sprintf("%s/%s", cephv1.CustomResourceGroup, cephv1.Version),
}

// ReconcileObjectStoreZoneGroup reconciles a ObjectStoreZoneGroup object
type ReconcileObjectStoreZoneGroup struct {
	client  client.Client
	scheme  *runtime.Scheme
	context *clusterd.Context
}

// Add creates a new CephObjectStoreZoneGroup Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, context *clusterd.Context) error {
	return add(mgr, newReconciler(mgr, context))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, context *clusterd.Context) reconcile.Reconciler {
	// Add the cephv1 scheme to the manager scheme so that the controller knows about it
	mgrScheme := mgr.GetScheme()
	cephv1.AddToScheme(mgr.GetScheme())

	return &ReconcileObjectStoreZoneGroup{
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

	// Watch for changes on the CephObjectStoreZoneGroup CRD object
	err = c.Watch(&source.Kind{Type: &cephv1.CephObjectStoreZoneGroup{TypeMeta: controllerTypeMeta}}, &handler.EnqueueRequestForObject{}, opcontroller.WatchControllerPredicate())
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a CephObjectStoreZoneGroup object and makes changes based on the state read
// and what is in the CephObjectStoreZoneGroup.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileObjectStoreZoneGroup) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// workaround because the rook logging mechanism is not compatible with the controller-runtime loggin interface
	reconcileResponse, err := r.reconcile(request)
	if err != nil {
		logger.Errorf("failed to reconcile %v", err)
	}

	return reconcileResponse, err
}

func (r *ReconcileObjectStoreZoneGroup) reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the CephObjectStoreZoneGroup instance
	cephObjectStoreZoneGroup := &cephv1.CephObjectStoreZoneGroup{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cephObjectStoreZoneGroup)
	if err != nil {
		if kerrors.IsNotFound(err) {
			logger.Debug("CephObjectStoreZoneGroup resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errors.Wrap(err, "failed to get CephObjectStoreZoneGroup")
	}

	// The CR was just created, initializing status fields
	if cephObjectStoreZoneGroup.Status == nil {
		cephObjectStoreZoneGroup.Status = &cephv1.Status{}
		cephObjectStoreZoneGroup.Status.Phase = k8sutil.Created
		err := opcontroller.UpdateStatus(r.client, cephObjectStoreZoneGroup)
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
		if !cephObjectStoreZoneGroup.GetDeletionTimestamp().IsZero() && !cephClusterExists {
			// Remove finalizer
			err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreZoneGroup)
			if err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
			}

			// Return and do not requeue. Successful deletion.
			return reconcile.Result{}, nil
		}
		return reconcileResponse, nil
	}

	// Set a finalizer so we can do cleanup before the object goes away
	err = opcontroller.AddFinalizerIfNotPresent(r.client, cephObjectStoreZoneGroup)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to add finalizer")
	}

	// DELETE: the CR was deleted
	if !cephObjectStoreZoneGroup.GetDeletionTimestamp().IsZero() {
		logger.Debugf("deleting zonegroup CR %q", cephObjectStoreZoneGroup.Name)

		// Remove finalizer
		err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreZoneGroup)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
		}

		// Return and do not requeue. Successful deletion.
		return reconcile.Result{}, nil
	}

	// validate the zonegroup settings
	err = ValidateZoneGroup(cephObjectStoreZoneGroup)
	if err != nil {
		cephObjectStoreZoneGroup.Status.Phase = k8sutil.ReconcileFailedStatus
		err := opcontroller.UpdateStatus(r.client, cephObjectStoreZoneGroup)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to set status")
		}
		return reconcile.Result{}, errors.Wrapf(err, "invalid name CR %q spec", cephObjectStoreZoneGroup.Name)
	}

	// Start object reconciliation, updating status for this
	cephObjectStoreZoneGroup.Status.Phase = k8sutil.ReconcilingStatus
	err = opcontroller.UpdateStatus(r.client, cephObjectStoreZoneGroup)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to set status")
	}

	// Make sure an ObjectStoreRealm is present
	realmName, reconcileResponse, err := r.reconcileObjectStoreRealm(cephObjectStoreZoneGroup)
	if err != nil {
		return reconcileResponse, err
	}
	logger.Debugf("CephObjectStoreRealm %s found", realmName)

	// Make sure Realm has been created in Ceph Cluster
	realmName, reconcileResponse, err = r.reconcileCephRealm(cephObjectStoreZoneGroup)
	if err != nil {
		return reconcileResponse, err
	}
	logger.Debugf("Realm %s found in Ceph cluster", realmName)

	// CREATE/UPDATE CEPH ZONEGROUP
	reconcileResponse, err = r.reconcileCephZoneGroup(cephObjectStoreZoneGroup)
	if err != nil {
		cephObjectStoreZoneGroup.Status.Phase = k8sutil.ReconcileFailedStatus
		errStatus := opcontroller.UpdateStatus(r.client, cephObjectStoreZoneGroup)
		if errStatus != nil {
			logger.Errorf("failed to set status. %v", errStatus)
		}
		return reconcileResponse, err
	}

	// Set Ready status, we are done reconciling
	cephObjectStoreZoneGroup.Status.Phase = k8sutil.ReadyStatus
	err = opcontroller.UpdateStatus(r.client, cephObjectStoreZoneGroup)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to set status")
	}

	// Return and do not requeue
	logger.Debug("done reconciling")
	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreZoneGroup) reconcileCephZoneGroup(cephObjectStoreZoneGroup *cephv1.CephObjectStoreZoneGroup) (reconcile.Result, error) {
	err := createCephZoneGroup(r.context, cephObjectStoreZoneGroup.Name, cephObjectStoreZoneGroup.Namespace, cephObjectStoreZoneGroup.Spec.Realm, cephObjectStoreZoneGroup.Spec.IsMaster)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to create object store zonegroup %q", cephObjectStoreZoneGroup.Name)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreZoneGroup) reconcileObjectStoreRealm(cephObjectStoreZoneGroup *cephv1.CephObjectStoreZoneGroup) (string, reconcile.Result, error) {
	cephObjectStoreRealm, err := getObjectStoreRealm(r.context.RookClientset.CephV1(), cephObjectStoreZoneGroup.Namespace, cephObjectStoreZoneGroup.Spec.Realm)
	if err != nil {
		return cephObjectStoreZoneGroup.Spec.Realm, WaitForRequeueIfObjectStoreRealmNotReady, errors.Wrapf(err, "failed to find CephObjectStoreRealm %q", cephObjectStoreZoneGroup.Spec.Realm)
	}

	return cephObjectStoreRealm.Name, reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreZoneGroup) reconcileCephRealm(cephObjectStoreZoneGroup *cephv1.CephObjectStoreZoneGroup) (string, reconcile.Result, error) {
	err := getCephRealm(r.context, cephObjectStoreZoneGroup.Name, cephObjectStoreZoneGroup.Namespace, cephObjectStoreZoneGroup.Spec.Realm)
	if err != nil {
		return cephObjectStoreZoneGroup.Spec.Realm, WaitForRequeueIfObjectStoreRealmNotReady, errors.Wrapf(err, "realm %s does not exist in the Ceph cluster", cephObjectStoreZoneGroup.Spec.Realm)
	}

	return cephObjectStoreZoneGroup.Spec.Realm, reconcile.Result{}, nil
}

// ValidateZoneGroup validates the zonegroup arguments
func ValidateZoneGroup(u *cephv1.CephObjectStoreZoneGroup) error {
	if u.Name == "" {
		return errors.New("missing name")
	}
	if u.Namespace == "" {
		return errors.New("missing namespace")
	}
	if u.Spec.Realm == "" {
		return errors.New("missing realm")
	}
	return nil
}
