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

package objectzone

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/daemon/ceph/client"
)

// Context holds the context for the object store.
type Context struct {
	Context     *clusterd.Context
	Name        string
	ClusterName string
}

// create a ceph zone
func createCephZone(c *clusterd.Context, zoneName string, nameSpace string, realmName string, zoneGroupName string, isMaster bool) error {
	logger.Infof("creating object store zone %q in zonegroup %q in realm %q in namespace %q", zoneName, zoneGroupName, realmName, nameSpace)

	realmArg := fmt.Sprintf("--rgw-realm=%s", realmName)
	zoneGroupArg := fmt.Sprintf("--rgw-zonegroup=%s", zoneGroupName)
	zoneArg := fmt.Sprintf("--rgw-zone=%s", zoneName)
	objContext := NewContext(c, zoneName, nameSpace)

	masterArg := ""
	if isMaster {
		masterArg = "--master"
	}

	_, err := runAdminCommandNoRealm(objContext, "zone", "get", realmArg, zoneGroupArg, zoneArg)
	if err != nil {
		_, err := runAdminCommandNoRealm(objContext, "zone", "create", realmArg, zoneGroupArg, zoneArg, masterArg)
		if err != nil {
			logger.Warningf("failed to create rgw zone %q. %v", zoneName, err)
		}
	}

	return nil
}

// NewContext creates a new object store zone context.
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
