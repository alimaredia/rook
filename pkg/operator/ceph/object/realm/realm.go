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
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/daemon/ceph/client"
	ceph "github.com/rook/rook/pkg/daemon/ceph/client"
	"github.com/rook/rook/pkg/operator/ceph/config"
	"strconv"
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
			logger.Warningf("failed to create rgw realm %q. %v", realmName, err)
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

func createRootPool(c *clusterd.Context, realmName string, nameSpace string, poolSize uint) error {
	context := NewContext(c, realmName, nameSpace)
	rootPool := ".rgw.root"
	confirmFlag := "--yes-i-really-mean-it"
	appName := "rook-ceph-rgw"

	// see if .rgw.root exists
	if poolDetails, err := ceph.GetPoolDetails(context.Context, context.ClusterName, rootPool); err != nil {
		logger.Infof(".rgw.root pool not yet created")
		failureDomain := cephv1.DefaultFailureDomain
		metadataPoolPGs, err := config.GetMonStore(context.Context, context.ClusterName).Get("mon.", "rgw_rados_pool_pg_num_min")

		// set the crush root to the default if not already specified
		crushRoot := "default"
		args := []string{"osd", "crush", "rule", "create-replicated", rootPool, crushRoot, failureDomain}
		_, err = client.NewCephCommand(context.Context, context.ClusterName, args).Run()
		if err != nil {
			return errors.Wrapf(err, "failed to create crush rule %s", rootPool)
		}

		// create the pool
		args = []string{"osd", "pool", "create", rootPool, metadataPoolPGs, "replicated", rootPool, "--size", strconv.FormatUint(uint64(poolSize), 10)}
		output, err := client.NewCephCommand(context.Context, context.ClusterName, args).Run()
		if err != nil {
			return errors.Wrapf(err, "failed to create replicated pool %s. %s", rootPool, string(output))
		}

		// the pool is type replicated, set the size for the pool now that it's been created
		if err = client.SetPoolReplicatedSizeProperty(context.Context, context.ClusterName, rootPool, strconv.FormatUint(uint64(poolSize), 10)); err != nil {
			return errors.Wrapf(err, "failed to set size property to replicated pool %q to %d", rootPool, poolSize)
		}

		// tag pool with "rook-ceph-rgw" tag
		args = []string{"osd", "pool", "application", "enable", rootPool, appName, confirmFlag}
		_, err = client.NewCephCommand(context.Context, context.ClusterName, args).Run()
		if err != nil {
			return errors.Wrapf(err, "failed to enable application %s on pool %s", appName, rootPool)
		}
		logger.Infof("created replicated pool %s", rootPool)
	} else {
		logger.Infof("%s pool has already been created", rootPool)
		if poolDetails.Size != poolSize {
			logger.Infof("pool size is changing from %d to %d", poolDetails.Size, poolSize)
			if err = client.SetPoolReplicatedSizeProperty(context.Context, context.ClusterName, rootPool, strconv.FormatUint(uint64(poolSize), 10)); err != nil {
				return errors.Wrapf(err, "failed to set size property to replicated pool %q to %d", poolDetails.Name, poolSize)
			}
		}
	}

	return nil
}
