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

// Package objectzone to manage a rook object store zone.
package objectzone

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
	controllerName = "ceph-object-store-zone-controller"
	waitTime       = 10 * time.Second
)

var WaitForRequeueIfObjectStoreZoneGroupNotReady = reconcile.Result{Requeue: true, RequeueAfter: waitTime}

var logger = capnslog.NewPackageLogger("github.com/rook/rook", controllerName)

var cephObjectStoreZoneKind = reflect.TypeOf(cephv1.CephObjectStoreZone{}).Name()

// Sets the type meta for the controller main object
var controllerTypeMeta = metav1.TypeMeta{
	Kind:       cephObjectStoreZoneKind,
	APIVersion: fmt.Sprintf("%s/%s", cephv1.CustomResourceGroup, cephv1.Version),
}

// ReconcileObjectStoreZone reconciles a ObjectStoreZone object
type ReconcileObjectStoreZone struct {
	client  client.Client
	scheme  *runtime.Scheme
	context *clusterd.Context
}

// Add creates a new CephObjectStoreZone Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, context *clusterd.Context) error {
	return add(mgr, newReconciler(mgr, context))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, context *clusterd.Context) reconcile.Reconciler {
	// Add the cephv1 scheme to the manager scheme so that the controller knows about it
	mgrScheme := mgr.GetScheme()
	cephv1.AddToScheme(mgr.GetScheme())

	return &ReconcileObjectStoreZone{
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

	// Watch for changes on the CephObjectStoreZone CRD object
	err = c.Watch(&source.Kind{Type: &cephv1.CephObjectStoreZone{TypeMeta: controllerTypeMeta}}, &handler.EnqueueRequestForObject{}, opcontroller.WatchControllerPredicate())
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a CephObjectStoreZone object and makes changes based on the state read
// and what is in the CephObjectStoreZone.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileObjectStoreZone) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// workaround because the rook logging mechanism is not compatible with the controller-runtime loggin interface
	reconcileResponse, err := r.reconcile(request)
	if err != nil {
		logger.Errorf("failed to reconcile: %v", err)
	}

	return reconcileResponse, err
}

func (r *ReconcileObjectStoreZone) reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the CephObjectStoreZone instance
	cephObjectStoreZone := &cephv1.CephObjectStoreZone{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cephObjectStoreZone)
	if err != nil {
		if kerrors.IsNotFound(err) {
			logger.Debug("CephObjectStoreZone resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errors.Wrap(err, "failed to get CephObjectStoreZone")
	}

	// The CR was just created, initializing status fields
	if cephObjectStoreZone.Status == nil {
		cephObjectStoreZone.Status = &cephv1.Status{}
		cephObjectStoreZone.Status.Phase = k8sutil.Created
		err := opcontroller.UpdateStatus(r.client, cephObjectStoreZone)
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
		if !cephObjectStoreZone.GetDeletionTimestamp().IsZero() && !cephClusterExists {
			// Remove finalizer
			err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreZone)
			if err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
			}

			// Return and do not requeue. Successful deletion.
			return reconcile.Result{}, nil
		}
		return reconcileResponse, nil
	}

	// Set a finalizer so we can do cleanup before the object goes away
	err = opcontroller.AddFinalizerIfNotPresent(r.client, cephObjectStoreZone)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to add finalizer")
	}

	// DELETE: the CR was deleted
	if !cephObjectStoreZone.GetDeletionTimestamp().IsZero() {
		logger.Debugf("deleting zone CR %q", cephObjectStoreZone.Name)

		// Remove finalizer
		err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreZone)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
		}

		// Return and do not requeue. Successful deletion.
		return reconcile.Result{}, nil
	}

	// validate the zone settings
	err = ValidateZoneCR(cephObjectStoreZone)
	if err != nil {
		cephObjectStoreZone.Status.Phase = k8sutil.ReconcileFailedStatus
		err := opcontroller.UpdateStatus(r.client, cephObjectStoreZone)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to set status")
		}
		return reconcile.Result{}, errors.Wrapf(err, "invalid name CR %q spec", cephObjectStoreZone.Name)
	}

	// Start object reconciliation, updating status for this
	cephObjectStoreZone.Status.Phase = k8sutil.ReconcilingStatus
	err = opcontroller.UpdateStatus(r.client, cephObjectStoreZone)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to set status")
	}

	// Make sure an ObjectStoreZoneGroup is present
	realmName, zoneGroupName, reconcileResponse, err := r.reconcileObjectStoreZoneGroup(cephObjectStoreZone)
	if err != nil {
		return reconcileResponse, err
	}
	logger.Debugf("CephObjectStoreZoneGroup %s found", zoneGroupName)

	// Make sure Realm has been created in Ceph Cluster
	zoneGroupName, reconcileResponse, err = r.reconcileCephZoneGroup(cephObjectStoreZone, realmName)
	if err != nil {
		return reconcileResponse, err
	}
	logger.Debugf("Zone Group %s found in Ceph cluster", zoneGroupName)

	// CREATE/UPDATE CEPH ZONE
	reconcileResponse, err = r.reconcileCephZone(cephObjectStoreZone)
	if err != nil {
		cephObjectStoreZone.Status.Phase = k8sutil.ReconcileFailedStatus
		errStatus := opcontroller.UpdateStatus(r.client, cephObjectStoreZone)
		if errStatus != nil {
			logger.Errorf("failed to set status. %v", errStatus)
		}
		return reconcileResponse, err
	}

	// Set Ready status, we are done reconciling
	cephObjectStoreZone.Status.Phase = k8sutil.ReadyStatus
	err = opcontroller.UpdateStatus(r.client, cephObjectStoreZone)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to set status")
	}

	// Return and do not requeue
	logger.Debug("done reconciling")
	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreZone) reconcileCephZone(cephObjectStoreZone *cephv1.CephObjectStoreZone) (reconcile.Result, error) {
	err := createCephZone(r.context, cephObjectStoreZone.Name, cephObjectStoreZone.Namespace, cephObjectStoreZone.Spec.Realm, cephObjectStoreZone.Spec.ZoneGroup, cephObjectStoreZone.Spec.IsMaster)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to create object store zone %q", cephObjectStoreZone.Name)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreZone) reconcileObjectStoreZoneGroup(cephObjectStoreZone *cephv1.CephObjectStoreZone) (string, string, reconcile.Result, error) {
	cephObjectStoreZoneGroup, err := getObjectStoreZoneGroup(r.context.RookClientset.CephV1(), cephObjectStoreZone.Namespace, cephObjectStoreZone.Spec.ZoneGroup)
	if err != nil || cephObjectStoreZoneGroup == nil {
		return "", cephObjectStoreZone.Spec.ZoneGroup, WaitForRequeueIfObjectStoreZoneGroupNotReady, err
	}

	return cephObjectStoreZoneGroup.Spec.Realm, cephObjectStoreZoneGroup.Name, reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreZone) reconcileCephZoneGroup(cephObjectStoreZone *cephv1.CephObjectStoreZone, realmName string) (string, reconcile.Result, error) {
	err := getCephZoneGroup(r.context, cephObjectStoreZone.Name, cephObjectStoreZone.Namespace, cephObjectStoreZone.Spec.ZoneGroup, realmName)
	if err != nil {
		return cephObjectStoreZone.Spec.ZoneGroup, WaitForRequeueIfObjectStoreZoneGroupNotReady, err
	}

	return cephObjectStoreZone.Spec.ZoneGroup, reconcile.Result{}, nil
}

// ValidateZoneCR validates the zone arguments
func ValidateZoneCR(u *cephv1.CephObjectStoreZone) error {
	if u.Name == "" {
		return errors.New("missing name")
	}
	if u.Namespace == "" {
		return errors.New("missing namespace")
	}
	if u.Spec.ZoneGroup == "" {
		return errors.New("missing zonegroup")
	}
	return nil
}
