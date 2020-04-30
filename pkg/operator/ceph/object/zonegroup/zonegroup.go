/*
Copyright 2016 The Rook Authors. All rights reserved.

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

package objectzonegroup

import (
	"fmt"
	"github.com/pkg/errors"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	cephclientset "github.com/rook/rook/pkg/client/clientset/versioned/typed/ceph.rook.io/v1"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/daemon/ceph/client"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Context holds the context for the object store.
type Context struct {
	Context     *clusterd.Context
	Name        string
	ClusterName string
}

// create a ceph zonegroup
func createCephZoneGroup(c *clusterd.Context, zoneGroupName string, nameSpace string, realmName string, isMaster bool) error {
	logger.Infof("creating object store zonegroup %q in realm %q in namespace %q", zoneGroupName, realmName, nameSpace)

	realmArg := fmt.Sprintf("--rgw-realm=%s", realmName)
	zoneGroupArg := fmt.Sprintf("--rgw-zonegroup=%s", zoneGroupName)
	objContext := NewContext(c, zoneGroupName, nameSpace)

	masterArg := ""
	if isMaster {
		masterArg = "--master"
	}

	_, err := runAdminCommandNoRealm(objContext, "zonegroup", "get", realmArg, zoneGroupArg)
	if err != nil {
		_, err := runAdminCommandNoRealm(objContext, "zonegroup", "create", realmArg, zoneGroupArg, masterArg)
		if err != nil {
			logger.Warningf("failed to create rgw zonegroup %q. %v", zoneGroupName, err)
		}
	}

	return nil
}

// NewContext creates a new object store zonegroup context.
func NewContext(context *clusterd.Context, name, clusterName string) *Context {
	return &Context{Context: context, Name: name, ClusterName: clusterName}
}

func runAdminCommandNoRealm(c *Context, args ...string) (string, error) {
	command, args := client.FinalizeCephCommandArgs("radosgw-admin", args, c.Context.ConfigDir, c.ClusterName, client.AdminUsername)

	// start the rgw admin command
	output, err := c.Context.Executor.ExecuteCommandWithOutput(command, args...)
	if err != nil {
		return "", errors.Wrapf(err, "failed to run radosgw-admin")
	}

	return output, nil
}

func getObjectStoreRealm(c cephclientset.CephV1Interface, namespace, realmName string) (*cephv1.CephObjectStoreRealm, error) {
	// Verify the object store realm API object actually exists
	realm, err := c.CephObjectStoreRealms(namespace).Get(realmName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.Wrapf(err, "cephObjectStoreRealm %s not found", realmName)
		}
		return nil, errors.Wrapf(err, "error getting cephObjectStore")
	}
	return realm, err
}
