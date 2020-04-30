/*
Copyright The Kubernetes Authors.

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

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	internalinterfaces "github.com/rook/rook/pkg/client/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// CephBlockPools returns a CephBlockPoolInformer.
	CephBlockPools() CephBlockPoolInformer
	// CephClients returns a CephClientInformer.
	CephClients() CephClientInformer
	// CephClusters returns a CephClusterInformer.
	CephClusters() CephClusterInformer
	// CephFilesystems returns a CephFilesystemInformer.
	CephFilesystems() CephFilesystemInformer
	// CephNFSes returns a CephNFSInformer.
	CephNFSes() CephNFSInformer
	// CephObjectStores returns a CephObjectStoreInformer.
	CephObjectStores() CephObjectStoreInformer
	// CephObjectStoreRealms returns a CephObjectStoreRealmInformer.
	CephObjectStoreRealms() CephObjectStoreRealmInformer
	// CephObjectStoreUsers returns a CephObjectStoreUserInformer.
	CephObjectStoreUsers() CephObjectStoreUserInformer
	// CephObjectStoreZones returns a CephObjectStoreZoneInformer.
	CephObjectStoreZones() CephObjectStoreZoneInformer
	// CephObjectStoreZoneGroups returns a CephObjectStoreZoneGroupInformer.
	CephObjectStoreZoneGroups() CephObjectStoreZoneGroupInformer
	// CephRBDMirrors returns a CephRBDMirrorInformer.
	CephRBDMirrors() CephRBDMirrorInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// CephBlockPools returns a CephBlockPoolInformer.
func (v *version) CephBlockPools() CephBlockPoolInformer {
	return &cephBlockPoolInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephClients returns a CephClientInformer.
func (v *version) CephClients() CephClientInformer {
	return &cephClientInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephClusters returns a CephClusterInformer.
func (v *version) CephClusters() CephClusterInformer {
	return &cephClusterInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephFilesystems returns a CephFilesystemInformer.
func (v *version) CephFilesystems() CephFilesystemInformer {
	return &cephFilesystemInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephNFSes returns a CephNFSInformer.
func (v *version) CephNFSes() CephNFSInformer {
	return &cephNFSInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephObjectStores returns a CephObjectStoreInformer.
func (v *version) CephObjectStores() CephObjectStoreInformer {
	return &cephObjectStoreInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephObjectStoreRealms returns a CephObjectStoreRealmInformer.
func (v *version) CephObjectStoreRealms() CephObjectStoreRealmInformer {
	return &cephObjectStoreRealmInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephObjectStoreUsers returns a CephObjectStoreUserInformer.
func (v *version) CephObjectStoreUsers() CephObjectStoreUserInformer {
	return &cephObjectStoreUserInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephObjectStoreZones returns a CephObjectStoreZoneInformer.
func (v *version) CephObjectStoreZones() CephObjectStoreZoneInformer {
	return &cephObjectStoreZoneInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephObjectStoreZoneGroups returns a CephObjectStoreZoneGroupInformer.
func (v *version) CephObjectStoreZoneGroups() CephObjectStoreZoneGroupInformer {
	return &cephObjectStoreZoneGroupInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// CephRBDMirrors returns a CephRBDMirrorInformer.
func (v *version) CephRBDMirrors() CephRBDMirrorInformer {
	return &cephRBDMirrorInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
