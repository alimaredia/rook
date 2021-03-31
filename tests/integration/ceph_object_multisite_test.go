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

package integration

import (
	"github.com/rook/rook/tests/framework/clients"
	"github.com/rook/rook/tests/framework/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

/*
var (
	userid          = "rook-user"
	userdisplayname = "A rook RGW user"
	bucketname      = "smokebkt"
)
*/

// Smoke Test for ObjectStore - Test check the following operations on ObjectStore in order
// Create object store, Create User, Connect to Object Store, Create Bucket, Read/Write/Delete to bucket,
// Check issues in MGRs, Delete Bucket and Delete user
func runObjectMultisiteE2ETest(helper *clients.TestClient, k8sh *utils.K8sHelper, s suite.Suite, namespace string) {
	storeName := "teststore"

	logger.Infof("Object Storage End To End Integration Test - Create Object Store, User,Bucket and read/write to bucket")
	logger.Infof("Running on Rook Cluster %s", namespace)
	//clusterInfo := client.AdminClusterInfo(namespace)

	logger.Infof("Step 0 : Create Object Store User")
	cosuErr := helper.ObjectUserClient.Create(namespace, userid, userdisplayname, storeName)
	require.Nil(s.T(), cosuErr)

	logger.Infof("Step 1 : Create Object Store")
	cobsErr := helper.ObjectClient.Create(namespace, storeName, 3)
	require.Nil(s.T(), cobsErr)

	logger.Infof("Delete Object Store")
	dobsErr := helper.ObjectClient.Delete(namespace, storeName)
	assert.Nil(s.T(), dobsErr)
	logger.Infof("Done deleting object store")
}
