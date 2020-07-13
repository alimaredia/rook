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

// Package objectuser to manage a rook object store user.
package objectuser

import (
	"context"
	"fmt"
	"reflect"
	"syscall"
	"time"

	"github.com/rook/rook/pkg/operator/ceph/cluster/mon"
	opcontroller "github.com/rook/rook/pkg/operator/ceph/controller"
	"github.com/rook/rook/pkg/util/exec"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/rook/rook/pkg/clusterd"
	cephconfig "github.com/rook/rook/pkg/daemon/ceph/config"
	"github.com/rook/rook/pkg/operator/ceph/object"
	"github.com/rook/rook/pkg/operator/k8sutil"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	appName             = object.AppName
	controllerName      = "ceph-object-store-user-controller"
	cephObjectStoreKind = "CephObjectStoreUser"
)

var waitForRequeueIfMultisiteNotReady = reconcile.Result{Requeue: true, RequeueAfter: 10 * time.Second}

var logger = capnslog.NewPackageLogger("github.com/rook/rook", controllerName)

var cephObjectStoreUserKind = reflect.TypeOf(cephv1.CephObjectStoreUser{}).Name()

// Sets the type meta for the controller main object
var controllerTypeMeta = metav1.TypeMeta{
	Kind:       cephObjectStoreUserKind,
	APIVersion: fmt.Sprintf("%s/%s", cephv1.CustomResourceGroup, cephv1.Version),
}

// ReconcileObjectStoreUser reconciles a ObjectStoreUser object
type ReconcileObjectStoreUser struct {
	client          client.Client
	scheme          *runtime.Scheme
	context         *clusterd.Context
	objContext      *object.Context
	userConfig      object.ObjectUser
	cephClusterSpec *cephv1.ClusterSpec
	clusterInfo     *cephconfig.ClusterInfo
}

// Add creates a new CephObjectStoreUser Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, context *clusterd.Context) error {
	return add(mgr, newReconciler(mgr, context))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, context *clusterd.Context) reconcile.Reconciler {
	// Add the cephv1 scheme to the manager scheme so that the controller knows about it
	mgrScheme := mgr.GetScheme()
	cephv1.AddToScheme(mgr.GetScheme())

	return &ReconcileObjectStoreUser{
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
	logger.Info("successfully started")

	// Watch for changes on the CephObjectStoreUser CRD object
	err = c.Watch(&source.Kind{Type: &cephv1.CephObjectStoreUser{TypeMeta: controllerTypeMeta}}, &handler.EnqueueRequestForObject{}, opcontroller.WatchControllerPredicate())
	if err != nil {
		return err
	}

	// Watch secrets
	err = c.Watch(&source.Kind{Type: &corev1.Secret{TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: corev1.SchemeGroupVersion.String()}}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &cephv1.CephObjectStoreUser{},
	}, opcontroller.WatchPredicateForNonCRDObject(&cephv1.CephObjectStoreUser{TypeMeta: controllerTypeMeta}, mgr.GetScheme()))
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a CephObjectStoreUser object and makes changes based on the state read
// and what is in the CephObjectStoreUser.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileObjectStoreUser) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// workaround because the rook logging mechanism is not compatible with the controller-runtime loggin interface
	reconcileResponse, err := r.reconcile(request)
	if err != nil {
		logger.Errorf("failed to reconcile %v", err)
	}

	return reconcileResponse, err
}

func (r *ReconcileObjectStoreUser) reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the CephObjectStoreUser instance
	cephObjectStoreUser := &cephv1.CephObjectStoreUser{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cephObjectStoreUser)
	if err != nil {
		if kerrors.IsNotFound(err) {
			logger.Debug("CephObjectStoreUser resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errors.Wrap(err, "failed to get CephObjectStoreUser")
	}

	// The CR was just created, initializing status fields
	if cephObjectStoreUser.Status == nil {
		updateStatus(r.client, request.NamespacedName, k8sutil.Created)
	}

	// Make sure a CephCluster is present otherwise do nothing
	cephCluster, isReadyToReconcile, cephClusterExists, reconcileResponse := opcontroller.IsReadyToReconcile(r.client, r.context, request.NamespacedName, controllerName)
	if !isReadyToReconcile {
		// This handles the case where the Ceph Cluster is gone and we want to delete that CR
		// We skip the deleteUser() function since everything is gone already
		//
		// Also, only remove the finalizer if the CephCluster is gone
		// If not, we should wait for it to be ready
		// This handles the case where the operator is not ready to accept Ceph command but the cluster exists
		if !cephObjectStoreUser.GetDeletionTimestamp().IsZero() && !cephClusterExists {
			// Remove finalizer
			err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreUser)
			if err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
			}

			// Return and do not requeue. Successful deletion.
			return reconcile.Result{}, nil
		}
		return reconcileResponse, nil
	}
	r.cephClusterSpec = &cephCluster.Spec

	// Set a finalizer so we can do cleanup before the object goes away
	err = opcontroller.AddFinalizerIfNotPresent(r.client, cephObjectStoreUser)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to add finalizer")
	}

	// Populate clusterInfo
	// Always populate it during each reconcile
	var clusterInfo *cephconfig.ClusterInfo
	if r.cephClusterSpec.External.Enable {
		clusterInfo = mon.PopulateExternalClusterInfo(r.context, request.NamespacedName.Namespace)
	} else {
		clusterInfo, _, _, err = mon.LoadClusterInfo(r.context, request.NamespacedName.Namespace)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to populate cluster info")
		}
	}
	r.clusterInfo = clusterInfo

	// validate isObjectStoreInitialized
	store, err := r.isObjectStoreInitialized(cephObjectStoreUser)
	if err != nil {
		if !cephObjectStoreUser.GetDeletionTimestamp().IsZero() {
			// Remove finalizer
			err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreUser)
			if err != nil {
				return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
			}

			// Return and do not requeue. Successful deletion.
			return reconcile.Result{}, nil
		}
		logger.Debugf("ObjectStore resource not ready in namespace %q, retrying in %q. %v",
			request.NamespacedName.Namespace, opcontroller.WaitForRequeueIfCephClusterNotReady.RequeueAfter.String(), err)
		updateStatus(r.client, request.NamespacedName, k8sutil.ReconcileFailedStatus)
		return opcontroller.WaitForRequeueIfCephClusterNotReady, nil
	}

	// Generate user config
	userConfig := generateUserConfig(cephObjectStoreUser)
	r.userConfig = userConfig

	// Set the cephx external username if the CephCluster is external
	if r.cephClusterSpec.External.Enable {
		r.objContext.RunAsUser = r.clusterInfo.ExternalCred.Username
	}

	// DELETE: the CR was deleted
	if !cephObjectStoreUser.GetDeletionTimestamp().IsZero() {
		logger.Debugf("deleting pool %q", cephObjectStoreUser.Name)
		err := r.deleteUser(cephObjectStoreUser)
		if err != nil {
			return reconcile.Result{}, errors.Wrapf(err, "failed to delete ceph object user %q", cephObjectStoreUser.Name)
		}

		// Remove finalizer
		err = opcontroller.RemoveFinalizer(r.client, cephObjectStoreUser)
		if err != nil {
			return reconcile.Result{}, errors.Wrap(err, "failed to remove finalizer")
		}

		// Return and do not requeue. Successful deletion.
		return reconcile.Result{}, nil
	}

	// validate the user settings
	err = r.validateUser(cephObjectStoreUser)
	if err != nil {
		updateStatus(r.client, request.NamespacedName, k8sutil.ReconcileFailedStatus)
		return reconcile.Result{}, errors.Wrapf(err, "invalid pool CR %q spec", cephObjectStoreUser.Name)
	}

	// If object store uses multisite, get names of Realm/Zonegroup/Zone and check to make sure they have been created
	if store.Spec.IsMultisite() {
		realmName, zoneGroupName, zoneName, reconcileResponse, err := r.reconcileMultisiteCRs(cephObjectStoreUser, store)
		if err != nil {
			return reconcileResponse, errors.Wrapf(err, "multisite CRs have not been configured for user %q", cephObjectStoreUser.Name)
		}

		reconcileResponse, err = r.reconcileCephMultisite(cephObjectStoreUser, realmName, zoneGroupName, zoneName)
		if err != nil {
			return reconcileResponse, errors.Wrapf(err, "ceph multisite has not been configured for user %q", cephObjectStoreUser.Name)
		}

		r.userConfig.Realm = &realmName
		r.userConfig.ZoneGroup = &zoneGroupName
		r.userConfig.Zone = &zoneName
	}

	// CREATE/UPDATE CEPH USER
	reconcileResponse, err = r.reconcileCephUser(cephObjectStoreUser)
	if err != nil {
		updateStatus(r.client, request.NamespacedName, k8sutil.ReconcileFailedStatus)
		return reconcileResponse, err
	}

	// CREATE/UPDATE KUBERNETES SECRET
	reconcileResponse, err = r.reconcileCephUserSecret(cephObjectStoreUser)
	if err != nil {
		updateStatus(r.client, request.NamespacedName, k8sutil.ReconcileFailedStatus)
		return reconcileResponse, err
	}

	// Set Ready status, we are done reconciling
	updateStatus(r.client, request.NamespacedName, k8sutil.ReadyStatus)

	// Return and do not requeue
	logger.Debug("done reconciling")
	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreUser) reconcileCephUser(cephObjectStoreUser *cephv1.CephObjectStoreUser) (reconcile.Result, error) {
	err := r.createCephUser(cephObjectStoreUser)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to create object store user %q", cephObjectStoreUser.Name)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreUser) createCephUser(u *cephv1.CephObjectStoreUser) error {
	logger.Infof("creating ceph object user %q in namespace %q", u.Name, u.Namespace)
	user, rgwerr, err := object.CreateUser(r.objContext, r.userConfig)
	if err != nil {
		if rgwerr == object.ErrorCodeFileExists {
			objectUser, _, err := object.GetUser(r.objContext, r.userConfig.UserID)
			if err != nil {
				return errors.Wrapf(err, "failed to get details from ceph object user %q", objectUser.UserID)
			}

			// Set access and secret key
			r.userConfig.AccessKey = objectUser.AccessKey
			r.userConfig.SecretKey = objectUser.SecretKey

			return nil
		}
		return errors.Wrapf(err, "failed to create ceph object user %q. error code %d", u.Name, rgwerr)
	}

	// Set access and secret key
	r.userConfig.AccessKey = user.AccessKey
	r.userConfig.SecretKey = user.SecretKey

	logger.Infof("created ceph object user %q", u.Name)
	return nil
}

func (r *ReconcileObjectStoreUser) isObjectStoreInitialized(u *cephv1.CephObjectStoreUser) (*cephv1.CephObjectStore, error) {
	objContext := object.NewContext(r.context, u.Spec.Store, u.Namespace)
	r.objContext = objContext

	err := r.objectStoreInitialized(u)
	if err != nil {
		return nil, errors.Wrap(err, "failed to detect if object store is initialized")
	}

	store, err := r.context.RookClientset.CephV1().CephObjectStores(u.Namespace).Get(u.Spec.Store, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed get object store %q", u.Spec.Store)
	}

	return store, nil
}

func generateUserConfig(user *cephv1.CephObjectStoreUser) object.ObjectUser {
	// Set DisplayName to match Name if DisplayName is not set
	displayName := user.Spec.DisplayName
	if len(displayName) == 0 {
		displayName = user.Name
	}

	realm := user.Spec.Store
	zoneGroup := user.Spec.Store
	zone := user.Spec.Store

	// create the user
	userConfig := object.ObjectUser{
		UserID:      user.Name,
		DisplayName: &displayName,
		Realm:       &realm,
		ZoneGroup:   &zoneGroup,
		Zone:        &zone,
	}

	return userConfig
}

func (r *ReconcileObjectStoreUser) generateCephUserSecret(u *cephv1.CephObjectStoreUser) *v1.Secret {
	// Store the keys in a secret
	secrets := map[string]string{
		"AccessKey": *r.userConfig.AccessKey,
		"SecretKey": *r.userConfig.SecretKey,
		"Endpoint":  r.objContext.Endpoint,
	}

	secretName := fmt.Sprintf("rook-ceph-object-user-%s-%s", u.Spec.Store, u.Name)
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: u.Namespace,
			Labels: map[string]string{
				"app":               appName,
				"user":              u.Name,
				"rook_cluster":      u.Namespace,
				"rook_object_store": u.Spec.Store,
			},
		},
		StringData: secrets,
		Type:       k8sutil.RookType,
	}

	return secret
}

func (r *ReconcileObjectStoreUser) reconcileCephUserSecret(cephObjectStoreUser *cephv1.CephObjectStoreUser) (reconcile.Result, error) {
	// Generate Kubernetes Secret
	secret := r.generateCephUserSecret(cephObjectStoreUser)

	// Set owner ref to the object store user object
	err := controllerutil.SetControllerReference(cephObjectStoreUser, secret, r.scheme)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to set owner reference for ceph object user %q secret", secret.Name)
	}

	// Create Kubernetes Secret
	err = opcontroller.CreateOrUpdateObject(r.client, secret)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to create or update ceph object user %q secret", secret.Name)
	}

	logger.Infof("created ceph object user secret %q", secret.Name)
	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreUser) reconcileMultisiteCRs(user *cephv1.CephObjectStoreUser, store *cephv1.CephObjectStore) (string, string, string, reconcile.Result, error) {
	zone, err := r.context.RookClientset.CephV1().CephObjectZones(user.Namespace).Get(store.Spec.Zone.Name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return "", "", "", waitForRequeueIfMultisiteNotReady, err
		}
		return "", "", "", waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "error getting CephObjectZone %q for user %q", store.Spec.Zone.Name, user.Name)
	}
	logger.Debugf("CephObjectZone resource %s found for user %q", zone.Name, user.Name)

	zoneGroup, err := r.context.RookClientset.CephV1().CephObjectZoneGroups(user.Namespace).Get(zone.Spec.ZoneGroup, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return "", "", "", waitForRequeueIfMultisiteNotReady, err
		}
		return "", "", "", waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "error getting CephObjectZoneGroup %q for user %q", zone.Spec.ZoneGroup, user.Name)
	}
	logger.Debugf("CephObjectZoneGroup resource %s found for user %q", zoneGroup.Name, user.Name)

	realm, err := r.context.RookClientset.CephV1().CephObjectRealms(user.Namespace).Get(zoneGroup.Spec.Realm, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return "", "", "", waitForRequeueIfMultisiteNotReady, err
		}
		return "", "", "", waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "error getting CephObjectRealm %q for user %q", zoneGroup.Spec.Realm, user.Name)
	}
	logger.Debugf("CephObjectRealm resource %s found for user %q", realm.Name, user.Name)

	return realm.Name, zoneGroup.Name, zone.Name, reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreUser) reconcileCephMultisite(user *cephv1.CephObjectStoreUser, realmName, zoneGroupName, zoneName string) (reconcile.Result, error) {
	realmArg := fmt.Sprintf("--rgw-realm=%s", realmName)
	zoneGroupArg := fmt.Sprintf("--rgw-zonegroup=%s", zoneGroupName)
	zoneArg := fmt.Sprintf("--rgw-zone=%s", zoneName)
	objContext := object.NewContext(r.context, user.Name, user.Namespace)

	_, err := object.RunAdminCommandNoRealm(objContext, "zone", "get", realmArg, zoneGroupArg, zoneArg)
	if err != nil {
		if code, ok := exec.ExitStatus(err); ok && code == int(syscall.ENOENT) {
			return waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "ceph zone %q not found for user %q", zoneName, user.Name)
		} else {
			return waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "radosgw-admin zone get failed with code %d", code)
		}
	}
	logger.Infof("ceph zone %s found for user %q", zoneName, user.Name)

	_, err = object.RunAdminCommandNoRealm(objContext, "zonegroup", "get", realmArg, zoneGroupArg)
	if err != nil {
		if code, ok := exec.ExitStatus(err); ok && code == int(syscall.ENOENT) {
			return waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "ceph zone group %q not found for user %q", zoneGroupName, user.Name)
		} else {
			return waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "radosgw-admin zonegroup get failed with code %d", code)
		}
	}
	logger.Infof("ceph zone group %s found for user %q", zoneGroupName, user.Name)

	_, err = object.RunAdminCommandNoRealm(objContext, "realm", "get", realmArg)
	if err != nil {
		if code, ok := exec.ExitStatus(err); ok && code == int(syscall.ENOENT) {
			return waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "ceph realm %q not found for user %q", realmName, user.Name)
		} else {
			return waitForRequeueIfMultisiteNotReady, errors.Wrapf(err, "radosgw-admin realm get failed with code %d", code)
		}
	}
	logger.Infof("ceph realm %s found for user %q", realmName, user.Name)

	return reconcile.Result{}, nil
}

func (r *ReconcileObjectStoreUser) objectStoreInitialized(cephObjectStoreUser *cephv1.CephObjectStoreUser) error {
	err := r.getObjectStore(cephObjectStoreUser.Spec.Store)
	if err != nil {
		return err
	}
	logger.Debug("CephObjectStore exists")

	// If the cluster is external just return
	// since there are no pods running
	if r.cephClusterSpec.External.Enable {
		return nil
	}

	// There are no pods running when the cluster is external
	// Unless you pass the admin key...
	pods, err := r.getRgwPodList(cephObjectStoreUser)
	if err != nil {
		return err
	}

	// check if at least one pod is running
	if len(pods.Items) > 0 {
		logger.Debugf("CephObjectStore %q is running with %d pods", cephObjectStoreUser.Name, len(pods.Items))
		return nil
	}

	return errors.New("no rgw pod found")
}

func (r *ReconcileObjectStoreUser) getObjectStore(storeName string) error {
	// check if CephObjectStore CR is created
	objectStores := &cephv1.CephObjectStoreList{}
	err := r.client.List(context.TODO(), objectStores)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return errors.Wrapf(err, "CephObjectStore %q could not be found", storeName)
		}
		return errors.Wrap(err, "failed to get CephObjectStore")
	}

	for _, store := range objectStores.Items {
		if store.Name == storeName {
			logger.Infof("CephObjectStore %q found", storeName)
			r.objContext.Endpoint = store.Status.Info["endpoint"]
			return nil
		}
	}

	return errors.Errorf("CephObjectStore %q could not be found", storeName)
}

func (r *ReconcileObjectStoreUser) getRgwPodList(cephObjectStoreUser *cephv1.CephObjectStoreUser) (*corev1.PodList, error) {
	pods := &corev1.PodList{}

	// check if ObjectStore is initialized
	// rook does this by starting the RGW pod(s)
	listOpts := []client.ListOption{
		client.InNamespace(cephObjectStoreUser.Namespace),
		client.MatchingLabels(labelsForRgw(cephObjectStoreUser.Spec.Store)),
	}

	err := r.client.List(context.TODO(), pods, listOpts...)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return pods, errors.Wrap(err, "no rgw pod could not be found")
		}
		return pods, errors.Wrap(err, "failed to list rgw pods")
	}

	return pods, nil
}

// Delete the user
func (r *ReconcileObjectStoreUser) deleteUser(u *cephv1.CephObjectStoreUser) error {
	output, err := object.DeleteUser(r.objContext, u.Name)
	if err != nil {
		return errors.Wrapf(err, "failed to delete ceph object user %q. %v", u.Name, output)
	}

	logger.Infof("ceph object user %q deleted successfully", u.Name)
	return nil
}

// validateUser validates the user arguments
func (r *ReconcileObjectStoreUser) validateUser(u *cephv1.CephObjectStoreUser) error {
	if u.Name == "" {
		return errors.New("missing name")
	}
	if u.Namespace == "" {
		return errors.New("missing namespace")
	}
	if !r.cephClusterSpec.External.Enable {
		if u.Spec.Store == "" {
			return errors.New("missing store")
		}
	}
	return nil
}

func labelsForRgw(name string) map[string]string {
	return map[string]string{"rgw": name, k8sutil.AppAttr: appName}
}

// updateStatus updates an object with a given status
func updateStatus(client client.Client, name types.NamespacedName, status string) {
	user := &cephv1.CephObjectStoreUser{}
	if err := client.Get(context.TODO(), name, user); err != nil {
		if kerrors.IsNotFound(err) {
			logger.Debug("CephObjectStoreUser resource not found. Ignoring since object must be deleted.")
			return
		}
		logger.Warningf("failed to retrieve object store user %q to update status to %q. %v", name, status, err)
		return
	}
	if user.Status == nil {
		user.Status = &cephv1.Status{}
	}

	user.Status.Phase = status
	if err := opcontroller.UpdateStatus(client, user); err != nil {
		logger.Errorf("failed to set object store user %q status to %q. %v", name, status, err)
		return
	}
	logger.Debugf("object store user %q status updated to %q", name, status)
}
