package services

import (
	"github.com/huaweicloud/golangsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func initNetwork(t *testing.T, client *Client) {
	require.NoError(t, client.InitNetwork())
}

func TestClient_CreateVPC(t *testing.T) {
	client := authClient(t)
	initNetwork(t, client)
	vpc, err := client.CreateVPC(vpcName)
	require.NoError(t, err)
	assert.NoError(t, client.DeleteVPC(vpc.ID))
}

func TestClient_CreateSubnet(t *testing.T) {
	client := authClient(t)
	initNetwork(t, client)
	vpc, err := client.CreateVPC(vpcName)
	require.NoError(t, err)

	subnet, err := client.CreateSubnet(vpc.ID, subnetName)
	assert.NoError(t, err)

	err = client.WaitForSubnetStatus(subnet.ID, "ACTIVE")
	assert.NoError(t, err)

	found, err := client.FindSubnet(vpc.ID, subnetName)
	assert.NoError(t, err)
	assert.Equalf(t, subnet.ID, found, invalidFind, "subnet")

	assert.NoError(t, client.DeleteSubnet(vpc.ID, found))

	err = client.WaitForSubnetStatus(subnet.ID, "")
	assert.IsType(t, golangsdk.ErrDefault404{}, err)

	assert.NoError(t, client.DeleteVPC(vpc.ID))
}
