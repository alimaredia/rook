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

// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// CephObjectStoreZoneGroupLister helps list CephObjectStoreZoneGroups.
type CephObjectStoreZoneGroupLister interface {
	// List lists all CephObjectStoreZoneGroups in the indexer.
	List(selector labels.Selector) (ret []*v1.CephObjectStoreZoneGroup, err error)
	// CephObjectStoreZoneGroups returns an object that can list and get CephObjectStoreZoneGroups.
	CephObjectStoreZoneGroups(namespace string) CephObjectStoreZoneGroupNamespaceLister
	CephObjectStoreZoneGroupListerExpansion
}

// cephObjectStoreZoneGroupLister implements the CephObjectStoreZoneGroupLister interface.
type cephObjectStoreZoneGroupLister struct {
	indexer cache.Indexer
}

// NewCephObjectStoreZoneGroupLister returns a new CephObjectStoreZoneGroupLister.
func NewCephObjectStoreZoneGroupLister(indexer cache.Indexer) CephObjectStoreZoneGroupLister {
	return &cephObjectStoreZoneGroupLister{indexer: indexer}
}

// List lists all CephObjectStoreZoneGroups in the indexer.
func (s *cephObjectStoreZoneGroupLister) List(selector labels.Selector) (ret []*v1.CephObjectStoreZoneGroup, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.CephObjectStoreZoneGroup))
	})
	return ret, err
}

// CephObjectStoreZoneGroups returns an object that can list and get CephObjectStoreZoneGroups.
func (s *cephObjectStoreZoneGroupLister) CephObjectStoreZoneGroups(namespace string) CephObjectStoreZoneGroupNamespaceLister {
	return cephObjectStoreZoneGroupNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// CephObjectStoreZoneGroupNamespaceLister helps list and get CephObjectStoreZoneGroups.
type CephObjectStoreZoneGroupNamespaceLister interface {
	// List lists all CephObjectStoreZoneGroups in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1.CephObjectStoreZoneGroup, err error)
	// Get retrieves the CephObjectStoreZoneGroup from the indexer for a given namespace and name.
	Get(name string) (*v1.CephObjectStoreZoneGroup, error)
	CephObjectStoreZoneGroupNamespaceListerExpansion
}

// cephObjectStoreZoneGroupNamespaceLister implements the CephObjectStoreZoneGroupNamespaceLister
// interface.
type cephObjectStoreZoneGroupNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all CephObjectStoreZoneGroups in the indexer for a given namespace.
func (s cephObjectStoreZoneGroupNamespaceLister) List(selector labels.Selector) (ret []*v1.CephObjectStoreZoneGroup, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.CephObjectStoreZoneGroup))
	})
	return ret, err
}

// Get retrieves the CephObjectStoreZoneGroup from the indexer for a given namespace and name.
func (s cephObjectStoreZoneGroupNamespaceLister) Get(name string) (*v1.CephObjectStoreZoneGroup, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("cephobjectstorezonegroup"), name)
	}
	return obj.(*v1.CephObjectStoreZoneGroup), nil
}
