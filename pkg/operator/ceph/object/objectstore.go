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

package object

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/daemon/ceph/client"
	ceph "github.com/rook/rook/pkg/daemon/ceph/client"
	"github.com/rook/rook/pkg/operator/ceph/config"
	opcontroller "github.com/rook/rook/pkg/operator/ceph/controller"
	"github.com/rook/rook/pkg/operator/k8sutil"
)

const (
	rootPool = ".rgw.root"

	// AppName is the name Rook uses for the object store's application
	AppName               = "rook-ceph-rgw"
	bucketProvisionerName = "ceph.rook.io/bucket"
)

var (
	metadataPools = []string{
		// .rgw.root (rootPool) is appended to this slice where needed
		"rgw.control",
		"rgw.meta",
		"rgw.log",
		"rgw.buckets.index",
		"rgw.buckets.non-ec",
	}
	dataPoolName = "rgw.buckets.data"
)

type idType struct {
	ID string `json:"id"`
}

type zoneGroupType struct {
	MasterZoneID string      `json:"master_zone"`
	Zones        []zonesType `json:"zones"`
}

type zonesType struct {
	Name      string   `json:"name"`
	Endpoints []string `json:"endpoints"`
}

type realmType struct {
	Realms []string `json:"realms"`
}

func deleteRealmAndPools(context *Context, spec cephv1.ObjectStoreSpec) error {
	stores, err := getObjectStores(context)
	if err != nil {
		return errors.Wrap(err, "failed to detect object stores during deletion")
	}
	logger.Infof("Found stores %v when deleting store %s", stores, context.Name)

	err = deleteRealm(context)
	if err != nil {
		return errors.Wrap(err, "failed to delete realm")
	}

	lastStore := false
	if len(stores) == 1 && stores[0] == context.Name {
		lastStore = true
	}

	if !spec.PreservePoolsOnDelete {
		err = deletePools(context, spec, lastStore)
		if err != nil {
			return errors.Wrap(err, "failed to delete object store pools")
		}
	} else {
		logger.Infof("PreservePoolsOnDelete is set in object store %s. Pools not deleted", context.Name)
	}
	return nil
}

func checkZoneIsMaster(context *Context, realmArg string, zoneGroupArg string, zoneArg string) (bool, error) {
	zoneGroupOutput, err := RunAdminCommandNoRealm(context, "zonegroup", "get", realmArg, zoneGroupArg)
	if err != nil {
		return false, errors.Wrap(err, "failed to get rgw zone group")
	}
	masterZoneID, err := decodeMasterZoneID(zoneGroupOutput)
	if err != nil {
		return false, errors.Wrap(err, "failed to parse master zone id")
	}

	zoneOutput, err := RunAdminCommandNoRealm(context, "zone", "get", realmArg, zoneGroupArg, zoneArg)
	if err != nil {
		return false, errors.Wrap(err, "failed to get rgw zone")
	}
	zoneID, err := decodeID(zoneOutput)
	if err != nil {
		return false, errors.Wrap(err, "failed to parse zone id")
	}

	if zoneID == masterZoneID {
		return true, nil
	}

	return false, nil
}

func getZoneEndpoints(context *Context, realmArg string, zoneGroupArg string, zoneName string, serviceEndpoint string) (string, error) {
	zoneGroupOutput, err := RunAdminCommandNoRealm(context, "zonegroup", "get", realmArg, zoneGroupArg)
	if err != nil {
		return "", errors.Wrap(err, "failed to get rgw zone group")
	}
	zones, err := decodeZonesList(zoneGroupOutput)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse zones list")
	}

	numZones := len(zones)
	zoneEndpoints := ""
	for i := 0; i < numZones; i++ {
		if zones[i].Name == zoneName {
			numEndpoints := len(zones[i].Endpoints)
			for j := 0; j < numEndpoints; j++ {
				// in case object-store operator code is rereconciled, zone modify could get run again with serviceEndpoint added again
				if zones[i].Endpoints[j] != serviceEndpoint {
					zoneEndpoints += zones[i].Endpoints[j] + ","
				}
			}
			break
		}
	}

	zoneEndpoints += serviceEndpoint

	return zoneEndpoints, nil
}

func setMultisite(context *Context, serviceIP string, spec cephv1.ObjectStoreSpec, realmName string, zoneGroupName string, zoneName string) error {
	realmArg := fmt.Sprintf("--rgw-realm=%s", realmName)
	zoneGroupArg := fmt.Sprintf("--rgw-zonegroup=%s", zoneGroupName)
	zoneArg := fmt.Sprintf("--rgw-zone=%s", zoneName)
	updatePeriod := false
	serviceEndpoint := fmt.Sprintf("%s:%d", serviceIP, spec.Gateway.Port)

	zoneEndpoints, err := getZoneEndpoints(context, realmArg, zoneGroupArg, zoneName, serviceEndpoint)
	if err != nil {
		return err
	}
	logger.Debugf("Endpoints for zone %q are: %q", zoneName, zoneEndpoints)
	endpointArg := fmt.Sprintf("--endpoints=%s", zoneEndpoints)

	if spec.IsMultisite() {
		updatePeriod = true
		zoneIsMaster, err := checkZoneIsMaster(context, realmArg, zoneGroupArg, zoneArg)
		if err != nil {
			return err
		}

		if zoneIsMaster {
			_, err := RunAdminCommandNoRealm(context, "zonegroup", "modify", realmArg, zoneGroupArg, endpointArg)
			if err != nil {
				return errors.Wrapf(err, "failed to add object store %q in rgw zone group %q", context.Name, zoneGroupName)
			}
			logger.Debugf("Added endpoints %q to zonegroup %q", zoneEndpoints, zoneGroupName)
		}
		_, err = RunAdminCommandNoRealm(context, "zone", "modify", realmArg, zoneGroupArg, zoneArg, endpointArg)
		if err != nil {
			return errors.Wrapf(err, "failed to add object store %q in rgw zone %q", context.Name, zoneName)
		}
		logger.Infof("Added object store %q to realm %q, zonegroup %q, zone %q", context.Name, realmName, zoneGroupName, zoneName)
	} else {
		// create the realm if it doesn't exist yet
		output, err := runAdminCommand(context, "realm", "get")
		if err != nil {
			updatePeriod = true
			output, err = runAdminCommand(context, "realm", "create")
			if err != nil {
				return errors.Wrapf(err, "failed to create rgw realm %q", context.Name)
			}
		}

		realmID, err := decodeID(output)
		if err != nil {
			return errors.Wrap(err, "failed to parse realm id")
		}

		// create the zonegroup if it doesn't exist yet
		output, err = runAdminCommand(context, "zonegroup", "get")
		if err != nil {
			updatePeriod = true
			output, err = runAdminCommand(context, "zonegroup", "create", "--master", endpointArg)
			if err != nil {
				return errors.Wrapf(err, "failed to create rgw zonegroup for %q", context.Name)
			}
		}

		zoneGroupID, err := decodeID(output)
		if err != nil {
			return errors.Wrap(err, "failed to parse zone group id")
		}

		// create the zone if it doesn't exist yet
		output, err = runAdminCommand(context, "zone", "get", zoneArg)
		if err != nil {
			updatePeriod = true
			output, err = runAdminCommand(context, "zone", "create", "--master", endpointArg, zoneArg)
			if err != nil {
				return errors.Wrapf(err, "failed to create rgw zonegroup for %q", context.Name)
			}
		}

		zoneID, err := decodeID(output)
		if err != nil {
			return errors.Wrap(err, "failed to parse zone id")
		}

		logger.Infof("RGW: realm=%s, zonegroup=%s, zone=%s", realmID, zoneGroupID, zoneID)
	}

	if updatePeriod {
		// the period will help notify other zones of changes if there are multi-zones
		_, err := RunAdminCommandNoRealm(context, "period", "update", "--commit", realmArg, zoneGroupArg, zoneArg)
		if err != nil {
			return errors.Wrap(err, "failed to update period")
		}
	}

	return nil
}

func deleteRealm(context *Context) error {
	//  <name>
	_, err := runAdminCommand(context, "realm", "delete", "--rgw-realm", context.Name)
	if err != nil {
		logger.Warningf("failed to delete rgw realm %q. %v", context.Name, err)
	}

	_, err = runAdminCommand(context, "zonegroup", "delete", "--rgw-zonegroup", context.Name)
	if err != nil {
		logger.Warningf("failed to delete rgw zonegroup %q. %v", context.Name, err)
	}

	_, err = runAdminCommand(context, "zone", "delete", "--rgw-zone", context.Name)
	if err != nil {
		logger.Warningf("failed to delete rgw zone %q. %v", context.Name, err)
	}

	return nil
}

func decodeID(data string) (string, error) {
	var id idType
	err := json.Unmarshal([]byte(data), &id)
	if err != nil {
		return "", errors.Wrap(err, "Failed to unmarshal json")
	}

	return id.ID, err
}

func decodeMasterZoneID(data string) (string, error) {
	var config zoneGroupType
	err := json.Unmarshal([]byte(data), &config)
	if err != nil {
		return "", errors.Wrap(err, "Failed to unmarshal json")
	}

	return config.MasterZoneID, err
}

func decodeZonesList(data string) ([]zonesType, error) {
	var config zoneGroupType
	err := json.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to unmarshal json")
	}

	return config.Zones, err
}

func getObjectStores(context *Context) ([]string, error) {
	output, err := RunAdminCommandNoRealm(context, "realm", "list")
	if err != nil {
		if strings.Index(err.Error(), "exit status 2") != 0 {
			return []string{}, nil
		}
		return nil, err
	}

	var r realmType
	err = json.Unmarshal([]byte(output), &r)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to unmarshal realms")
	}

	return r.Realms, nil
}

func deletePools(context *Context, spec cephv1.ObjectStoreSpec, lastStore bool) error {
	if emptyPool(spec.DataPool) && emptyPool(spec.MetadataPool) {
		logger.Info("skipping removal of pools since not specified in the object store")
		return nil
	}

	pools := append(metadataPools, dataPoolName)
	if lastStore {
		pools = append(pools, rootPool)
	}

	for _, pool := range pools {
		name := poolName(context.Name, pool)
		if err := ceph.DeletePool(context.Context, context.ClusterName, name); err != nil {
			logger.Warningf("failed to delete pool %q. %v", name, err)
		}
	}

	// Delete erasure code profile if any
	erasureCodes, err := ceph.ListErasureCodeProfiles(context.Context, context.ClusterName)
	if err != nil {
		return errors.Wrapf(err, "failed to list erasure code profiles for cluster %s", context.ClusterName)
	}
	// cleans up the EC profile for the data pool only. Metadata pools don't support EC (only replication is supported).
	ecProfileName := client.GetErasureCodeProfileForPool(context.Name)
	for i := range erasureCodes {
		if erasureCodes[i] == ecProfileName {
			if err := ceph.DeleteErasureCodeProfile(context.Context, context.ClusterName, ecProfileName); err != nil {
				return errors.Wrapf(err, "failed to delete erasure code profile %s for object store %s", ecProfileName, context.Name)
			}
			break
		}
	}

	return nil
}

func createPools(context *Context, spec cephv1.ObjectStoreSpec) error {
	if emptyPool(spec.DataPool) && emptyPool(spec.MetadataPool) {
		logger.Info("no pools specified for the object store, checking for their existence...")
		pools := append(metadataPools, dataPoolName)
		pools = append(pools, rootPool)
		var missingPools []string
		for _, pool := range pools {
			poolName := poolName(context.Name, pool)
			_, err := ceph.GetPoolDetails(context.Context, context.ClusterName, poolName)
			if err != nil {
				logger.Debugf("failed to find pool %q. %v", poolName, err)
				missingPools = append(missingPools, poolName)
			}
		}
		if len(missingPools) > 0 {
			return fmt.Errorf("object store pools are missing: %v", missingPools)
		}
	}

	// get the default PG count for rgw metadata pools
	metadataPoolPGs, err := config.GetMonStore(context.Context, context.ClusterName).Get("mon.", "rgw_rados_pool_pg_num_min")
	if err != nil {
		logger.Warningf("failed to adjust the PG count for rgw metadata pools. using the general default. %v", err)
		metadataPoolPGs = ceph.DefaultPGCount
	}

	if err := createSimilarPools(context, append(metadataPools, rootPool), spec.MetadataPool, metadataPoolPGs, ""); err != nil {
		return errors.Wrap(err, "failed to create metadata pools")
	}

	ecProfileName := ""
	if spec.DataPool.IsErasureCoded() {
		ecProfileName = client.GetErasureCodeProfileForPool(context.Name)
		// create a new erasure code profile for the data pool
		if err := ceph.CreateErasureCodeProfile(context.Context, context.ClusterName, ecProfileName, spec.DataPool); err != nil {
			return errors.Wrapf(err, "failed to create erasure code profile for object store %s", context.Name)
		}
	}

	if err := createSimilarPools(context, []string{dataPoolName}, spec.DataPool, ceph.DefaultPGCount, ecProfileName); err != nil {
		return errors.Wrap(err, "failed to create data pool")
	}

	return nil
}

func createSimilarPools(context *Context, pools []string, poolSpec cephv1.PoolSpec, pgCount, ecProfileName string) error {
	for _, pool := range pools {
		// create the pool if it doesn't exist yet
		name := poolName(context.Name, pool)
		if poolDetails, err := ceph.GetPoolDetails(context.Context, context.ClusterName, name); err != nil {
			// If the ceph config has an EC profile, an EC pool must be created. Otherwise, it's necessary
			// to create a replicated pool.
			var err error
			if poolSpec.IsErasureCoded() {
				// An EC pool backing an object store does not need to enable EC overwrites, so the pool is
				// created with that property disabled to avoid unnecessary performance impact.
				err = ceph.CreateECPoolForApp(context.Context, context.ClusterName, name, ecProfileName, poolSpec, pgCount, AppName, false /* enableECOverwrite */)
			} else {
				err = ceph.CreateReplicatedPoolForApp(context.Context, context.ClusterName, name, poolSpec, pgCount, AppName)
			}
			if err != nil {
				return errors.Wrapf(err, "failed to create pool %s for object store %s.", name, context.Name)
			}
		} else {
			// pools already exist
			if !poolSpec.IsErasureCoded() {
				// detect if the replication is different from the pool details
				if poolDetails.Size != poolSpec.Replicated.Size {
					logger.Infof("pool size is changed from %d to %d", poolDetails.Size, poolSpec.Replicated.Size)
					if err := ceph.SetPoolReplicatedSizeProperty(context.Context, context.ClusterName, poolDetails.Name, strconv.FormatUint(uint64(poolSpec.Replicated.Size), 10)); err != nil {
						return errors.Wrapf(err, "failed to set size property to replicated pool %q to %d", poolDetails.Name, poolSpec.Replicated.Size)
					}
				}
			}
		}
		// Set the pg_num_min if not the default so the autoscaler won't immediately increase the pg count
		if pgCount != ceph.DefaultPGCount {
			if err := ceph.SetPoolProperty(context.Context, context.ClusterName, name, "pg_num_min", pgCount); err != nil {
				return errors.Wrapf(err, "failed to set pg_num_min on pool %q to %q", name, pgCount)
			}
		}
	}
	return nil
}

func poolName(storeName, poolName string) string {
	if strings.HasPrefix(poolName, ".") {
		return poolName
	}
	// the name of the pool is <instance>.<name>, except for the pool ".rgw.root" that spans object stores
	return fmt.Sprintf("%s.%s", storeName, poolName)
}

// GetObjectBucketProvisioner returns the bucket provisioner name appended with operator namespace if OBC is watching on it
func GetObjectBucketProvisioner(c *clusterd.Context, namespace string) string {
	provName := bucketProvisionerName
	obcWatchOnNamespace, err := k8sutil.GetOperatorSetting(c.Clientset, opcontroller.OperatorSettingConfigMapName, "ROOK_OBC_WATCH_OPERATOR_NAMESPACE", "false")
	if err != nil {
		logger.Warningf("failed to verify if obc should watch the operator namespace or all of them, watching all")
	} else {
		if strings.EqualFold(obcWatchOnNamespace, "true") {
			provName = fmt.Sprintf("%s.%s", namespace, bucketProvisionerName)
		}
	}
	return provName
}
