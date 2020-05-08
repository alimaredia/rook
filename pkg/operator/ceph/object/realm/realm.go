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

package objectrealm

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

// create a ceph realm
func createCephRealm(c *clusterd.Context, realmName string, nameSpace string) error {
	logger.Infof("creating object store realm %q in namespace", realmName)

	realmArg := fmt.Sprintf("--rgw-realm=%s", realmName)
	objContext := NewContext(c, realmName, nameSpace)

	_, err := runAdminCommandNoRealm(objContext, "realm", "get", realmArg)
	if err != nil {
		_, err := runAdminCommandNoRealm(objContext, "realm", "create", realmArg)
		if err != nil {
			return errors.Wrapf(err, "failed to create ceph realm %s", realmName)
		}
	}

	return nil
}

// NewContext creates a new object store realm context.
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
